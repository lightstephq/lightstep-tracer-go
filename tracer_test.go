package lightstep_test

import (
	"encoding/json"
	"strings"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/grpc"

	. "github.com/lightstep/lightstep-tracer-go"
	bt "github.com/lightstep/lightstep-tracer-go/basictracer"
	cpb "github.com/lightstep/lightstep-tracer-go/collectorpb"
	cpbfakes "github.com/lightstep/lightstep-tracer-go/collectorpb/collectorpbfakes"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	ot "github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/log"
)

func startNSpans(n int, tracer ot.Tracer) {
	for i := 0; i < n; i++ {
		tracer.StartSpan(string(i)).Finish()
	}
}

func attachSpanListener(fakeClient *cpbfakes.FakeCollectorServiceClient) func() []*cpb.Span {
	reportChan := make(chan *cpb.ReportRequest)
	fakeClient.ReportStub = func(context context.Context, reportResponse *cpb.ReportRequest, options ...grpc.CallOption) (*cpb.ReportResponse, error) {
		select {
		case reportChan <- reportResponse:
		case <-time.After(1 * time.Second):
		}
		return &cpb.ReportResponse{}, nil
	}

	return func() []*cpb.Span {
		timeout := time.After(5 * time.Second)
		for {
			select {
			case report := <-reportChan:
				if len(report.GetSpans()) > 0 {
					return report.GetSpans()
				}
			case <-timeout:
				Fail("timed out trying to get spans")
			}
		}
	}
}

func FakeGrpcConnection(fakeClient *cpbfakes.FakeCollectorServiceClient) func() (GrpcConnection, cpb.CollectorServiceClient, error) {
	return func() (GrpcConnection, cpb.CollectorServiceClient, error) {
		return new(dummyConn), fakeClient, nil
	}
}

type dummyConn struct{}

func (*dummyConn) Close() error                               { return nil }
func (*dummyConn) GetMethodConfig(_ string) grpc.MethodConfig { return grpc.MethodConfig{} }

