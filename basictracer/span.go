package basictracer

import (
	"time"

	opentracing "github.com/opentracing/opentracing-go"
)

// Span provides access to the essential details of the span, for use
// by basictracer consumers.  These methods may only be called prior
// to (*opentracing.Span).Finish().
type Span interface {
	opentracing.Span

	// Operation names the work done by this span instance
	Operation() string

	// Start indicates when the span began
	Start() time.Time
}
