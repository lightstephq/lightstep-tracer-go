package lightstep

import (
	"time"

	"github.com/opentracing/basictracer-go"
)

type spansBuffer struct {
	rawSpans    []basictracer.RawSpan
	dropped     int64
	reportStart time.Time
	reportEnd   time.Time
}

func newSpansBuffer(size int) (b spansBuffer) {
	b.rawSpans = make([]basictracer.RawSpan, 0, size)
	b.reportStart = time.Time{}
	b.reportEnd = time.Time{}
	return
}

func (b *spansBuffer) isHalfFull() bool {
	return len(b.rawSpans) > cap(b.rawSpans)/2
}

func (b *spansBuffer) setCurrent(now time.Time) {
	b.reportStart = now
	b.reportEnd = now
}

func (b *spansBuffer) setFlushing(now time.Time) {
	b.reportEnd = now
}

func (b *spansBuffer) clear() {
	b.rawSpans = b.rawSpans[:0]
	b.reportStart = time.Time{}
	b.reportEnd = time.Time{}
	b.dropped = 0
}

func (b *spansBuffer) addSpan(span basictracer.RawSpan) {
	if len(b.rawSpans) == cap(b.rawSpans) {
		b.dropped++
		return
	}
	b.rawSpans = append(b.rawSpans, span)
}

func (into *spansBuffer) mergeUnreported(from *spansBuffer) {
	into.dropped += from.dropped
	if from.reportStart.Before(into.reportStart) {
		into.reportStart = from.reportStart
	}
	if from.reportEnd.After(into.reportEnd) {
		into.reportEnd = from.reportEnd
	}

	// Note: Somewhat arbitrarily dropping the spans that won't
	// fit; could be more principled here to avoid bias.
	have := len(into.rawSpans)
	space := cap(into.rawSpans) - have
	unreported := len(from.rawSpans)

	if space > unreported {
		space = unreported
	}

	copy(into.rawSpans[have:], from.rawSpans[0:space])
	into.dropped += int64(unreported - space)

	from.clear()
}
