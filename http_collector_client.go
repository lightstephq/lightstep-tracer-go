package lightstep

import (
	"bytes"
	"github.com/golang/protobuf/proto"
	"github.com/lightstep/lightstep-tracer-go/collectorpb"
	"golang.org/x/net/context"
	"golang.org/x/net/http2"
	"io/ioutil"
	"net/http"
	"time"
)

const (
	collectorHttpMethod                 = "POST"
	collectorHttpPath                   = "/v1/report"
	collectorHttpContentTypeHeaderValue = "application/octet-stream"

	contentTypeHeaderKey = "Content-Type"
)

// grpcCollectorClient specifies how to send reports back to a LightStep
// collector via grpc.
type httpCollectorClient struct {
	// auth and runtime information
	reporterID  uint64
	accessToken string // accessToken is the access token used for explicit trace collection requests.
	attributes  map[string]string

	reportTimeout time.Duration

	// Remote service that will receive reports.
	socketAddress string
	client        *http.Client

	// converters
	converter *protoConverter
}

type transportCloser struct {
	transport http2.Transport
}

func (closer *transportCloser) Close() error {
	closer.transport.CloseIdleConnections()

	return nil
}

func newHttpCollectorClient(opts Options, reporterID uint64, attributes map[string]string) *httpCollectorClient {
	return &httpCollectorClient{
		reporterID:  reporterID,
		accessToken: opts.AccessToken,
		attributes:  attributes,
		reportTimeout: opts.ReportTimeout,
		socketAddress: opts.Collector.HostPort(),
		converter: newProtoConverter(opts),
	}
}

func (client *httpCollectorClient) ConnectClient() (Connection, error) {
	transport := &http2.Transport{}

	client.client = &http.Client{
		Transport: transport,
		Timeout:   client.reportTimeout,
	}

	return &transportCloser{}, nil
}

func (client *httpCollectorClient) ShouldReconnect() bool {
	// http2 will handle connection reuse under the hood
	return false
}

func (client *httpCollectorClient) Report(context context.Context, buffer *reportBuffer) (collectorResponse, error) {
	httpRequest, err := client.toRequest(context, buffer)
	if err != nil {
		return nil, err
	}

	httpResponse, err := client.client.Do(httpRequest)
	if err != nil {
		return nil, err
	}
	defer httpResponse.Body.Close()

	response, err := client.toResponse(httpResponse)
	if err != nil {
		return nil, err
	}

	return response, nil
}

func (client *httpCollectorClient) toRequest(
	context context.Context,
	buffer *reportBuffer,
) (*http.Request, error) {
	protoRequest := client.converter.toReportRequest(
		client.reporterID,
		client.attributes,
		client.accessToken,
		buffer,
	)

	buf, err := proto.Marshal(protoRequest)
	if err != nil {
		return nil, err
	}

	requestBody := bytes.NewReader(buf)

	request, err := http.NewRequest(collectorHttpMethod, client.socketAddress+collectorHttpPath, requestBody)
	if err != nil {
		return nil, err
	}
	request = request.WithContext(context)
	request.Header.Set(contentTypeHeaderKey, collectorHttpContentTypeHeaderValue)

	return request, nil
}

func (client *httpCollectorClient) toResponse(response *http.Response) (collectorResponse, error) {
	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	protoResponse := &collectorpb.ReportResponse{}
	if err := proto.Unmarshal(body, protoResponse); err != nil {
		return nil, err
	}

	return protoResponse, nil
}
