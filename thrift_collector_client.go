package lightstep

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/context"

	"github.com/lightstep/lightstep-tracer-go/lightstep_thrift"
	"github.com/lightstep/lightstep-tracer-go/thrift_0_9_2/lib/go/thrift"
)

// ThriftCollectorClient specifies how to send reports back to a LightStep
// collector via thrift
type ThriftCollectorClient struct {
	// auth and runtime information
	auth       *lightstep_thrift.Auth
	attributes map[string]string
	startTime  time.Time

	lastReportAttempt  time.Time
	maxReportingPeriod time.Duration
	reportInFlight     bool
	// Remote service that will receive reports
	thriftClient lightstep_thrift.ReportingService

	// apiURL is the base URL of the LightStep web API, used for
	// explicit trace collection requests.
	apiURL string

	// AccessToken is the access token used for explicit trace
	// collection requests.
	AccessToken string

	verbose bool

	// flags replacement
	maxLogMessageLen int
	maxLogKeyLen     int

	reportTimeout time.Duration

	thriftConnectorFactory ConnectorFactory
}

func NewThriftCollectorClient(opts Options, attributes map[string]string) *ThriftCollectorClient {
	reportTimeout := 60 * time.Second
	if opts.ReportTimeout > 0 {
		reportTimeout = opts.ReportTimeout
	}

	now := time.Now()
	rec := &ThriftCollectorClient{
		auth: &lightstep_thrift.Auth{
			AccessToken: thrift.StringPtr(opts.AccessToken),
		},
		attributes:             attributes,
		startTime:              now,
		maxReportingPeriod:     defaultMaxReportingPeriod,
		verbose:                opts.Verbose,
		apiURL:                 getThriftAPIURL(opts),
		AccessToken:            opts.AccessToken,
		maxLogMessageLen:       opts.MaxLogValueLen,
		maxLogKeyLen:           opts.MaxLogKeyLen,
		reportTimeout:          reportTimeout,
		thriftConnectorFactory: opts.ConnFactory,
	}

	return rec
}

func (client *ThriftCollectorClient) ConnectClient() (Connection, error) {
	var conn Connection

	if client.thriftConnectorFactory != nil {
		unchecked_client, transport, err := client.thriftConnectorFactory()
		if err != nil {
			return nil, err
		}

		thriftClient, ok := unchecked_client.(lightstep_thrift.ReportingService)
		if !ok {
			return nil, fmt.Errorf("Thrift connector factory did not provide valid client!")
		}

		conn = transport
		client.thriftClient = thriftClient
	} else {
		transport, err := thrift.NewTHttpPostClient(client.apiURL, client.reportTimeout)
		if err != nil {
			maybeLogError(err, client.verbose)
			return nil, err
		}

		conn = transport
		client.thriftClient = lightstep_thrift.NewReportingServiceClientFactory(
			transport, thrift.NewTBinaryProtocolFactoryDefault())
	}
	return conn, nil
}

func (*ThriftCollectorClient) ShouldReconnect() bool {
	return false
}

func (client *ThriftCollectorClient) Report(_ context.Context, buffer *reportBuffer) (*CollectorResponse, error) {
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

	resp, err := client.thriftClient.Report(client.auth, req)
	commands := make([]*Command, len(resp.Commands))
	for i, command := range resp.GetCommands() {
		commands[i] = &Command{command.GetDisable()}
	}
	return &CollectorResponse{Errors: resp.GetErrors(), Commands: commands}, err
}

// caller must hold r.lock
func (r *ThriftCollectorClient) thriftRuntime() *lightstep_thrift.Runtime {
	runtimeAttrs := []*lightstep_thrift.KeyValue{}
	for k, v := range r.attributes {
		runtimeAttrs = append(runtimeAttrs, &lightstep_thrift.KeyValue{k, v})
	}
	return &lightstep_thrift.Runtime{
		StartMicros: thrift.Int64Ptr(r.startTime.UnixNano() / 1000),
		Attrs:       runtimeAttrs,
	}
}

func getThriftCollectorURL(opts Options) string {
	return getThriftURL(opts.Collector,
		defaultCollectorHost,
		collectorPath)
}

func getThriftAPIURL(opts Options) string {
	return getThriftURL(opts.LightStepAPI, defaultAPIHost, "")
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
