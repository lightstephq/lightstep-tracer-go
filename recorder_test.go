package lightstep

import (
	"testing"

	"github.com/opentracing/basictracer-go"
)

func makeSpanSlice(length int) []basictracer.RawSpan {
	spans := make([]basictracer.RawSpan, length)
	for i := range spans {
		spans[i].SpanContext = &basictracer.SpanContext{}
	}
	return spans
}

func TestMaxBufferSize(t *testing.T) {
	recorder := NewRecorder(Options{
		AccessToken: "0987654321",
	}).(*Recorder)

	checkCapSize := func(spanLen, spanCap int) {
		recorder.lock.Lock()
		defer recorder.lock.Unlock()

		if recorder.buffer.cap() != spanCap {
			t.Error("Unexpected buffer size")
		}
		if recorder.buffer.len() != spanLen {
			t.Error("Unexpected buffer size")
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
	recorder = NewRecorder(Options{
		AccessToken:      "0987654321",
		MaxBufferedSpans: maxBuffer,
	}).(*Recorder)

	checkCapSize(0, maxBuffer)

	spans = append(spans, makeSpanSlice(100*defaultMaxSpans)...)
	for _, span := range spans {
		recorder.RecordSpan(span)
	}

	checkCapSize(maxBuffer, maxBuffer)
}
