package lightstep_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/grpc"

	. "github.com/lightstep/lightstep-tracer-go"
	cpb "github.com/lightstep/lightstep-tracer-go/collectorpb"
	cpbfakes "github.com/lightstep/lightstep-tracer-go/collectorpb/collectorpbfakes"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	ot "github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/log"
)

type haveKeyValuesMatcher []*cpb.KeyValue

func HaveKeyValues(keyValues ...*cpb.KeyValue) types.GomegaMatcher {
	return haveKeyValuesMatcher(keyValues)
}

func (matcher haveKeyValuesMatcher) Match(actual interface{}) (bool, error) {
	var actualKeyValues []*cpb.KeyValue

	switch v := actual.(type) {
	case []*cpb.KeyValue:
		actualKeyValues = v
	case *cpb.Log:
		actualKeyValues = v.GetKeyvalues()
	default:
		return false, fmt.Errorf("HaveKeyValues matcher expects either a []*KeyValue or a *Log")
	}

	expectedKeyValues := []*cpb.KeyValue(matcher)
	if len(expectedKeyValues) != len(actualKeyValues) {
		return false, nil
	}

	for i, _ := range actualKeyValues {
		if !reflect.DeepEqual(actualKeyValues[i], expectedKeyValues[i]) {
			return false, nil
		}
	}

	return true, nil
}

func (matcher haveKeyValuesMatcher) FailureMessage(actual interface{}) string {
	return fmt.Sprintf("Expected '%v' to have key values '%v'", actual, matcher)
}

func (matcher haveKeyValuesMatcher) NegatedFailureMessage(actual interface{}) string {
	return fmt.Sprintf("Expected '%v' to not have key values '%v'", actual, matcher)
}

func KeyValue(key string, value interface{}, storeAsJson ...bool) *cpb.KeyValue {
	tag := &cpb.KeyValue{Key: key}
	switch typedValue := value.(type) {
	case int:
		tag.Value = &cpb.KeyValue_IntValue{int64(typedValue)}
	case string:
		if len(storeAsJson) > 0 && storeAsJson[0] {
			tag.Value = &cpb.KeyValue_JsonValue{typedValue}
		} else {
			tag.Value = &cpb.KeyValue_StringValue{typedValue}
		}
	case bool:
		tag.Value = &cpb.KeyValue_BoolValue{typedValue}
	case float32:
		tag.Value = &cpb.KeyValue_DoubleValue{float64(typedValue)}
	case float64:
		tag.Value = &cpb.KeyValue_DoubleValue{typedValue}
	}
	return tag
}

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

