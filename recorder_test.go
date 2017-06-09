package lightstep_test

import (
	. "github.com/lightstep/lightstep-tracer-go"
	"github.com/lightstep/lightstep-tracer-go/basictracer"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	ot "github.com/opentracing/opentracing-go"
)

func makeSpanSlice(length int) []basictracer.RawSpan {
	return make([]basictracer.RawSpan, length)
}

var _ = Describe("Recorder", func() {
	const (
		arbitraryTimestampSecs = 1473442150
	)

	var (
		tracer ot.Tracer
	)

	Context("When calling Close() twice", func() {
		Context("With grpc enabled", func() {
			BeforeEach(func() {
				tracer = NewTracer(Options{
					AccessToken: "0987654321",
					UseGRPC:     true,
				})
			})
			It("Should not fail", func() {
				err := CloseTracer(tracer)
				Expect(err).NotTo(HaveOccurred())

				err = CloseTracer(tracer)
				Expect(err).NotTo(HaveOccurred())
			})
		})

		Context("With thrift enabled", func() {
			BeforeEach(func() {
				tracer = NewTracer(Options{
					AccessToken: "0987654321",
					UseGRPC:     false,
				})
			})
			It("Should not fail", func() {
				err := CloseTracer(tracer)
				Expect(err).NotTo(HaveOccurred())

				err = CloseTracer(tracer)
				Expect(err).NotTo(HaveOccurred())
			})
		})
	})
})