var _ = Describe("Tracer", func() {
	var tracer ot.Tracer
	const port = 9090

	Context("With grpc enabled", func() {
		var latestSpans func() []*cpb.Span
		var fakeClient *cpbfakes.FakeCollectorServiceClient

		BeforeEach(func() {
			fakeClient = new(cpbfakes.FakeCollectorServiceClient)
			latestSpans = attachSpanListener(fakeClient)
		})

		AfterEach(func() {
			errChan := make(chan error)
			go func() { errChan <- CloseTracer(tracer) }()
			Eventually(errChan).Should(Receive(BeNil()))
		})

		Context("With default options", func() {
			BeforeEach(func() {
				tracer = NewTracer(Options{
					AccessToken:     "0987654321",
					Collector:       Endpoint{"localhost", port, true},
					ReportingPeriod: 1 * time.Millisecond,
					ReportTimeout:   10 * time.Millisecond,
					GrpcConnector:   FakeGrpcConnection(fakeClient),
				})

				// make sure the fake client is working
				Eventually(fakeClient.ReportCallCount).ShouldNot(BeZero())
			})

			It("Should record baggage info internally", func() {
				span := tracer.StartSpan("x")
				span.SetBaggageItem("x", "y")
				Expect(span.BaggageItem("x")).To(Equal("y"))
			})

			It("Should send span operation names to the collector", func() {
				tracer.StartSpan("smooth").Finish()

				spans := latestSpans()
				Expect(spans).To(HaveLen(1))

				Expect(spans[0].OperationName).To(Equal("smooth"))
			})

			It("Should send tags back to the collector", func() {
				span := tracer.StartSpan("brokay doogle")
				span.SetTag("tag", "you're it!")
				span.Finish()

				spans := latestSpans()
				Expect(spans).To(HaveLen(1))

				Expect(spans[0].GetTags()).To(BeEquivalentTo(
					[]*cpb.KeyValue{
						&cpb.KeyValue{
							Key:   "tag",
							Value: &cpb.KeyValue_StringValue{"you're it!"},
						},
					}))
			})

			It("Should send baggage info to the collector", func() {
				span := tracer.StartSpan("x")
				span.SetBaggageItem("x", "y")
				span.Finish()

				spans := latestSpans()
				Expect(spans).To(HaveLen(1))

				Expect(spans[0].GetSpanContext().GetBaggage()).To(BeEquivalentTo(map[string]string{"x": "y"}))
			})

			It("ForeachBaggageItem", func() {
				span := tracer.StartSpan("x")
				span.SetBaggageItem("x", "y")
				baggage := make(map[string]string)
				span.Context().ForeachBaggageItem(func(k, v string) bool {
					baggage[k] = v
					return true
				})
				Expect(baggage).To(BeEquivalentTo(map[string]string{"x": "y"}))

				span.SetBaggageItem("a", "b")
				baggage = make(map[string]string)
				span.Context().ForeachBaggageItem(func(k, v string) bool {
					baggage[k] = v
					return false // exit early
				})
				Expect(baggage).To(HaveLen(1))
				span.Finish()

				spans := latestSpans()
				Expect(spans).To(HaveLen(1))

				Expect(spans[0].GetSpanContext().GetBaggage()).To(HaveLen(2))
			})

			Describe("CloseTracer", func() {
				It("Should not explode when called twice", func() {
					errChan := make(chan error)
					go func() { errChan <- CloseTracer(tracer) }()
					Eventually(errChan).Should(Receive(BeNil()))

					go func() { errChan <- CloseTracer(tracer) }()
					Eventually(errChan).Should(Receive(BeNil()))
				})

				It("Should behave nicely", func() {
					By("Not hanging")
					errChan := make(chan error)
					go func() { errChan <- CloseTracer(tracer) }()

					Eventually(errChan).Should(Receive(BeNil()))

					By("Stop communication with server")
					lastCallCount := fakeClient.ReportCallCount()
					Consistently(fakeClient.ReportCallCount, 2, 0.05).Should(Equal(lastCallCount))

					By("Allowing other tracers to reconnect to the server")
					tracer = NewTracer(Options{
						AccessToken:     "0987654321",
						Collector:       Endpoint{"localhost", port, true},
						ReportingPeriod: 1 * time.Millisecond,
						ReportTimeout:   10 * time.Millisecond,
						GrpcConnector: func() (GrpcConnection, cpb.CollectorServiceClient, error) {
							return new(dummyConn), fakeClient, nil
						},
					})

					Eventually(fakeClient.ReportCallCount).ShouldNot(Equal(lastCallCount))
				})
			})

			Describe("Options", func() {
				const expectedTraceID uint64 = 1
				const expectedSpanID uint64 = 2
				const expectedParentSpanID uint64 = 3

				Context("When only the TraceID is set", func() {
					BeforeEach(func() {
						tracer.StartSpan("x", SetTraceID(expectedTraceID)).Finish()
					})

					It("Should set the options appropriately", func() {
						By("Only running one span")
						var spans []*cpb.Span
						Eventually(func() []*cpb.Span {
							spans = latestSpans()
							return spans
						}).Should(HaveLen(1))

						By("Appropriately setting TraceID")
						Expect(spans[0].GetSpanContext().GetTraceId()).To(Equal(expectedTraceID))
						Expect(spans[0].GetSpanContext().GetSpanId()).ToNot(Equal(uint64(0)))
						Expect(spans[0].GetReferences()).To(BeEmpty())
					})
				})

				Context("When both the TraceID and SpanID are set", func() {
					BeforeEach(func() {
						tracer.StartSpan("x", SetTraceID(expectedTraceID), SetSpanID(expectedSpanID)).Finish()
					})

					It("Should set the options appropriately", func() {
						By("Only running one span")
						var spans []*cpb.Span
						Eventually(func() []*cpb.Span {
							spans = latestSpans()
							return spans
						}).Should(HaveLen(1))

						By("Appropriately setting the TraceID and SpanID")
						Expect(spans[0].GetSpanContext().TraceId).To(Equal(expectedTraceID))
						Expect(spans[0].GetSpanContext().SpanId).To(Equal(expectedSpanID))
						Expect(spans[0].GetReferences()).To(BeEmpty())
					})
				})

				Context("When TraceID, SpanID, and ParentSpanID are set", func() {
					BeforeEach(func() {
						tracer.StartSpan("x", SetTraceID(expectedTraceID), SetSpanID(expectedSpanID), SetParentSpanID(expectedParentSpanID)).Finish()
					})

					It("Should set the options appropriately", func() {
						By("Only running one span")
						var spans []*cpb.Span
						Eventually(func() []*cpb.Span {
							spans = latestSpans()
							return spans
						}).Should(HaveLen(1))

						By("Appropriately setting TraceID, SpanID, and ParentSpanID")
						Expect(spans[0].GetSpanContext().TraceId).To(Equal(expectedTraceID))
						Expect(spans[0].GetSpanContext().SpanId).To(Equal(expectedSpanID))
						Expect(spans[0].GetReferences()).ToNot(BeEmpty())
						Expect(spans[0].GetReferences()[0].GetSpanContext().SpanId).To(Equal(expectedParentSpanID))
					})
				})
			})

			Describe("Binary Carriers", func() {
				const knownCarrier1 = "EigJOjioEaYHBgcRNmifUO7/xlgYASISCgdjaGVja2VkEgdiYWdnYWdl"
				const knownCarrier2 = "EigJEX+FpwZ/EmYR2gfYQbxCMskYASISCgdjaGVja2VkEgdiYWdnYWdl"
				const badCarrier1 = "Y3QbxCMskYASISCgdjaGVja2VkEgd"

				var knownContext1 = bt.SpanContext{
					SpanID:  6397081719746291766,
					TraceID: 506100417967962170,
					Baggage: map[string]string{"checked": "baggage"},
				}
				var knownContext2 = bt.SpanContext{
					SpanID:  14497723526785009626,
					TraceID: 7355080808006516497,
					Baggage: map[string]string{"checked": "baggage"},
				}
				var testContext1 = bt.SpanContext{
					SpanID:  123,
					TraceID: 456,
					Baggage: nil,
				}
				var testContext2 = bt.SpanContext{
					SpanID:  123000000000,
					TraceID: 456000000000,
					Baggage: map[string]string{"a": "1", "b": "2", "c": "3"},
				}

				Context("tracer inject", func() {
					var carrierString string
					var carrierBytes []byte

					BeforeEach(func() {
						carrierString = ""
						carrierBytes = []byte{}
					})

					It("Should support injecting into strings ", func() {
						for _, origContext := range []bt.SpanContext{knownContext1, knownContext2, testContext1, testContext2} {
							err := tracer.Inject(origContext, BinaryCarrier, &carrierString)
							Expect(err).ToNot(HaveOccurred())

							context, err := tracer.Extract(BinaryCarrier, carrierString)
							Expect(err).ToNot(HaveOccurred())
							Expect(context).To(BeEquivalentTo(origContext))
						}
					})

					It("Should support infjecting into byte arrays", func() {
						for _, origContext := range []bt.SpanContext{knownContext1, knownContext2, testContext1, testContext2} {
							err := tracer.Inject(origContext, BinaryCarrier, &carrierBytes)
							Expect(err).ToNot(HaveOccurred())

							context, err := tracer.Extract(BinaryCarrier, carrierBytes)
							Expect(err).ToNot(HaveOccurred())
							Expect(context).To(BeEquivalentTo(origContext))
						}
					})

					It("Should return nil for nil contexts", func() {
						err := tracer.Inject(nil, BinaryCarrier, carrierString)
						Expect(err).To(HaveOccurred())

						err = tracer.Inject(nil, BinaryCarrier, carrierBytes)
						Expect(err).To(HaveOccurred())
					})
				})

				Context("tracer extract", func() {
					It("Should extract SpanContext from carrier as string", func() {
						context, err := tracer.Extract(BinaryCarrier, knownCarrier1)
						Expect(context).To(BeEquivalentTo(knownContext1))
						Expect(err).To(BeNil())

						context, err = tracer.Extract(BinaryCarrier, knownCarrier2)
						Expect(context).To(BeEquivalentTo(knownContext2))
						Expect(err).To(BeNil())
					})

					It("Should extract SpanContext from carrier as []byte", func() {
						context, err := tracer.Extract(BinaryCarrier, []byte(knownCarrier1))
						Expect(context).To(BeEquivalentTo(knownContext1))
						Expect(err).To(BeNil())

						context, err = tracer.Extract(BinaryCarrier, []byte(knownCarrier2))
						Expect(context).To(BeEquivalentTo(knownContext2))
						Expect(err).To(BeNil())
					})

					It("Should return nil for bad carriers", func() {
						for _, carrier := range []interface{}{badCarrier1, []byte(badCarrier1), "", []byte(nil)} {
							context, err := tracer.Extract(BinaryCarrier, carrier)
							Expect(context).To(BeNil())
							Expect(err).To(HaveOccurred())
						}
					})
				})
			})
		})

		Context("With custom log length", func() {
			BeforeEach(func() {
				tracer = NewTracer(Options{
					AccessToken:     "0987654321",
					Collector:       Endpoint{"localhost", port, true},
					ReportingPeriod: 1 * time.Millisecond,
					ReportTimeout:   10 * time.Millisecond,
					MaxLogKeyLen:    10,
					MaxLogValueLen:  11,
					GrpcConnector:   FakeGrpcConnection(fakeClient),
				})
				// make sure the fake client is working
				Eventually(fakeClient.ReportCallCount).ShouldNot(BeZero())
			})

			Describe("Logging", func() {
				var span ot.Span
				BeforeEach(func() {
					span = tracer.StartSpan("spantastic")
				})

				It("Should send logs back to the collector", func() {
					span.LogFields(
						log.String("donut", "bacon"),
						log.Object("key", []interface{}{"gr", 8}),
						log.String("donut army"+strings.Repeat("O", 50), strings.Repeat("O", 110)),
						log.Int("life", 42),
					)
					span.Finish()

					obj, _ := json.Marshal([]interface{}{"gr", 8})
					expected := []*cpb.KeyValue{
						&cpb.KeyValue{Key: "donut", Value: &cpb.KeyValue_StringValue{"bacon"}},
						&cpb.KeyValue{Key: "key", Value: &cpb.KeyValue_JsonValue{string(obj)}},
						&cpb.KeyValue{Key: "donut arm…", Value: &cpb.KeyValue_StringValue{"OOOOOOOOOO…"}},
						&cpb.KeyValue{Key: "life", Value: &cpb.KeyValue_IntValue{42}},
					}

					Eventually(func() []*cpb.KeyValue {
						spans := latestSpans()
						if len(spans) > 0 && len(spans[0].GetLogs()) > 0 {
							return spans[0].GetLogs()[0].GetKeyvalues()
						}
						return []*cpb.KeyValue{}
					}).Should(BeEquivalentTo(expected))

				})
			})
		})

		Context("With custom MaxBufferedSpans", func() {
			BeforeEach(func() {
				tracer = NewTracer(Options{
					AccessToken:      "0987654321",
					Collector:        Endpoint{"localhost", port, true},
					ReportingPeriod:  1 * time.Millisecond,
					ReportTimeout:    10 * time.Millisecond,
					MaxLogKeyLen:     10,
					MaxLogValueLen:   11,
					MaxBufferedSpans: 10,
					GrpcConnector:    FakeGrpcConnection(fakeClient),
				})

				// make sure the fake client is working
				Eventually(fakeClient.ReportCallCount).ShouldNot(BeZero())
			})

			Describe("SpanBuffer", func() {
				It("should respect MaxBufferedSpans", func() {
					startNSpans(10, tracer)
					Eventually(latestSpans).Should(HaveLen(10))

					startNSpans(10, tracer)
					Eventually(latestSpans).Should(HaveLen(10))
				})
			})
		})

		Context("With DropSpanLogs set", func() {
			BeforeEach(func() {
				tracer = NewTracer(Options{
					AccessToken:     "0987654321",
					Collector:       Endpoint{"localhost", port, true},
					ReportingPeriod: 1 * time.Millisecond,
					ReportTimeout:   10 * time.Millisecond,
					DropSpanLogs:    true,
					GrpcConnector:   FakeGrpcConnection(fakeClient),
				})

				// make sure the fake client is working
				Eventually(fakeClient.ReportCallCount).ShouldNot(BeZero())
			})

			It("Should not record logs", func() {
				span := tracer.StartSpan("x")
				span.LogFields(log.String("Led", "Zeppelin"), log.Uint32("32bit", 4294967295))
				span.SetTag("tag", "value")
				span.Finish()

				spans := latestSpans()
				Expect(spans).To(HaveLen(1))
				Expect(spans[0].GetOperationName()).To(Equal("x"))
				Expect(spans[0].GetTags()).To(BeEquivalentTo([]*cpb.KeyValue{
					&cpb.KeyValue{
						Key:   "tag",
						Value: &cpb.KeyValue_StringValue{"value"},
					}}))
				Expect(spans[0].GetLogs()).To(BeEmpty())
			})
		})

		Context("With MaxLogsPerSpan set", func() {
			BeforeEach(func() {
				tracer = NewTracer(Options{
					AccessToken:     "0987654321",
					Collector:       Endpoint{"localhost", port, true},
					ReportingPeriod: 1 * time.Millisecond,
					ReportTimeout:   10 * time.Millisecond,
					MaxLogsPerSpan:  10,
					GrpcConnector:   FakeGrpcConnection(fakeClient),
				})

				// make sure the fake client is working
				Eventually(fakeClient.ReportCallCount).ShouldNot(BeZero())
			})

			It("", func() {
				const logCount = 50
				span := tracer.StartSpan("span")
				for i := 0; i < logCount; i++ {
					span.LogKV("id", i)
				}
				span.Finish()

				spans := latestSpans()
				Expect(spans).To(HaveLen(1))
				Expect(spans[0].OperationName).To(Equal("span"))
				Expect(spans[0].GetLogs()).To(HaveLen(10))

				split := (len(spans[0].GetLogs()) - 1) / 2
				firstLogs := spans[0].GetLogs()[:split]
				for i, log := range firstLogs {
					Expect(log.GetKeyvalues()).To(BeEquivalentTo([]*cpb.KeyValue{
						&cpb.KeyValue{
							Key:   "id",
							Value: &cpb.KeyValue_IntValue{int64(i)},
						},
					}))
				}

				warnLog := spans[0].GetLogs()[split]
				Expect(warnLog.GetKeyvalues()).To(BeEquivalentTo([]*cpb.KeyValue{
					&cpb.KeyValue{
						Key:   "event",
						Value: &cpb.KeyValue_StringValue{"dropped Span logs"},
					},
					&cpb.KeyValue{
						Key:   "dropped_log_count",
						Value: &cpb.KeyValue_IntValue{int64(logCount - len(spans[0].GetLogs()) + 1)},
					},
					&cpb.KeyValue{
						Key:   "component",
						Value: &cpb.KeyValue_StringValue{"basictracer"},
					},
				}))

				lastLogs := spans[0].GetLogs()[split+1:]
				for i, log := range lastLogs {
					Expect(log.GetKeyvalues()).To(BeEquivalentTo([]*cpb.KeyValue{
						&cpb.KeyValue{
							Key:   "id",
							Value: &cpb.KeyValue_IntValue{int64(logCount - len(lastLogs) + i)},
						},
					}))
				}
			})
		})
	})
})
