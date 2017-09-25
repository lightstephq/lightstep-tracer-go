package lightstep

import (
	"fmt"
	"math/rand"
	"reflect"
	"time"

	"golang.org/x/net/context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	// N.B.(jmacd): Do not use google.golang.org/glog in this package.
	"github.com/lightstep/lightstep-tracer-go/collectorpb"
)

const (
	spansDropped     = "spans.dropped"
	logEncoderErrors = "log_encoder.errors"
)

var (
	intType = reflect.TypeOf(int64(0))
)

// grpcCollectorClient specifies how to send reports back to a LightStep
// collector via grpc.
type grpcCollectorClient struct {
	// auth and runtime information
	attributes map[string]string
	reporterID uint64

	// accessToken is the access token used for explicit trace collection requests.
	accessToken string
	maxReportingPeriod time.Duration // set by GrpcOptions.MaxReportingPeriod
	reconnectPeriod    time.Duration // set by GrpcOptions.ReconnectPeriod
	reportingTimeout   time.Duration // set by GrpcOptions.ReportTimeout

	// Remote service that will receive reports.
	hostPort      string
	grpcClient    collectorpb.CollectorServiceClient
	connTimestamp time.Time
	dialOptions   []grpc.DialOption

	// converters
	converter *protoConverter

	// For testing purposes only
	grpcConnectorFactory ConnectorFactory
}

func newGrpcCollectorClient(opts Options, reporterID uint64, attributes map[string]string) *grpcCollectorClient {
	rec := &grpcCollectorClient{
		accessToken:          opts.AccessToken,
		attributes:           attributes,
		maxReportingPeriod:   opts.ReportingPeriod,
		reportingTimeout:     opts.ReportTimeout,
		reporterID:           reporterID,
		hostPort:             opts.Collector.HostPort(),
		reconnectPeriod:      time.Duration(float64(opts.ReconnectPeriod) * (1 + 0.2*rand.Float64())),
		converter:            newProtoConverter(opts),
		grpcConnectorFactory: opts.ConnFactory,
	}

	rec.dialOptions = append(rec.dialOptions, grpc.WithDefaultCallOptions(grpc.MaxCallSendMsgSize(opts.GRPCMaxCallSendMsgSizeBytes)))
	if opts.Collector.Plaintext {
		rec.dialOptions = append(rec.dialOptions, grpc.WithInsecure())
	} else {
		rec.dialOptions = append(rec.dialOptions, grpc.WithTransportCredentials(credentials.NewClientTLSFromCert(nil, "")))
	}

	return rec
}

func (client *grpcCollectorClient) ConnectClient() (Connection, error) {
	now := time.Now()
	var conn Connection
	if client.grpcConnectorFactory != nil {
		uncheckedClient, transport, err := client.grpcConnectorFactory()
		if err != nil {
			return nil, err
		}

		grpcClient, ok := uncheckedClient.(collectorpb.CollectorServiceClient)
		if !ok {
			return nil, fmt.Errorf("Grpc connector factory did not provide valid client!")
		}

		conn = transport
		client.grpcClient = grpcClient
	} else {
		transport, err := grpc.Dial(client.hostPort, client.dialOptions...)
		if err != nil {
			return nil, err
		}

		conn = transport
		client.grpcClient = collectorpb.NewCollectorServiceClient(transport)
	}
	client.connTimestamp = now
	return conn, nil
}

func (client *grpcCollectorClient) ShouldReconnect() bool {
	return time.Now().Sub(client.connTimestamp) > client.reconnectPeriod
}

func (client *grpcCollectorClient) Report(ctx context.Context, buffer *reportBuffer) (collectorResponse, error) {
	resp, err := client.grpcClient.Report(ctx, client.converter.toReportRequest(
		client.reporterID,
		client.attributes,
		client.accessToken,
		buffer,
	))
	if err != nil {
		return nil, err
	}
	return resp, nil
}
