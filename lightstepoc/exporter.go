package lightstepoc

import (
	"context"
	"errors"

	"github.com/lightstep/lightstep-tracer-go"
	"github.com/lightstep/lightstep-tracer-go/lightstepoc/internal/conversions"
	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
	"github.com/opentracing/opentracing-go/log"
	"go.opencensus.io/trace"
)

var (
	// ErrFailedToCreateExporter indicates that the underlying tracer could not be created,
	// but the root reason is not known.
	ErrFailedToCreateExporter = errors.New("lightstepoc: failed to create exporter")
)

// Exporter may be registered with OpenCensus so that span data can be exported to LightStep
type Exporter struct {
	tracer lightstep.Tracer
}

// NewExporter creates a new Exporter.
// It returns an error if the underlying tracer could not be created, e.g., due to invalid options
func NewExporter(opts ...Option) (*Exporter, error) {
	c := defaultConfig()
	for _, opt := range opts {
		opt(c)
	}

	tracer := lightstep.NewTracer(c.tracerOptions)
	if tracer == nil {
		if err := c.tracerOptions.Validate(); err != nil {
			return nil, err
		}
		return nil, ErrFailedToCreateExporter
	}

	return &Exporter{
		tracer: tracer,
	}, nil
}

// ExportSpan queues the span to be sent LightStep.
// Spans are typically batched for performance reasons. Call `Exporter#Flush` to send all queued spans.
func (e *Exporter) ExportSpan(sd *trace.SpanData) {
	opts := []opentracing.StartSpanOption{
		opentracing.StartTime(sd.StartTime),
	}

	if traceID, ok := conversions.ConvertTraceID(sd.SpanContext.TraceID); ok {
		opts = append(opts, lightstep.SetTraceID(traceID))
	}

	if spanID, ok := conversions.ConvertSpanID(sd.SpanContext.SpanID); ok {
		opts = append(opts, lightstep.SetSpanID(spanID))
	}

	if parentSpanID, ok := conversions.ConvertSpanID(sd.ParentSpanID); ok {
		opts = append(opts, lightstep.SetParentSpanID(parentSpanID))
	}

	for _, link := range sd.Links {
		switch link.Type {
		case trace.LinkTypeChild:
			spanContext := conversions.ConvertLinkToSpanContext(link)
			opts = append(opts, opentracing.ChildOf(spanContext))
		}
	}

	switch sd.SpanKind {
	case trace.SpanKindServer:
		opts = append(opts, ext.SpanKindRPCServer)
	case trace.SpanKindClient:
		opts = append(opts, ext.SpanKindRPCClient)
	}

	span := e.tracer.StartSpan(sd.Name, opts...)

	for _, entry := range sd.SpanContext.Tracestate.Entries() {
		span.SetBaggageItem(entry.Key, entry.Value)
	}

	ext.HTTPStatusCode.Set(span, uint16(sd.Status.Code))

	for k, v := range sd.Attributes {
		span.SetTag(k, v)
	}

	var logRecords []opentracing.LogRecord
	for _, annotation := range sd.Annotations {
		logRecords = append(logRecords, opentracing.LogRecord{
			Timestamp: annotation.Time,
			Fields:    []log.Field{log.Object(annotation.Message, annotation.Attributes)},
		})
	}

	span.FinishWithOptions(opentracing.FinishOptions{
		FinishTime: sd.EndTime,
		LogRecords: logRecords,
	})
}

// Flush sends all buffered spans to LightStep
func (e *Exporter) Flush(ctx context.Context) {
	e.tracer.Flush(ctx)
}

// Close flushes all buffered spans and then kills open connections to LightStep, releasing resources
func (e *Exporter) Close(ctx context.Context) {
	e.tracer.Close(ctx)
}
