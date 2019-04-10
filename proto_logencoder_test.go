package lightstep

import (
	"fmt"
	"math"
	"strings"

	"github.com/lightstep/lightstep-tracer-common/golang/gogo/collectorpb"
	"github.com/opentracing/opentracing-go/log"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("ProtoLogEncoderTest", func() {
	It("encodes empty keys correctly", func(done Done) {
		check(log.String("", ""))
		check(log.Int("", 0))
		close(done)
	})

	It("encodes string-value keys correctly", func(done Done) {
		check(log.String("string:Hello", "Hello"))
		check(log.String("string:", ""))
		close(done)
	})

	It("encodes bool-value keys correctly", func(done Done) {
		check(log.Bool("bool:false", false))
		check(log.Bool("bool:true", true))
		close(done)
	})

	It("encodes int-value keys correctly", func(done Done) {
		check(log.Int("int:1", 1))
		check(log.Int("int:-1", -1))
		close(done)
	})

	It("encodes int32-value keys correctly", func(done Done) {
		check(log.Int32("int:2147483647", math.MaxInt32))
		check(log.Int32("int:0", 0))
		check(log.Int32("int:10", 10))
		check(log.Int32("int:-10", -10))
		check(log.Int32("int:-2147483648", math.MinInt32))
		close(done)
	})

	It("encodes int64-value keys correctly", func(done Done) {
		check(log.Int64("int:2147483647", math.MaxInt32))
		check(log.Int64("int:-2147483648", math.MinInt32))
		check(log.Int64("int:9223372036854775807", math.MaxInt64))
		check(log.Int64("int:-9223372036854775808", math.MinInt64))
		close(done)
	})

	It("encodes uint32-value keys correctly", func(done Done) {
		// Note: unsigned integers are currently encoded as strings.  LS-117)
		check(log.Uint32("string:0", 0))
		check(log.Uint32("string:10", 10))
		check(log.Uint32("string:2147483647", math.MaxInt32))
		check(log.Uint32("string:4294967295", math.MaxUint32))
		close(done)
	})

	It("encodes uint64-value keys correctly", func(done Done) {
		check(log.Uint64("string:0", 0))
		check(log.Uint64("string:10", 10))
		check(log.Uint64("string:2147483647", math.MaxInt32))
		check(log.Uint64("string:2147483648", math.MaxInt32+1))
		check(log.Uint64("string:4294967295", math.MaxUint32))
		check(log.Uint64("string:18446744073709551615", math.MaxUint64))
		close(done)
	})

	It("encodes object-value keys correctly", func(done Done) {
		check(log.Object("json:{}", struct{}{}))
		close(done)
	})

})

func check(f log.Field) {
	options := Options{}
	converter := newProtoConverter(options)
	buffer := newSpansBuffer(1)

	out := &collectorpb.Log{}

	marshalFields(converter, out, []log.Field{f}, &buffer)

	Expect(buffer.logEncoderErrorCount).To(Equal(int64(0)))

	comp := out.Fields[0]

	Expect(len(out.Fields)).To(Equal(1))

	// Make sure empty keys don't crash.
	if len(f.Key()) == 0 {
		Expect(comp.Key).To(Equal(""))
		return
	}

	insplit := strings.SplitN(f.Key(), ":", 2)
	vtype := insplit[0]
	expect := insplit[1]

	var value interface{}

	switch vtype {
	case "string":
		value = comp.Value.(*collectorpb.KeyValue_StringValue).StringValue
	case "int":
		value = comp.Value.(*collectorpb.KeyValue_IntValue).IntValue
	case "bool":
		value = comp.Value.(*collectorpb.KeyValue_BoolValue).BoolValue
	case "json":
		value = comp.Value.(*collectorpb.KeyValue_JsonValue).JsonValue
	default:
		panic("Invalid type")

	}

	Expect(fmt.Sprint(value)).To(Equal(expect))
}
