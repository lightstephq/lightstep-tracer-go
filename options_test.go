package lightstep

import (
	"github.com/lightstep/lightstep-tracer-go/basictracer"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	opentracing "github.com/opentracing/opentracing-go"
)

var _ = Describe("Options", func() {
	const (
		expectedTraceID      uint64 = 1
		expectedSpanID       uint64 = 2
		expectedParentSpanID uint64 = 3
	)

	var (
		recorder *basictracer.InMemorySpanRecorder
		tracer   opentracing.Tracer
	)

	BeforeEach(func() {
		recorder = basictracer.NewInMemoryRecorder()
		tracer = basictracer.NewWithOptions(basictracer.Options{Recorder: recorder})
	})

	Context("When only the TraceID is set", func() {
		BeforeEach(func() {
			tracer.StartSpan("x", SetTraceID(expectedTraceID)).Finish()
		})

		It("Should set the options appropriately", func() {
			By("Only running one span")
			spans := recorder.GetSpans()
			Expect(len(spans)).To(Equal(1))

			By("Appropriately setting TraceID")
			span := spans[0]
			Expect(span.Context.TraceID).To(Equal(expectedTraceID))
			Expect(span.Context.SpanID).ToNot(Equal(uint64(0)))
			Expect(span.ParentSpanID).To(Equal(uint64(0)))
		})
	})

	Context("When both the TraceID and SpanID are set", func() {
		BeforeEach(func() {
			tracer.StartSpan("x", SetTraceID(expectedTraceID), SetSpanID(expectedSpanID)).Finish()
		})

		It("Should set the options appropriately", func() {
			By("Only running one span")
			spans := recorder.GetSpans()
			Expect(len(spans)).To(Equal(1))

			By("Appropriately setting the TraceID and SpanID")
			span := spans[0]
			Expect(span.Context.TraceID).To(Equal(expectedTraceID))
			Expect(span.Context.SpanID).To(Equal(expectedSpanID))
			Expect(span.ParentSpanID).To(Equal(uint64(0)))
		})
	})

	Context("When TraceID, SpanID, and ParentSpanID are set", func() {
		BeforeEach(func() {
			tracer.StartSpan("x", SetTraceID(expectedTraceID), SetSpanID(expectedSpanID), SetParentSpanID(expectedParentSpanID)).Finish()
		})

		It("Should set the options appropriately", func() {
			By("Only running one span")
			spans := recorder.GetSpans()
			Expect(len(spans)).To(Equal(1))

			By("Appropriately setting TraceID, SpanID, and ParentSpanID")
			span := spans[0]
			Expect(span.Context.TraceID).To(Equal(expectedTraceID))
			Expect(span.Context.SpanID).To(Equal(expectedSpanID))
			Expect(span.ParentSpanID).To(Equal(expectedParentSpanID))
		})
	})
})
