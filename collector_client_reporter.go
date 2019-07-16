package lightstep

import (
	"context"
	"io/ioutil"

	"github.com/lightstep/lightstep-tracer-common/golang/gogo/collectorpb"
)

func newCustomCollector(opts Options) *customCollectorClient {
	return &customCollectorClient{collector: opts.CustomCollector}
}

type customCollectorClient struct {
	collector Collector
}

func (client *customCollectorClient) Report(ctx context.Context, req *collectorpb.ReportRequest) (collectorResponse, error) {
	resp, err := client.collector.Report(ctx, req)
	if err != nil {
		return nil, err
	}
	return protoResponse{ReportResponse: resp}, nil
}

func (customCollectorClient) ConnectClient() (Connection, error) {
	return ioutil.NopCloser(nil), nil
}

func (customCollectorClient) ShouldReconnect() bool {
	return false
}