func FakeGrpcConnection(fakeClient *cpbfakes.FakeCollectorServiceClient) ConnectorFactory {
	return func() (interface{}, Connection, error) {
		return fakeClient, new(dummyConn), nil
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
					ConnFactory:     FakeGrpcConnection(fakeClient),
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
				Expect(spans[0].GetTags()).To(HaveKeyValues(KeyValue("tag", "you're it!")))
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
						ConnFactory:     FakeGrpcConnection(fakeClient),
					})
					Eventually(fakeClient.ReportCallCount).ShouldNot(Equal(lastCallCount))
				})
			})

			Describe("Options", func() {
				const expectedTraceID uint64 = 1
				const expectedSpanID uint64 = 2
				const expectedParentSpanID uint64 = 3

				Context("When the TraceID is set", func() {
					BeforeEach(func() {
						tracer.StartSpan("x", SetTraceID(expectedTraceID)).Finish()
					})

					It("Should set the specified options", func() {
						spans := latestSpans()
						Expect(spans).To(HaveLen(1))
						Expect(spans[0].GetSpanContext().GetTraceId()).To(Equal(expectedTraceID))
						Expect(spans[0].GetSpanContext().GetSpanId()).ToNot(Equal(uint64(0)))
						Expect(spans[0].GetReferences()).To(BeEmpty())
					})
				})

				Context("When both the TraceID and SpanID are set", func() {
					BeforeEach(func() {
						tracer.StartSpan("x", SetTraceID(expectedTraceID), SetSpanID(expectedSpanID)).Finish()
					})

					It("Should set the specified options", func() {
						spans := latestSpans()
						Expect(spans).To(HaveLen(1))
						Expect(spans[0].GetSpanContext().TraceId).To(Equal(expectedTraceID))
						Expect(spans[0].GetSpanContext().SpanId).To(Equal(expectedSpanID))
						Expect(spans[0].GetReferences()).To(BeEmpty())
					})
				})

				Context("When TraceID, SpanID, and ParentSpanID are set", func() {
					BeforeEach(func() {
						tracer.StartSpan("x", SetTraceID(expectedTraceID), SetSpanID(expectedSpanID), SetParentSpanID(expectedParentSpanID)).Finish()
					})

					It("Should set the specified options", func() {
						spans := latestSpans()
						Expect(spans).To(HaveLen(1))
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

				var knownContext1 = SpanContext{
					SpanID:  6397081719746291766,
					TraceID: 506100417967962170,
					Baggage: map[string]string{"checked": "baggage"},
				}
				var knownContext2 = SpanContext{
					SpanID:  14497723526785009626,
					TraceID: 7355080808006516497,
					Baggage: map[string]string{"checked": "baggage"},
				}
				var testContext1 = SpanContext{
					SpanID:  123,
					TraceID: 456,
					Baggage: nil,
				}
				var testContext2 = SpanContext{
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
						for _, origContext := range []SpanContext{knownContext1, knownContext2, testContext1, testContext2} {
							err := tracer.Inject(origContext, BinaryCarrier, &carrierString)
							Expect(err).ToNot(HaveOccurred())

							context, err := tracer.Extract(BinaryCarrier, carrierString)
							Expect(err).ToNot(HaveOccurred())
							Expect(context).To(BeEquivalentTo(origContext))
						}
					})

					It("Should support infjecting into byte arrays", func() {
						for _, origContext := range []SpanContext{knownContext1, knownContext2, testContext1, testContext2} {
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
					ConnFactory:     FakeGrpcConnection(fakeClient),
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
					spans := latestSpans()
					Expect(spans).To(HaveLen(1))
					Expect(spans[0].GetLogs()).To(HaveLen(1))
					Expect(spans[0].GetLogs()[0]).To(HaveKeyValues(
						KeyValue("donut", "bacon"),
						KeyValue("key", string(obj), true),
						KeyValue("donut arm…", "OOOOOOOOOO…"),
						KeyValue("life", 42),
					))
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
					ConnFactory:      FakeGrpcConnection(fakeClient),
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
					ConnFactory:     FakeGrpcConnection(fakeClient),
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
				Expect(spans[0].GetTags()).To(HaveKeyValues(KeyValue("tag", "value")))
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
					ConnFactory:     FakeGrpcConnection(fakeClient),
				})

				// make sure the fake client is working
				Eventually(fakeClient.ReportCallCount).ShouldNot(BeZero())
			})

			It("keeps all logs if they don't exceed MaxLogsPerSpan", func() {
				const logCount = 10
				span := tracer.StartSpan("span")
				for i := 0; i < logCount; i++ {
					span.LogKV("id", i)
				}
				span.Finish()

				spans := latestSpans()
				Expect(spans).To(HaveLen(1))
				Expect(spans[0].OperationName).To(Equal("span"))
				Expect(spans[0].GetLogs()).To(HaveLen(10))

				for i, log := range spans[0].GetLogs() {
					Expect(log).To(HaveKeyValues(KeyValue("id", i)))
				}
			})

			It("throws away the middle logs when they exceed MaxLogsPerSpan", func() {
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
					Expect(log).To(HaveKeyValues(KeyValue("id", i)))
				}

				warnLog := spans[0].GetLogs()[split]
				Expect(warnLog).To(HaveKeyValues(
					KeyValue("event", "dropped Span logs"),
					KeyValue("dropped_log_count", logCount-len(spans[0].GetLogs())+1),
					KeyValue("component", "basictracer"),
				))

				lastLogs := spans[0].GetLogs()[split+1:]
				for i, log := range lastLogs {
					Expect(log).To(HaveKeyValues(KeyValue("id", logCount-len(lastLogs)+i)))
				}
			})
		})
	})
})
