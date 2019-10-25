package conversions

import (
	"encoding/binary"

	"github.com/lightstep/lightstep-tracer-go"
	"go.opencensus.io/trace"
)

func ConvertTraceID(original trace.TraceID) uint64 {
	return binary.BigEndian.Uint64(original[:8])
}

func ConvertSpanID(original trace.SpanID) uint64 {
	return binary.BigEndian.Uint64(original[:])
}

func ConvertLinkToSpanContext(link trace.Link) lightstep.SpanContext {
	spanContext := lightstep.SpanContext{
		Baggage: make(map[string]string),
		TraceID: ConvertTraceID(link.TraceID),
		SpanID:  ConvertSpanID(link.SpanID),
	}

	for k, v := range link.Attributes {
		if attribute, ok := v.(string); ok {
			spanContext.Baggage[k] = attribute
		}
	}

	return spanContext
}
