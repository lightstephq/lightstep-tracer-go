package lightstep_test

import (
	. "github.com/lightstep/lightstep-tracer-go"

	cpbfakes "github.com/lightstep/lightstep-tracer-go/collectorpb/collectorpbfakes"
	"github.com/lightstep/lightstep-tracer-go/lightstepfakes"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("SpanRecorder", func() {
	var tracer Tracer

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
