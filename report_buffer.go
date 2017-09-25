package lightstep

import (
	"time"
)

type droppedSpans struct {
	droppedSpanCountTotal                             int64
	droppedSpanCountBufferFull                        int64
	droppedSpanCountBufferFullAndTranslationInProcess int64
	droppedSpanCountDeadlineExceeded                  int64
	droppedSpanCountServer                            int64
	droppedSpanCountClient                            int64
	droppedSpanCountUnknown                           int64
	droppedSpanCountThrift                            int64
}

type reportBuffer struct {
	rawSpans             []RawSpan
	logEncoderErrorCount int64
	droppedSpans
	reportStart time.Time
	reportEnd   time.Time
}

func newSpansBuffer(size int) (b reportBuffer) {
	b.rawSpans = make([]RawSpan, 0, size)
	b.reportStart = time.Time{}
	b.reportEnd = time.Time{}
	return
}

func (b *reportBuffer) isHalfFull() bool {
	return len(b.rawSpans) > cap(b.rawSpans)/2
}

func (b *reportBuffer) setCurrent(now time.Time) {
	b.reportStart = now
	b.reportEnd = now
}

func (b *reportBuffer) setFlushing(now time.Time) {
	b.reportEnd = now
}

func (b *reportBuffer) clear() {
	b.rawSpans = b.rawSpans[:0]
	b.reportStart = time.Time{}
	b.reportEnd = time.Time{}
	b.droppedSpanCountTotal = 0
	b.droppedSpanCountBufferFull = 0
	b.droppedSpanCountBufferFullAndTranslationInProcess = 0
	b.droppedSpanCountServer = 0
	b.droppedSpanCountClient = 0
	b.droppedSpanCountDeadlineExceeded = 0
	b.logEncoderErrorCount = 0
}

func (b *reportBuffer) addSpan(span RawSpan) {
	if len(b.rawSpans) == cap(b.rawSpans) {
		b.droppedSpanCountBufferFull++
		return
	}
	b.rawSpans = append(b.rawSpans, span)
}

func mergeDroppedSpans(a droppedSpans, b droppedSpans) droppedSpans {
	return droppedSpans{
		droppedSpanCountTotal:                             a.droppedSpanCountTotal + b.droppedSpanCountTotal,
		droppedSpanCountBufferFull:                        a.droppedSpanCountBufferFull + b.droppedSpanCountBufferFull,
		droppedSpanCountBufferFullAndTranslationInProcess: a.droppedSpanCountBufferFullAndTranslationInProcess + b.droppedSpanCountBufferFullAndTranslationInProcess,
		droppedSpanCountServer:                            a.droppedSpanCountServer + b.droppedSpanCountServer,
		droppedSpanCountClient:                            a.droppedSpanCountClient + b.droppedSpanCountClient,
		droppedSpanCountUnknown:                           a.droppedSpanCountUnknown + b.droppedSpanCountUnknown,
		droppedSpanCountDeadlineExceeded:                  a.droppedSpanCountDeadlineExceeded + b.droppedSpanCountDeadlineExceeded,
		droppedSpanCountThrift:                            a.droppedSpanCountThrift + b.droppedSpanCountThrift,
	}
}

// mergeFrom combines the spans and metadata in `from` with `into`,
// returning with `from` empty and `into` having a subset of the
// combined data.
func (into *reportBuffer) mergeFrom(from *reportBuffer, err error) {
	into.droppedSpans = mergeDroppedSpans(into.droppedSpans, from.droppedSpans)
	into.logEncoderErrorCount += from.logEncoderErrorCount
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

	into.rawSpans = append(into.rawSpans, from.rawSpans[0:space]...)

	into.droppedSpanCount += int64(unreported - space)

	from.clear()
}
