package lightstep

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/context"

	"github.com/lightstep/lightstep-tracer-go/lightstep_thrift"
	"github.com/lightstep/lightstep-tracer-go/thrift_0_9_2/lib/go/thrift"
)

// ThriftRecorder buffers spans and forwards them to a LightStep collector.
type ThriftRecorder struct {
	lock sync.Mutex

	// auth and runtime information
	auth       *lightstep_thrift.Auth
	attributes map[string]string
	startTime  time.Time

	// Time window of the data to be included in the next report.
	reportOldest   time.Time
	reportYoungest time.Time

	// buffered data
	buffer   spansBuffer
	counters counterSet // The unreported count

	lastReportAttempt  time.Time
	maxReportingPeriod time.Duration
	reportInFlight     bool
	// Remote service that will receive reports
	backend lightstep_thrift.ReportingService

	// apiURL is the base URL of the LightStep web API, used for
	// explicit trace collection requests.
	apiURL string

	// AccessToken is the access token used for explicit trace
	// collection requests.
	AccessToken string

	verbose bool

	// We allow our remote peer to disable this instrumentation at any
	// time, turning all potentially costly runtime operations into
	// no-ops.
	disabled bool
	closech  chan struct{}

	// flags replacement
	maxLogMessageLen int

	reportTimeout time.Duration
}

func NewThriftRecorder(opts Options, attributes map[string]string) *ThriftRecorder {

	now := time.Now()
	rec := &ThriftRecorder{
		auth: &lightstep_thrift.Auth{
			AccessToken: thrift.StringPtr(opts.AccessToken),
		},
		attributes:         attributes,
		startTime:          now,
		reportOldest:       now,
		reportYoungest:     now,
		maxReportingPeriod: defaultMaxReportingPeriod,
		verbose:            opts.Verbose,
		apiURL:             getThriftAPIURL(opts),
		AccessToken:        opts.AccessToken,
		maxLogMessageLen:   opts.MaxLogValueLen,
		reportTimeout:      opts.ReportTimeout,
	}

	timeout := 60 * time.Second
	if opts.ReportTimeout > 0 {
		timeout = opts.ReportTimeout
	}

	if opts.ThriftConnector != nil {
		rec.backend = opts.ThriftConnector()
	} else {
		transport, err := thrift.NewTHttpPostClient(getThriftCollectorURL(opts), timeout)
		if err != nil {
			rec.maybeLogError(err)
			return nil
		}

		rec.backend = lightstep_thrift.NewReportingServiceClientFactory(
			transport, thrift.NewTBinaryProtocolFactoryDefault())
	}

	return rec
}

type DummyConnection struct{}

func (x *DummyConnection) Close() error { return nil }

func (client *ThriftRecorder) ConnectClient() (Connection, error) {
	return &DummyConnection{}, nil
}

