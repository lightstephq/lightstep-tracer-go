package lightstep_test

import (
	cpb "github.com/lightstep/lightstep-tracer-common/golang/protobuf/collectorpb"
	cpbfakes "github.com/lightstep/lightstep-tracer-common/golang/protobuf/collectorpb/collectorpbfakes"
	. "github.com/lightstep/lightstep-tracer-go"
)

type cpbfakesFakeClient struct {
	cpbfakes.FakeCollectorServiceClient
}

func newGrpcFakeClient() fakeCollectorClient {
	fakeClient := new(cpbfakes.FakeCollectorServiceClient)
	fakeClient.ReportReturns(&cpb.ReportResponse{}, nil)
	return &cpbfakesFakeClient{FakeCollectorServiceClient: *fakeClient}
}

func (fakeClient *cpbfakesFakeClient) ConnectorFactory() ConnectorFactory {
	return fakeGrpcConnection(&fakeClient.FakeCollectorServiceClient)
}

func (fakeClient *cpbfakesFakeClient) getSpans() []*cpb.Span {
	return getReportedGRPCSpans(&fakeClient.FakeCollectorServiceClient)
}

func (fakeClient *cpbfakesFakeClient) GetSpansLen() int {
	return len(fakeClient.getSpans())
}
