package lightstep

import (
	"context"
	"errors"
	"fmt"

	"github.com/lightstep/lightstep-tracer-common/golang/gogo/collectorpb"
	"github.com/lightstep/lightstep-tracer-common/golang/gogo/collectorpb/collectorpbfakes"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("TracerImpl", func() {
	var tracer *tracerImpl
	var opts Options

	const accessToken = "ACCESS_TOKEN"

	var fakeClient *collectorpbfakes.FakeCollectorServiceClient
	var fakeConn ConnectorFactory

	var eventHandler func(Event)
	var eventChan <-chan Event
	const eventBufferSize = 10

	BeforeEach(func() {
		fakeClient = new(collectorpbfakes.FakeCollectorServiceClient)
		fakeClient.ReportReturns(&collectorpb.ReportResponse{}, nil)
		fakeConn = fakeGrpcConnection(fakeClient)

		eventHandler, eventChan = NewEventChannel(eventBufferSize)
		SetGlobalEventHandler(eventHandler)

		opts = Options{
			AccessToken: accessToken,
			ConnFactory: fakeConn,
		}
	})

	JustBeforeEach(func() {
		ottracer := NewTracer(opts)
		tracer = ottracer.(*tracerImpl)
	})

	Describe("Flush", func() {
		Context("when the client fails to report the buffer", func() {
			JustBeforeEach(func() {
				for i := 0; i < 10; i++ {
					tracer.StartSpan(fmt.Sprint("span ", i)).Finish()
				}

				fakeClient := newFakeCollectorClient(tracer.client)
				fakeClient.report = func(_ context.Context, _ *collectorpb.ReportRequest) (collectorResponse, error) {
					return protoResponse{}, errors.New("report failed")
				}

				tracer.client = fakeClient
			})
			It("should emit an EventFlushError", func(done Done) {
				tracer.Flush(context.Background())

				err := <-eventChan
				flushErr, ok := err.(EventFlushError)
				Expect(ok).To(BeTrue())

				Expect(flushErr.State()).To(Equal(FlushErrorTransport))
				close(done)
			})
			It("should clear the flushing buffer", func() {
				Expect(len(tracer.buffer.rawSpans)).To(Equal(10))
				tracer.Flush(context.Background())
				Expect(len(tracer.flushing.rawSpans)).To(Equal(0))
			})
		})
	})
})

type dummyConnection struct{}

func (*dummyConnection) Close() error { return nil }

func fakeGrpcConnection(fakeClient *collectorpbfakes.FakeCollectorServiceClient) ConnectorFactory {
	return func() (interface{}, Connection, error) {
		return fakeClient, new(dummyConnection), nil
	}
}

type fakeCollectorClient struct {
	realClient      collectorClient
	report          func(context.Context, *collectorpb.ReportRequest) (collectorResponse, error)
	connectClient   func() (Connection, error)
	shouldReconnect func() bool
}

func newFakeCollectorClient(client collectorClient) *fakeCollectorClient {
	return &fakeCollectorClient{
		realClient:      client,
		report:          client.Report,
		connectClient:   client.ConnectClient,
		shouldReconnect: client.ShouldReconnect,
	}
}

func (f *fakeCollectorClient) Report(ctx context.Context, r *collectorpb.ReportRequest) (collectorResponse, error) {
	return f.report(ctx, r)
}
func (f *fakeCollectorClient) ConnectClient() (Connection, error) {
	return f.connectClient()
}
func (f *fakeCollectorClient) ShouldReconnect() bool {
	return f.shouldReconnect()
}
