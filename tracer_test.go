package lightstep_test

import (
	"sync"
	"time"

	"google.golang.org/grpc"

	"golang.org/x/net/context"

	. "github.com/lightstep/lightstep-tracer-go"
	cpb "github.com/lightstep/lightstep-tracer-go/collectorpb"
	cpbfakes "github.com/lightstep/lightstep-tracer-go/collectorpb/collectorpbfakes"
	"github.com/lightstep/lightstep-tracer-go/lightstepfakes"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("SpanRecorder", func() {
	var tracer Tracer

	Describe("FlushLightstepTracer", func() {
		var fakeClient *cpbfakes.FakeCollectorServiceClient
		var cancelch chan struct{}
		var startTestch chan bool

		BeforeEach(func() {
			cancelch = make(chan struct{})
			startTestch = make(chan bool)
			fakeClient = new(cpbfakes.FakeCollectorServiceClient)
			fakeClient.ReportReturns(&cpb.ReportResponse{}, nil)
			var once sync.Once
			fakeClient.ReportStub = func(ctx context.Context, req *cpb.ReportRequest, options ...grpc.CallOption) (*cpb.ReportResponse, error) {
				once.Do(func() { startTestch <- true })
				<-cancelch
				return new(cpb.ReportResponse), nil
			}

			tracer = NewTracer(Options{
				AccessToken: "YOU SHALL NOT PASS",
				ConnFactory: fakeGrpcConnection(fakeClient),
			})
		})

		Context("when the tracer is disabled", func() {
			BeforeEach(func() {
				tracer.Disable()
			})

			It("should not record or flush spans", func(done Done) {
				tracer.StartSpan("these spans should not be recorded").Finish()
				tracer.StartSpan("or flushed").Finish()
				tracer.Flush()
				Consistently(startTestch).ShouldNot(Receive())
				close(done)
			})
		})

		It("should retry flushing if a report is in progress", func() {
			Eventually(startTestch).Should(Receive())
			// tracer is now has a report in flight
			tracer.StartSpan("these spans should sit in the buffer").Finish()
			tracer.StartSpan("while the last report is in flight").Finish()

			finishedch := make(chan struct{})
			go func() {
				FlushLightStepTracer(tracer)
				close(finishedch)
			}()
			// flush should wait for the last report to finish
			Consistently(finishedch).ShouldNot(BeClosed())
			// no spans should have been reported yet
			Expect(getReportedGRPCSpans(fakeClient)).Should(HaveLen(0))
			// allow the last report to finish
			close(cancelch)
			// flush should now send a report with the last two spans
			Eventually(finishedch).Should(BeClosed())
			// now the spans that were sitting in the buffer should be reported
			Expect(getReportedGRPCSpans(fakeClient)).Should(HaveLen(2))
		})
	})

	Context("CloseLightstepTracer", func() {
		var fakeClient *cpbfakes.FakeCollectorServiceClient

		BeforeEach(func() {
			fakeClient = new(cpbfakes.FakeCollectorServiceClient)
			fakeClient.ReportReturns(&cpb.ReportResponse{}, nil)

			tracer = NewTracer(Options{
				AccessToken:        "token",
				ConnFactory:        fakeGrpcConnection(fakeClient),
				MinReportingPeriod: 100 * time.Second,
			})
		})

		It("flushes the buffer before closing", func() {
			tracer.StartSpan("span").Finish()
			err := CloseTracer(tracer)
			Expect(err).NotTo(HaveOccurred())
			Expect(fakeClient.ReportCallCount()).To(Equal(1))
			tracer.StartSpan("span2").Finish()
			Consistently(fakeClient.ReportCallCount).Should(Equal(1))
		})
	})

	Context("When tracer has a SpanRecorder", func() {
		var fakeRecorder *lightstepfakes.FakeSpanRecorder

		BeforeEach(func() {
			fakeRecorder = new(lightstepfakes.FakeSpanRecorder)
			tracer = NewTracer(Options{
				AccessToken: "value",
				ConnFactory: fakeGrpcConnection(new(cpbfakes.FakeCollectorServiceClient)),
				Recorder:    fakeRecorder,
			})
		})

		AfterEach(func() {
			closeTestTracer(tracer)
		})

		It("calls RecordSpan after finishing a span", func() {
			tracer.StartSpan("span").Finish()
			Expect(fakeRecorder.RecordSpanCallCount()).ToNot(BeZero())
		})
	})

	Context("When tracer does not have a SpanRecorder", func() {
		BeforeEach(func() {
			tracer = NewTracer(Options{
				AccessToken: "value",
				ConnFactory: fakeGrpcConnection(new(cpbfakes.FakeCollectorServiceClient)),
				Recorder:    nil,
			})
		})

		AfterEach(func() {
			closeTestTracer(tracer)
		})

		It("doesn't call RecordSpan after finishing a span", func() {
			span := tracer.StartSpan("span")
			Expect(span.Finish).ToNot(Panic())
		})
	})

})
