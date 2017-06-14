package lightstep_test

import (
	"net"
	"time"

	. "github.com/lightstep/lightstep-tracer-go"
	cpb "github.com/lightstep/lightstep-tracer-go/collectorpb"
	cpbfakes "github.com/lightstep/lightstep-tracer-go/collectorpb/collectorpbfakes"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	ot "github.com/opentracing/opentracing-go"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

// func newGrpcServer(port int) (net.Listener, *grpc.Server, error) {
// 	listener, err := net.Listen("tcp", fmt.Sprintf(":%v", port))
// 	if err != nil {
// 		return listener, nil, err
// 	}
// 	return listener, grpc.NewServer(), nil
// }

var _ = Describe("Options", func() {
	const expectedTraceID uint64 = 1
	const expectedSpanID uint64 = 2
	const expectedParentSpanID uint64 = 3
	const port = 9090

	var tracer ot.Tracer
	var grpcServer *grpc.Server
	var fakeCollector *cpbfakes.FakeCollectorServiceServer
	var latestSpans func() []*cpb.Span

	BeforeEach(func() {
		var listener net.Listener
		var err error
		if listener, fakeCollector, grpcServer, err = newGrpcServer(port); err != nil {
			Fail("failed to start grpc server")
		}

		// setup a channel to grab the latest report to the Collector
		reportChan := make(chan *cpb.ReportRequest)
		fakeCollector.ReportStub = func(context context.Context, reportRequest *cpb.ReportRequest) (*cpb.ReportResponse, error) {
			reportChan <- reportRequest
			return nil, nil
		}
		latestSpans = func() []*cpb.Span { return (<-reportChan).GetSpans() }

		go grpcServer.Serve(listener)

		tracer = NewTracer(Options{
			AccessToken:      "0987654321",
			Collector:        Endpoint{"localhost", port, true},
			ReportingPeriod:  1 * time.Millisecond,
			ReportTimeout:    10 * time.Millisecond,
			MaxLogKeyLen:     10,
			MaxLogValueLen:   11,
			MaxBufferedSpans: 10,
		})

		// make sure the grpcServer is serving
		Eventually(func() int {
			return fakeCollector.ReportCallCount()
		}).ShouldNot(BeZero())
	})

	AfterEach(func() {
		errChan := make(chan error)
		go func() { errChan <- CloseTracer(tracer) }()
		Eventually(errChan).Should(Receive(BeNil()))
		grpcServer.Stop()
	})

	Context("When only the TraceID is set", func() {
		BeforeEach(func() {
			tracer.StartSpan("x", SetTraceID(expectedTraceID)).Finish()
		})

		It("Should set the options appropriately", func() {
			By("Only running one span")
			var spans []*cpb.Span
			Eventually(func() []*cpb.Span {
				spans = latestSpans()
				return spans
			}).Should(HaveLen(1))

			By("Appropriately setting TraceID")
			Expect(spans[0].GetSpanContext().GetTraceId()).To(Equal(expectedTraceID))
			Expect(spans[0].GetSpanContext().GetSpanId()).ToNot(Equal(uint64(0)))
			Expect(spans[0].GetReferences()).To(BeEmpty())
		})
	})

	Context("When both the TraceID and SpanID are set", func() {
		BeforeEach(func() {
			tracer.StartSpan("x", SetTraceID(expectedTraceID), SetSpanID(expectedSpanID)).Finish()
		})

		It("Should set the options appropriately", func() {
			By("Only running one span")
			var spans []*cpb.Span
			Eventually(func() []*cpb.Span {
				spans = latestSpans()
				return spans
			}).Should(HaveLen(1))

			By("Appropriately setting the TraceID and SpanID")
			Expect(spans[0].GetSpanContext().TraceId).To(Equal(expectedTraceID))
			Expect(spans[0].GetSpanContext().SpanId).To(Equal(expectedSpanID))
			Expect(spans[0].GetReferences()).To(BeEmpty())
		})
	})

	Context("When TraceID, SpanID, and ParentSpanID are set", func() {
		BeforeEach(func() {
			tracer.StartSpan("x", SetTraceID(expectedTraceID), SetSpanID(expectedSpanID), SetParentSpanID(expectedParentSpanID)).Finish()
		})

		It("Should set the options appropriately", func() {
			By("Only running one span")
			var spans []*cpb.Span
			Eventually(func() []*cpb.Span {
				spans = latestSpans()
				return spans
			}).Should(HaveLen(1))

			By("Appropriately setting TraceID, SpanID, and ParentSpanID")
			Expect(spans[0].GetSpanContext().TraceId).To(Equal(expectedTraceID))
			Expect(spans[0].GetSpanContext().SpanId).To(Equal(expectedSpanID))
			Expect(spans[0].GetReferences()).ToNot(BeEmpty())
			Expect(spans[0].GetReferences()[0].GetSpanContext().SpanId).To(Equal(expectedParentSpanID))
		})
	})
})
