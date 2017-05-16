package lightstep

import (
	"github.com/lightstep/lightstep-tracer-go/basictracer"
	opentracing "github.com/opentracing/opentracing-go"
)

type (
	SetSpanID       uint64
	SetTraceID      uint64
	SetParentSpanID uint64
)

// just kidding these aren't real OT start span options
func (sid SetTraceID) Apply(sso *opentracing.StartSpanOptions)      {}
func (sid SetSpanID) Apply(sso *opentracing.StartSpanOptions)       {}
func (sid SetParentSpanID) Apply(sso *opentracing.StartSpanOptions) {}

func (sid SetTraceID) ApplyLS(sso *basictracer.StartSpanOptions) {
	sso.SetTraceID = uint64(sid)
}
func (sid SetSpanID) ApplyLS(sso *basictracer.StartSpanOptions) {
	sso.SetSpanID = uint64(sid)
}
func (sid SetParentSpanID) ApplyLS(sso *basictracer.StartSpanOptions) {
	sso.SetParentSpanID = uint64(sid)
}
