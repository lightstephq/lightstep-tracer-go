package lightstep_test

import (
	"github.com/lightstep/lightstep-tracer-common/golang/gogo/collectorpb"
	. "github.com/lightstep/lightstep-tracer-go"
)

type cpbSpan struct {
	collectorpb.Span
}

type cpbReference struct {
	collectorpb.Reference
}

func (fakeClient *cpbfakesFakeClient) GetSpan(i int) Span {
	return &cpbSpan{
		Span: *fakeClient.getSpans()[i],
	}
}

func (span *cpbSpan) GetSpanContext() SpanContext {
	return toProtoSpanContext(span.Span.GetSpanContext())
}

func (span *cpbSpan) GetTags() interface{} {
	return span.Span.GetTags()
}

func (span *cpbSpan) GetReferences() interface{} {
	return span.Span.GetReferences()
}

func (span *cpbSpan) GetReference(i int) Reference {
	return &cpbReference{
		Reference: *span.Span.GetReferences()[i],
	}
}

func (reference *cpbReference) GetSpanContext() SpanContext {
	return toProtoSpanContext(reference.Reference.GetSpanContext())
}

func (span *cpbSpan) GetLogs() []interface{} {
	logs := make([]interface{}, 0, len(span.Span.GetLogs()))
	for _, log := range span.Span.GetLogs() {
		logs = append(logs, log)
	}
	return logs
}

func toProtoSpanContext(spanContext *collectorpb.SpanContext) SpanContext {
	return SpanContext{
		TraceID: spanContext.TraceId,
		SpanID:  spanContext.SpanId,
		Baggage: spanContext.Baggage,
	}
}
