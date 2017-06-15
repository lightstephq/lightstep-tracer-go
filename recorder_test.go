package lightstep_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/grpc"

	. "github.com/lightstep/lightstep-tracer-go"
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
	return func() []*cpb.Span { return (<-reportChan).GetSpans() }
}

func FakeGrpcConnection(fakeClient *cpbfakes.FakeCollectorServiceClient) func() (GrpcConnection, cpb.CollectorServiceClient, error) {
	return func() (GrpcConnection, cpb.CollectorServiceClient, error) {
		return new(dummyConn), fakeClient, nil
	}
}

type dummyConn struct{}

func (*dummyConn) Close() error                               { return nil }
func (*dummyConn) GetMethodConfig(_ string) grpc.MethodConfig { return grpc.MethodConfig{} }

var _ = Describe("Recorder", func() {
	var tracer ot.Tracer

	Context("With grpc enabled", func() {
		var port int = 9090 + GinkgoParallelNode()
		var latestSpans func() []*cpb.Span
		var fakeClient *cpbfakes.FakeCollectorServiceClient

		BeforeEach(func() {
			fakeClient = new(cpbfakes.FakeCollectorServiceClient)
			latestSpans = attachSpanListener(fakeClient)
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

		AfterEach(func() {
			errChan := make(chan error)
			go func() { errChan <- CloseTracer(tracer) }()
			Eventually(errChan).Should(Receive(BeNil()))
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

		Describe("SpanBuffer", func() {
			It("should respect MaxBufferedSpans", func() {
				startNSpans(10, tracer)
				Eventually(latestSpans).Should(HaveLen(10))

				startNSpans(10, tracer)
				Eventually(latestSpans).Should(HaveLen(10))
			})
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

				_ = expected

				Eventually(func() []*cpb.KeyValue {
					spans := latestSpans()
					if len(spans) > 0 && len(spans[0].GetLogs()) > 0 {
						fmt.Println(spans[0].GetLogs())
						return spans[0].GetLogs()[0].GetKeyvalues()
					}
					return []*cpb.KeyValue{}
				}).Should(BeEquivalentTo(expected))

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

	})

	Context("With thrift enabled", func() {
		// var thriftServer *thrift.TSimpleServer
		// var fakeReportingHandler *lightstep_thriftfakes.FakeReportingService
		// var transport *thrift.TServerSocket

		BeforeEach(func() {
			// addr := "localhost:9090"
			// var err error
			// transport, err = thrift.NewTServerSocket(addr)
			// if err != nil {
			// 	Fail("failed to start thrift test server")
			// }
			// fakeReportingHandler = new(lightstep_thriftfakes.FakeReportingService)
			//
			// processor := lightstep_thrift.NewReportingServiceProcessor(fakeReportingHandler)
			// protocolFactory := thrift.NewTBinaryProtocolFactoryDefault()
			//
			// transportFactory := thrift.NewTTransportFactory()
			// thriftServer = thrift.NewTSimpleServer4(processor, transport, transportFactory, protocolFactory)
			//
			// go func() {
			// 	if err := thriftServer.Serve(); err != nil {
			// 		fmt.Println("Error running server: ", err)
			// 	}
			// }()
			//
			// time.Sleep(1000 * time.Millisecond)
			//
			// tracer = NewTracer(Options{
			// 	AccessToken: "0987654321",
			// 	Collector:   Endpoint{"localhost", 9090, true},
			// 	UseThrift:   true,
			// 	Verbose:     true,
			// })
			// span := tracer.StartSpan("span spun spunt")
			// time.Sleep(1000 * time.Millisecond)
			// span.Finish()
			// time.Sleep(1000 * time.Millisecond)
			// if err := FlushLightStepTracer(tracer); err != nil {
			// 	panic(err)
			// }
		})

		It("Should behave nicely", func() {
			// TODO: tracer cannot yet connect to thrift server
		})

		AfterEach(func() {
			// if err := thriftServer.Stop(); err != nil {
			// 	Fail("Failed to stop THIFT server")
			// }
			// if err := transport.Close(); err != nil {
			// 	Fail("Failed to close server port")
			// }
		})
	})

})
