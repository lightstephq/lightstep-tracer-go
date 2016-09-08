package lightstep

import (
	"testing"

	"github.com/lightstep/lightstep-tracer-go/thrift_rpc"
	"github.com/opentracing/basictracer-go"
)

func makeSpanSlice(length int) []basictracer.RawSpan {
	return make([]basictracer.RawSpan, length)
}

func TestMaxBufferSize(t *testing.T) {
	recorder := NewTracer(Options{
		AccessToken: "0987654321",
		UseGRPC:     true,
	}).(basictracer.Tracer).Options().Recorder.(*Recorder)

	checkCapSize := func(spanLen, spanCap int) {
		recorder.lock.Lock()
		defer recorder.lock.Unlock()

		if cap(recorder.buffer.rawSpans) != spanCap {
			t.Errorf("Unexpected buffer cap: %v != %v", cap(recorder.buffer.rawSpans), spanCap)
		}
		if len(recorder.buffer.rawSpans) != spanLen {
			t.Errorf("Unexpected buffer size: %v != %v", len(recorder.buffer.rawSpans), spanLen)
		}
	}

	checkCapSize(0, defaultMaxSpans)

	spans := makeSpanSlice(defaultMaxSpans)
	for _, span := range spans {
		recorder.RecordSpan(span)
	}

	checkCapSize(defaultMaxSpans, defaultMaxSpans)

	spans = append(spans, makeSpanSlice(defaultMaxSpans)...)
	for _, span := range spans {
		recorder.RecordSpan(span)
	}

	checkCapSize(defaultMaxSpans, defaultMaxSpans)

	maxBuffer := 10
	recorder = NewTracer(Options{
		AccessToken:      "0987654321",
		MaxBufferedSpans: maxBuffer,
		UseGRPC:          true,
	}).(basictracer.Tracer).Options().Recorder.(*Recorder)

	checkCapSize(0, maxBuffer)

	spans = append(spans, makeSpanSlice(100*defaultMaxSpans)...)
	for _, span := range spans {
		recorder.RecordSpan(span)
	}

	checkCapSize(maxBuffer, maxBuffer)

	_ = NewTracer(Options{
		AccessToken: "0987654321",
		UseGRPC:     false,
	}).(basictracer.Tracer).Options().Recorder.(*thrift_rpc.Recorder)
}
