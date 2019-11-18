package lightstep_test

import (
	. "github.com/lightstep/lightstep-tracer-go"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/opentracing/opentracing-go"
)

var _ = Describe("Propagator Stack", func() {
	var stack PropagatorStack
	Context("With no propagators", func() {
		var knownCarrier1 opentracing.TextMapCarrier

		var knownContext1 = SpanContext{
			SpanID:  6397081719746291766,
			TraceID: 506100417967962170,
			Baggage: map[string]string{"checked": "baggage"},
		}
		BeforeEach(func() {
			stack = PropagatorStack{}
		})

		It("should return error on inject", func() {
			err := stack.Inject(knownContext1, knownCarrier1)
			Expect(err).ToNot(BeNil())
		})

		It("should return error on extract", func() {
			_, err := stack.Extract(knownCarrier1)
			Expect(err).ToNot(BeNil())
		})
	})
	Context("With one propagator", func() {
		var knownCarrier1 opentracing.TextMapCarrier

		var knownContext1 = SpanContext{
			SpanID:  6397081719746291766,
			TraceID: 506100417967962170,
			Baggage: map[string]string{"checked": "baggage"},
		}
		BeforeEach(func() {
			knownCarrier1 = opentracing.TextMapCarrier{}
			stack = PropagatorStack{}
			stack.PushPropagator(LightStepPropagator)
		})

		It("should inject trace", func() {
			err := stack.Inject(knownContext1, knownCarrier1)
			Expect(err).To(BeNil())
			Expect(len(knownCarrier1)).To(Equal(4))
			Expect(knownCarrier1["ot-tracer-traceid"]).To(Equal("70607a611a8383a"))
			Expect(knownCarrier1["ot-tracer-spanid"]).To(Equal("58c6ffee509f6836"))
			Expect(knownCarrier1["ot-tracer-sampled"]).To(Equal("true"))
			Expect(knownCarrier1["ot-baggage-checked"]).To(Equal("baggage"))
		})

		It("should extract trace", func() {
			knownCarrier2 := opentracing.TextMapCarrier{
				"ot-tracer-traceid":  "70607a611a8383a",
				"ot-tracer-spanid":   "58c6ffee509f6836",
				"ot-tracer-sampled":  "true",
				"ot-baggage-checked": "baggage",
			}
			ctx, err := stack.Extract(knownCarrier2)
			Expect(err).To(BeNil())
			// check if spancontext is correct
			spanContext, _ := ctx.(SpanContext)

			Expect(spanContext.TraceID).To(Equal(knownContext1.TraceID))
			Expect(spanContext.SpanID).To(Equal(knownContext1.SpanID))
			Expect(spanContext.Baggage).To(Equal(knownContext1.Baggage))
		})
	})
	Context("With multiple propagator", func() {
		var knownCarrier1 opentracing.TextMapCarrier

		var knownContext1 = SpanContext{
			SpanID:  6397081719746291766,
			TraceID: 506100417967962170,
			Baggage: map[string]string{"checked": "baggage"},
		}
		BeforeEach(func() {
			knownCarrier1 = opentracing.TextMapCarrier{}
			stack = PropagatorStack{}
			stack.PushPropagator(LightStepPropagator)
			stack.PushPropagator(B3Propagator)
		})

		It("should inject trace using both propagators", func() {
			err := stack.Inject(knownContext1, knownCarrier1)
			Expect(err).To(BeNil())
			Expect(len(knownCarrier1)).To(Equal(7))

			Expect(knownCarrier1["ot-tracer-traceid"]).To(Equal("70607a611a8383a"))
			Expect(knownCarrier1["ot-tracer-spanid"]).To(Equal("58c6ffee509f6836"))
			Expect(knownCarrier1["ot-tracer-sampled"]).To(Equal("true"))
			Expect(knownCarrier1["x-b3-traceid"]).To(Equal("70607a611a8383a"))
			Expect(knownCarrier1["x-b3-spanid"]).To(Equal("58c6ffee509f6836"))
			Expect(knownCarrier1["x-b3-sampled"]).To(Equal("1"))
			Expect(knownCarrier1["ot-baggage-checked"]).To(Equal("baggage"))
		})

		It("should extract trace", func() {
			knownCarrier2 := opentracing.TextMapCarrier{
				"ot-tracer-traceid":  "70607a611a8383a",
				"ot-tracer-spanid":   "58c6ffee509f6836",
				"ot-tracer-sampled":  "true",
				"ot-baggage-checked": "baggage",
			}
			ctx, err := stack.Extract(knownCarrier2)
			Expect(err).To(BeNil())
			spanContext, _ := ctx.(SpanContext)

			Expect(spanContext.TraceID).To(Equal(knownContext1.TraceID))
			Expect(spanContext.SpanID).To(Equal(knownContext1.SpanID))
			Expect(spanContext.Baggage).To(Equal(knownContext1.Baggage))

			knownCarrier3 := opentracing.TextMapCarrier{
				"x-b3-traceid":       "70607a611a8383a",
				"x-b3-spanid":        "58c6ffee509f6836",
				"x-b3-sampled":       "true",
				"ot-baggage-checked": "baggage",
			}
			ctx, err = stack.Extract(knownCarrier3)
			Expect(err).To(BeNil())
			spanContext, _ = ctx.(SpanContext)

			Expect(spanContext.TraceID).To(Equal(knownContext1.TraceID))
			Expect(spanContext.SpanID).To(Equal(knownContext1.SpanID))
			Expect(spanContext.Baggage).To(Equal(knownContext1.Baggage))
		})
	})
})
