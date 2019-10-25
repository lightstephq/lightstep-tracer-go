package conversions_test

import (
	"encoding/binary"
	"testing"
	"testing/quick"

	. "github.com/lightstep/lightstep-tracer-go/lightstepoc/internal/conversions"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"go.opencensus.io/trace"
)

func TestConversions(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Conversions Suite")
}

var _ = Describe("#ConvertTraceID", func() {
	It("captures the first 8 bytes of the ID", func() {
		err := quick.Check(func(a uint64, b uint64) bool {
			var traceID trace.TraceID

			binary.BigEndian.PutUint64(traceID[0:8], a)
			binary.BigEndian.PutUint64(traceID[8:], b)

			Expect(ConvertTraceID(traceID)).To(Equal(a))

			return true
		}, nil)

		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = Describe("#ConvertSpanID", func() {
	It("captures the whole ID", func() {
		err := quick.Check(func(a uint64) bool {
			var spanID trace.SpanID

			binary.BigEndian.PutUint64(spanID[:], a)

			Expect(ConvertSpanID(spanID)).To(Equal(a))

			return true
		}, nil)

		Expect(err).NotTo(HaveOccurred())
	})
})
