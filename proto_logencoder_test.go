package lightstep

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/lightstep/lightstep-tracer-common/golang/gogo/collectorpb"
	"github.com/opentracing/opentracing-go/log"
	"github.com/stretchr/testify/assert"
)

func TestLogFieldEncoding(t *testing.T) {
	options := Options{}
	converter := newProtoConverter(options)
	buffer := newSpansBuffer(1)

	out := &collectorpb.Log{}
	in := []log.Field{
		log.String("", ""),
		log.Int("", 0),

		log.String("string:Hello", "Hello"),
		log.String("string:", ""),

		log.Bool("bool:false", false),
		log.Bool("bool:true", true),

		log.Int("int:1", 1),
		log.Int("int:-1", -1),

		log.Int32("int:2147483647", math.MaxInt32),
		log.Int32("int:0", 0),
		log.Int32("int:10", 10),
		log.Int32("int:-10", -10),
		log.Int32("int:-2147483648", math.MinInt32),

		log.Int64("int:2147483647", math.MaxInt32),
		log.Int64("int:-2147483648", math.MinInt32),
		log.Int64("int:9223372036854775807", math.MaxInt64),
		log.Int64("int:-9223372036854775808", math.MinInt64),

		// Note: unsigned integers are currently encoded as strings.  LS-1175
		log.Uint32("string:0", 0),
		log.Uint32("string:10", 10),
		log.Uint32("string:2147483647", math.MaxInt32),
		log.Uint32("string:4294967295", math.MaxUint32),

		log.Uint64("string:0", 0),
		log.Uint64("string:10", 10),
		log.Uint64("string:2147483647", math.MaxInt32),
		log.Uint64("string:2147483648", math.MaxInt32+1),
		log.Uint64("string:4294967295", math.MaxUint32),

		log.Uint64("string:18446744073709551615", math.MaxUint64),

		log.Object("json:{}", struct{}{}),
	}

	marshalFields(converter, out, in, &buffer)

	if !assert.Equal(t, int64(0), buffer.logEncoderErrorCount) {
		return

	}

	for idx, inf := range in {
		comp := out.Fields[idx]

		// Make sure empty keys don't crash.
		if len(inf.Key()) == 0 {
			if !assert.Equal(t, "", comp.Key) {
				return
			}
			continue
		}

		insplit := strings.SplitN(inf.Key(), ":", 2)
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

		if !assert.Equal(t, expect, fmt.Sprint(value)) {
			return
		}
	}
}