func (client *ThriftRecorder) Report(_ context.Context, buffer *reportBuffer) (*CollectorResponse, error) {
	rawSpans := buffer.rawSpans
	// Convert them to thrift.
	recs := make([]*lightstep_thrift.SpanRecord, len(rawSpans))
	// TODO: could pool lightstep_thrift.SpanRecords
	for i, raw := range rawSpans {
		var joinIds []*lightstep_thrift.TraceJoinId
		var attributes []*lightstep_thrift.KeyValue
		for key, value := range raw.Tags {
			if strings.HasPrefix(key, "join:") {
				joinIds = append(joinIds, &lightstep_thrift.TraceJoinId{key, fmt.Sprint(value)})
			} else {
				attributes = append(attributes, &lightstep_thrift.KeyValue{key, fmt.Sprint(value)})
			}
		}
		logs := make([]*lightstep_thrift.LogRecord, len(raw.Logs))
		for j, log := range raw.Logs {
			thriftLogRecord := &lightstep_thrift.LogRecord{
				TimestampMicros: thrift.Int64Ptr(log.Timestamp.UnixNano() / 1000),
			}
			// In the deprecated thrift case, we can reuse a single "field"
			// encoder across all of the N log fields.
			lfe := thriftLogFieldEncoder{thriftLogRecord, client}
			for _, f := range log.Fields {
				f.Marshal(&lfe)
			}
			logs[j] = thriftLogRecord
		}

		// TODO implement baggage
		if raw.ParentSpanID != 0 {
			attributes = append(attributes, &lightstep_thrift.KeyValue{ParentSpanGUIDKey,
				strconv.FormatUint(raw.ParentSpanID, 16)})
		}

		recs[i] = &lightstep_thrift.SpanRecord{
			SpanGuid:       thrift.StringPtr(strconv.FormatUint(raw.Context.SpanID, 16)),
			TraceGuid:      thrift.StringPtr(strconv.FormatUint(raw.Context.TraceID, 16)),
			SpanName:       thrift.StringPtr(raw.Operation),
			JoinIds:        joinIds,
			OldestMicros:   thrift.Int64Ptr(raw.Start.UnixNano() / 1000),
			YoungestMicros: thrift.Int64Ptr(raw.Start.Add(raw.Duration).UnixNano() / 1000),
			Attributes:     attributes,
			LogRecords:     logs,
		}
	}

	metrics := lightstep_thrift.Metrics{
		Counts: []*lightstep_thrift.MetricsSample{
			&lightstep_thrift.MetricsSample{
				Name:       "spans.dropped",
				Int64Value: &buffer.droppedSpanCount,
			},
		},
	}

	req := &lightstep_thrift.ReportRequest{
		OldestMicros:    thrift.Int64Ptr(buffer.reportEnd.UnixNano() / 1000),
		YoungestMicros:  thrift.Int64Ptr(buffer.reportStart.UnixNano() / 1000),
		Runtime:         client.thriftRuntime(),
		SpanRecords:     recs,
		InternalMetrics: &metrics,
	}

	resp, err := client.backend.Report(client.auth, req)
	commands := make([]*Command, len(resp.Commands))
	for i, command := range resp.GetCommands() {
		commands[i] = &Command{command.GetDisable()}
	}
	return &CollectorResponse{Errors: resp.GetErrors(), Commands: commands}, err
}

// caller must hold r.lock
func (r *ThriftRecorder) thriftRuntime() *lightstep_thrift.Runtime {
	runtimeAttrs := []*lightstep_thrift.KeyValue{}
	for k, v := range r.attributes {
		runtimeAttrs = append(runtimeAttrs, &lightstep_thrift.KeyValue{k, v})
	}
	return &lightstep_thrift.Runtime{
		StartMicros: thrift.Int64Ptr(r.startTime.UnixNano() / 1000),
		Attrs:       runtimeAttrs,
	}
}

// maybeLogError logs the first error it receives using the standard log
// package and may also log subsequent errors based on verboseFlag.
func (r *ThriftRecorder) maybeLogError(err error) {
	if r.verbose {
		log.Printf("LightStep error: %v\n", err)
	} else {
		// Even if the flag is not set, always log at least one error.
		logOneError.Do(func() {
			log.Printf("LightStep instrumentation error (%v). Set the Verbose option to enable more logging.\n", err)
		})
	}
}

// maybeLogInfof may format and log its arguments if verboseFlag is set.
func (r *ThriftRecorder) maybeLogInfof(format string, args ...interface{}) {
	if r.verbose {
		s := fmt.Sprintf(format, args...)
		log.Printf("LightStep info: %s\n", s)
	}
}

func getThriftCollectorURL(opts Options) string {
	return getURL(opts.Collector,
		defaultCollectorHost,
		collectorPath)
}

func getThriftAPIURL(opts Options) string {
	return getURL(opts.LightStepAPI, defaultAPIHost, "")
}

func getThriftURL(e Endpoint, host, path string) string {
	if e.Host != "" {
		host = e.Host
	}
	httpProtocol := "https"
	port := defaultSecurePort
	if e.Plaintext {
		httpProtocol = "http"
		port = defaultPlainPort
	}
	if e.Port > 0 {
		port = e.Port
	}
	return fmt.Sprintf("%s://%s:%d%s", httpProtocol, host, port, path)
}
