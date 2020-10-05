package lightstep_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"

	"github.com/lightstep/lightstep-tracer-common/golang/gogo/collectorpb"
	"github.com/lightstep/lightstep-tracer-common/golang/gogo/collectorpb/collectorpbfakes"
	. "github.com/lightstep/lightstep-tracer-go"
	"github.com/lightstep/lightstep-tracer-go/constants"
	"github.com/lightstep/lightstep-tracer-go/lightstepfakes"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/log"
)

var _ = Describe("Tracer", func() {
	var tracer Tracer
	var opts Options

	const accessToken = "ACCESS_TOKEN"
	var tags = opentracing.Tags{constants.ComponentNameKey: "test-service"}
	var fakeClient *collectorpbfakes.FakeCollectorServiceClient
	var fakeConn ConnectorFactory

	var fakeRecorder *lightstepfakes.FakeSpanRecorder

	var eventHandler func(Event)
	var eventChan <-chan Event
	const eventBufferSize = 10

	BeforeEach(func() {
		opts.UseGRPC = true
		opts.SystemMetrics.Disabled = true

		fakeClient = new(collectorpbfakes.FakeCollectorServiceClient)
		fakeClient.ReportReturns(&collectorpb.ReportResponse{}, nil)
		fakeConn = fakeGrpcConnection(fakeClient)
		fakeRecorder = new(lightstepfakes.FakeSpanRecorder)

		eventHandler, eventChan = NewEventChannel(eventBufferSize)
		SetGlobalEventHandler(eventHandler)
	})

	JustBeforeEach(func() {
		tracer = NewTracer(opts)
	})

	AfterEach(func() {
		closeTestTracer(tracer)
	})

	Describe("New Tracer", func() {
		BeforeEach(func() {
			opts.AccessToken = accessToken
			opts.ConnFactory = fakeConn
		})
		It("should emit a warning when the service name is unset", func() {
			event := <-eventChan
			ems, ok := event.(EventMissingService)
			Expect(ok).To(BeTrue())

			str := ems.String()
			// String should contain the default service name which is the name of this test
			ok = strings.Contains(str, "{lightstep-tracer-go.test}")
			Expect(ok).To(BeTrue())
		})
	})

	Describe("Start Span", func() {
		BeforeEach(func() {
			opts.AccessToken = accessToken
			opts.Tags = tags
			opts.ConnFactory = fakeConn
		})

		It("should start a span than can be finished", func() {
			span := tracer.StartSpan("operation_name")
			if !Expect(span).To(Not(BeNil())) {
				return
			}
			span.Finish()

			tracer.Flush(context.Background())

			if !Expect(fakeClient.ReportCallCount()).To(Equal(1)) {
				return
			}

			_, request, _ := fakeClient.ReportArgsForCall(0)
			Expect(request.Spans).To(HaveLen(1))
		})

		It("should start a span that can be finished twice but only reports once", func() {
			span := tracer.StartSpan("operation_name")
			if !Expect(span).To(Not(BeNil())) {
				return
			}
			span.Finish()
			span.Finish()

			tracer.Flush(context.Background())

			if !Expect(fakeClient.ReportCallCount()).To(Equal(1)) {
				return
			}

			_, request, _ := fakeClient.ReportArgsForCall(0)
			Expect(request.Spans).To(HaveLen(1))
		})

		It("can start a span with references to internal spans", func() {
			otherSpan := tracer.StartSpan("other")

			Expect(func() {
				tracer.StartSpan("test", opentracing.SpanReference{
					ReferencedContext: otherSpan.Context(),
				})
			}).To(Not(Panic()))
		})

		It("can start a span with references to external spans", func() {
			otherTracer := opentracing.NoopTracer{}
			otherSpan := otherTracer.StartSpan("other")

			Expect(func() {
				tracer.StartSpan("test", opentracing.SpanReference{
					ReferencedContext: otherSpan.Context(),
				})
			}).To(Not(Panic()))
		})
	})

	Describe("#Log", func() {
		BeforeEach(func() {
			opts.AccessToken = accessToken
			opts.Tags = tags
			opts.ConnFactory = fakeConn
		})

		It("logs the data", func() {
			span := tracer.StartSpan("test")
			Expect(span).NotTo(BeNil())

			logData := opentracing.LogData{
				Timestamp: time.Now(),
				Event:     "event",
				Payload:   "test",
			}

			span.Log(logData)
			span.Finish()
			tracer.Flush(context.Background())

			Expect(fakeClient.ReportCallCount()).To(Equal(1))

			_, request, _ := fakeClient.ReportArgsForCall(0)
			Expect(request.Spans).To(HaveLen(1))

			logs := request.Spans[0].Logs
			Expect(logs).To(HaveLen(1))
			Expect(logs[0].Fields).To(Equal([]*collectorpb.KeyValue{&collectorpb.KeyValue{
				Key: "event",
				Value: &collectorpb.KeyValue_StringValue{
					StringValue: "event",
				},
			}, &collectorpb.KeyValue{
				Key: "payload",
				Value: &collectorpb.KeyValue_JsonValue{
					JsonValue: `"test"`,
				},
			}}))
		})

		It("can happen concurrently with Flush", func() {
			span := tracer.StartSpan("test")
			Expect(span).NotTo(BeNil())

			wg := &sync.WaitGroup{}
			wg.Add(2)

			span.Log(opentracing.LogData{
				Timestamp: time.Now(),
				Event:     "event",
				Payload:   "test",
			})
			span.Finish()

			go func() {
				defer wg.Done()

				span.Log(opentracing.LogData{
					Timestamp: time.Now(),
					Event:     "another",
					Payload:   "abc",
				})
			}()

			go func() {
				defer wg.Done()

				tracer.Flush(context.Background())
			}()

			wg.Wait()

			Expect(fakeClient.ReportCallCount()).To(Equal(1))
		})
	})

	Describe("#LogFields", func() {
		BeforeEach(func() {
			opts.AccessToken = accessToken
			opts.Tags = tags
			opts.ConnFactory = fakeConn
		})

		It("logs the fields", func() {
			span := tracer.StartSpan("test")
			Expect(span).NotTo(BeNil())

			span.LogFields(log.String("event", "test"))
			span.Finish()
			tracer.Flush(context.Background())

			Expect(fakeClient.ReportCallCount()).To(Equal(1))

			_, request, _ := fakeClient.ReportArgsForCall(0)
			Expect(request.Spans).To(HaveLen(1))

			logs := request.Spans[0].Logs
			Expect(logs).To(HaveLen(1))
			Expect(logs[0].Fields).To(Equal([]*collectorpb.KeyValue{&collectorpb.KeyValue{
				Key: "event",
				Value: &collectorpb.KeyValue_StringValue{
					StringValue: "test",
				},
			}}))
		})

		It("can happen concurrently with Flush", func() {
			span := tracer.StartSpan("test")
			Expect(span).NotTo(BeNil())

			wg := &sync.WaitGroup{}
			wg.Add(2)

			span.LogFields(log.String("event", "test"))
			span.Finish()

			go func() {
				defer wg.Done()

				span.LogFields(log.String("another event", "abc"))
			}()

			go func() {
				defer wg.Done()

				tracer.Flush(context.Background())
			}()

			wg.Wait()

			Expect(fakeClient.ReportCallCount()).To(Equal(1))
		})
	})

	Describe("#SetTag", func() {
		BeforeEach(func() {
			opts.AccessToken = accessToken
			opts.Tags = tags
			opts.ConnFactory = fakeConn
		})

		It("applies the tag to the span", func() {
			span := tracer.StartSpan("test")
			Expect(span).NotTo(BeNil())

			span.SetTag("a", "bc")
			span.Finish()
			tracer.Flush(context.Background())

			Expect(fakeClient.ReportCallCount()).To(Equal(1))

			_, request, _ := fakeClient.ReportArgsForCall(0)
			Expect(request.Spans).To(HaveLen(1))

			tags := request.Spans[0].Tags
			Expect(tags).To(HaveLen(1))
			Expect(tags[0]).To(Equal(&collectorpb.KeyValue{
				Key: "a",
				Value: &collectorpb.KeyValue_StringValue{
					StringValue: "bc",
				},
			}))
		})

		It("can happen concurrently with Flush", func() {
			span := tracer.StartSpan("test")
			Expect(span).NotTo(BeNil())

			wg := &sync.WaitGroup{}
			wg.Add(2)

			span.SetTag("abc", 123)
			span.Finish()

			go func() {
				defer wg.Done()

				span.SetTag("a", "bc")
			}()

			go func() {
				defer wg.Done()

				tracer.Flush(context.Background())
			}()

			wg.Wait()

			Expect(fakeClient.ReportCallCount()).To(Equal(1))
		})
	})

	Describe("Access Token", func() {
		BeforeEach(func() {
			opts.AccessToken = accessToken
			opts.Tags = tags
			opts.ConnFactory = fakeConn
		})

		It("should return the access token", func() {
			Expect(GetLightStepAccessToken(tracer)).To(Equal(accessToken))
		})
	})

	Describe("Flush", func() {
		BeforeEach(func() {
			opts.AccessToken = accessToken
			opts.Tags = tags
			opts.ConnFactory = fakeConn
			opts.Recorder = fakeRecorder
		})

		Context("when the span context is not sampled", func() {
			It("the span should not be recorded", func() {
				span := tracer.StartSpan("parent", SetTraceID(1), SetSampled("false"))
				span.Finish()
				Expect(fakeRecorder.RecordSpanCallCount()).To(Equal(0))
			})
		})

		Context("when the span context is sampled", func() {
			It("the span should be recorded", func() {
				span := tracer.StartSpan("parent", SetTraceID(1), SetSampled("true"))
				span.Finish()
				Expect(fakeRecorder.RecordSpanCallCount()).To(Equal(1))

				span = tracer.StartSpan("parent", SetTraceID(1), SetSampled("fals"))
				span.Finish()
				Expect(fakeRecorder.RecordSpanCallCount()).To(Equal(2))
			})
		})

		Context("when the parent span carries the Sampled flag", func() {
			It("should propagate flags from parent to child", func() {
				opentracing.SetGlobalTracer(tracer)

				opentracing_parent_span := tracer.StartSpan("parent", SetTraceID(1), SetSampled("false"))
				opentracing_parent_context := opentracing.ContextWithSpan(context.Background(), opentracing_parent_span)
				opentracing_child_span, _ := opentracing.StartSpanFromContext(opentracing_parent_context, "child")

				lightstep_child_spancontext, _ := opentracing_child_span.Context().(SpanContext)

				Expect(lightstep_child_spancontext.Sampled).To(Equal("false"))
				opentracing_child_span.Finish()
				opentracing_parent_span.Finish()
			})
		})

		Context("when the tracer is dropping spans", func() {
			const ExpectedDroppedSpans = 2

			JustBeforeEach(func() {
				// overflow the span buffer so the tracer starts to drop spans
				for i := 0; i < DefaultMaxSpans+ExpectedDroppedSpans; i++ {
					tracer.StartSpan(fmt.Sprint("span ", i)).Finish()
				}
			})

			It("emits EventStatusReport", func(done Done) {
				tracer.Flush(context.Background())

				err := <-eventChan
				dropSpanErr, ok := err.(EventStatusReport)
				Expect(ok).To(BeTrue())

				Expect(dropSpanErr.FlushDuration()).To(BeNumerically(">", 0))
				Expect(dropSpanErr.DroppedSpans()).To(Equal(ExpectedDroppedSpans))
				close(done)
			})

			It("emits exactly one event", func() {
				tracer.Flush(context.Background())

				Eventually(eventChan).Should(Receive())
				Consistently(eventChan).ShouldNot(Receive())
			})
		})

		Context("when the tracer is disabled", func() {
			JustBeforeEach(func() {
				tracer.Disable()
				Eventually(eventChan).Should(Receive())
			})

			It("should not flush spans", func() {
				reportCallCount := fakeClient.ReportCallCount()
				tracer.StartSpan("these spans should not be recorded").Finish()
				tracer.StartSpan("or flushed").Finish()

				tracer.Flush(context.Background())

				Consistently(fakeClient.ReportCallCount).Should(Equal(reportCallCount))
			}, 5)

			It("emits EventFlushError", func(done Done) {
				tracer.Flush(context.Background())

				event := <-eventChan
				flushErrorEvent, ok := event.(EventFlushError)
				Expect(ok).To(BeTrue())
				Expect(flushErrorEvent.State()).To(Equal(FlushErrorTracerDisabled))
				close(done)
			})

			It("emits exactly one event", func() {
				tracer.Flush(context.Background())

				Eventually(eventChan).Should(Receive())
				Consistently(eventChan).ShouldNot(Receive())
			})
		})

		Context("when tracer has been closed", func() {
			var reportCallCount int

			JustBeforeEach(func() {
				tracer.Close(context.Background())
				reportCallCount = fakeClient.ReportCallCount()
			})

			It("should not flush spans", func() {
				tracer.StartSpan("can't flush this").Finish()
				tracer.StartSpan("hammer time").Finish()

				tracer.Flush(context.Background())

				Consistently(fakeClient.ReportCallCount).Should(Equal(reportCallCount))
			})

			It("emits EventFlushError", func(done Done) {
				tracer.Flush(context.Background())

				Eventually(eventChan).Should(Receive())

				event := <-eventChan
				flushErrorEvent, ok := event.(EventFlushError)
				Expect(ok).To(BeTrue())
				Expect(flushErrorEvent.State()).To(Equal(FlushErrorTracerClosed))
				close(done)
			})

			It("emits exactly two events", func() {
				tracer.Flush(context.Background())

				Eventually(eventChan).Should(Receive())
				Eventually(eventChan).Should(Receive())
				Consistently(eventChan).ShouldNot(Receive())
			})
		})

		Context("when there is an error sending spans", func() {
			BeforeEach(func() {
				// set client to fail on the first call, then return normally
				fakeClient.ReportReturnsOnCall(0, nil, errors.New("fail"))
				fakeClient.ReportReturns(new(collectorpb.ReportResponse), nil)
			})

			It("should restore the spans that failed to be sent", func() {
				tracer.StartSpan("if at first you don't succeed...").Finish()
				tracer.StartSpan("...copy flushing back into your buffer").Finish()
				tracer.Flush(context.Background())
				tracer.Flush(context.Background())
				Expect(len(getReportedGRPCSpans(fakeClient))).To(Equal(4))
			})
		})

		Context("when a report is in progress", func() {
			var startReportonce sync.Once
			var startReportch chan struct{}
			var finishReportch chan struct{}

			BeforeEach(func() {
				finishReportch = make(chan struct{})
				startReportch = make(chan struct{})
				startReportonce = sync.Once{}
				fakeClient.ReportStub = func(ctx context.Context, req *collectorpb.ReportRequest, options ...grpc.CallOption) (*collectorpb.ReportResponse, error) {
					startReportonce.Do(func() { close(startReportch) })
					<-finishReportch
					return new(collectorpb.ReportResponse), nil
				}
			})

			JustBeforeEach(func() {
				// wait for tracer to have a report in flight
				Eventually(startReportch).Should(BeClosed())
			})

			It("retries flushing", func() {
				tracer.StartSpan("these spans should sit in the buffer").Finish()
				tracer.StartSpan("while the last report is in flight").Finish()

				flushFinishedch := make(chan struct{})
				go func() {
					Flush(context.Background(), tracer)
					close(flushFinishedch)
				}()
				// flush should wait for the last report to finish
				Consistently(flushFinishedch).ShouldNot(BeClosed())
				// no spans should have been reported yet
				Expect(getReportedGRPCSpans(fakeClient)).To(HaveLen(0))
				// allow the last report to finish
				close(finishReportch)
				// flush should now send a report with the last two spans
				Eventually(flushFinishedch).Should(BeClosed())
				// now the spans that were sitting in the buffer should be reported
				Expect(getReportedGRPCSpans(fakeClient)).Should(HaveLen(2))
			})
		})
	})

	Describe("Close", func() {
		BeforeEach(func() {
			opts.AccessToken = accessToken
			opts.Tags = tags
			opts.ConnFactory = fakeConn
			opts.MinReportingPeriod = 100 * time.Second
		})

		It("flushes the buffer before closing", func() {
			tracer.StartSpan("span").Finish()
			err := CloseTracer(tracer)
			Expect(err).NotTo(HaveOccurred())
			Expect(fakeClient.ReportCallCount()).To(Equal(1))
			tracer.StartSpan("span2").Finish()
			Consistently(fakeClient.ReportCallCount).Should(Equal(1))
		})
	})

	Describe("valid SpanRecorder", func() {
		BeforeEach(func() {
			opts.AccessToken = accessToken
			opts.Tags = tags
			opts.ConnFactory = fakeConn
			opts.Recorder = fakeRecorder
		})

		It("calls RecordSpan after finishing a span", func() {
			tracer.StartSpan("span").Finish()
			Expect(fakeRecorder.RecordSpanCallCount()).ToNot(BeZero())
		})
	})

	Describe("nil SpanRecorder", func() {
		BeforeEach(func() {
			opts.AccessToken = accessToken
			opts.Tags = tags
			opts.ConnFactory = fakeConn
			opts.Recorder = nil
		})

		It("doesn't call RecordSpan after finishing a span", func() {
			span := tracer.StartSpan("span")
			Expect(span.Finish).ToNot(Panic())
		})
	})

	Describe("provides its ReporterID", func() {
		BeforeEach(func() {
			opts.AccessToken = accessToken
			opts.Tags = tags
			opts.ConnFactory = fakeConn
			opts.Recorder = nil
		})

		It("is non-zero", func() {
			rid, err := GetLightStepReporterID(tracer)
			Expect(err).To(BeNil())
			Expect(rid).To(Not(BeZero()))
		})
	})
})

var _ = Describe("UnsupportedTracer", func() {
	type unsupportedTracer struct {
		opentracing.Tracer
	}

	tracer := unsupportedTracer{}

	Describe("has no ReporterID", func() {
		It("is an error", func() {
			rid, err := GetLightStepReporterID(tracer)
			Expect(err).To(Not(BeNil()))
			Expect(rid).To(BeZero())
		})
	})
})
