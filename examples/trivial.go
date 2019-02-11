// A trivial LightStep Go tracer example.
//
// $ go build -o lightstep_trivial github.com/lightstep/lightstep-tracer-go/examples/trivial
// $ ./lightstep_trivial --access_token=YOUR_ACCESS_TOKEN

package main

import (
	"context"
	"flag"
	"fmt"
	logger "log"
	"time"

	"github.com/lightstep/lightstep-tracer-go"
	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/log"
)

var accessToken = flag.String("access_token", "", "your LightStep access token")

func subRoutine(ctx context.Context) {
	trivialSpan, _ := opentracing.StartSpanFromContext(ctx, "test span")
	defer trivialSpan.Finish()
	trivialSpan.LogEvent("logged something")
	trivialSpan.LogFields(log.String("string_key", "some string value"), log.Object("trivialSpan", trivialSpan))

	subSpan := opentracing.StartSpan(
		"child span", opentracing.ChildOf(trivialSpan.Context()))
	trivialSpan.LogFields(log.Int("int_key", 42), log.Object("subSpan", subSpan),
		log.String("time.eager", fmt.Sprint(time.Now())),
		log.Lazy(func(fv log.Encoder) {
			fv.EmitString("time.lazy", fmt.Sprint(time.Now()))
		}))
	defer subSpan.Finish()
}

type LoggingRecorder struct {
	r lightstep.SpanRecorder
}

func (r *LoggingRecorder) RecordSpan(span lightstep.RawSpan) {
	logger.Printf("span traceID: %v spanID: %v parentID: %v Operation: %v \n", span.Context.TraceID, span.Context.SpanID, span.ParentSpanID, span.Operation)
}

func main() {
	loggableRecorder := &LoggingRecorder{}

	// Use LightStep as the global OpenTracing Tracer.
	opentracing.InitGlobalTracer(lightstep.NewTracer(lightstep.Options{
		AccessToken: *accessToken,
		Collector:   lightstep.Endpoint{Host: "localhost", Port: 8360, Plaintext: true},
		UseGRPC:     true,
		Recorder:    loggableRecorder,
	}))

	// Do something that's traced.
	subRoutine(context.Background())

	// Force a flush before exit.
	lightstep.Flush(context.Background(), opentracing.GlobalTracer())
}
