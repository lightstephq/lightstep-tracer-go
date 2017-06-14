package lightstep_test

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	"google.golang.org/grpc"

	. "github.com/lightstep/lightstep-tracer-go"
	"github.com/lightstep/lightstep-tracer-go/collectorpb"
	"github.com/lightstep/lightstep-tracer-go/collectorpb/collectorpbfakes"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	ot "github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/log"
	"golang.org/x/net/context"
)

func newGrpcServer(port int) (net.Listener, *grpc.Server, error) {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%v", port))
	if err != nil {
		return listener, nil, err
	}
	return listener, grpc.NewServer(), nil
}

func startNSpans(n int, tracer ot.Tracer) {
	for i := 0; i < n; i++ {
		tracer.StartSpan(string(i)).Finish()
	}
}

var _ = Describe("Recorder", func() {
	var tracer ot.Tracer

	Context("With grpc enabled", func() {
		var port int = 9090 + GinkgoParallelNode()
		var fakeCollector *collectorpbfakes.FakeCollectorServiceServer
		var grpcServer *grpc.Server
		var reportChan chan *collectorpb.ReportRequest

		BeforeEach(func() {
			var listener net.Listener
			var err error
			if listener, grpcServer, err = newGrpcServer(port); err != nil {
				Fail("failed to start grpc server")
			}

			fakeCollector = new(collectorpbfakes.FakeCollectorServiceServer)
			// setup a channel to grab the latest report to the Collector
			reportChan = make(chan *collectorpb.ReportRequest)
			fakeCollector.ReportStub = func(context context.Context, reportRequest *collectorpb.ReportRequest) (*collectorpb.ReportResponse, error) {
				reportChan <- reportRequest
				return nil, nil
			}
			collectorpb.RegisterCollectorServiceServer(grpcServer, fakeCollector)
			// NOTE: pretty sure we don't have to check if grpcServer is serving
			// since if we get err then err == nil in which case listener will buffer data
			go grpcServer.Serve(listener)

			tracer = NewTracer(Options{
				AccessToken:      "0987654321",
				Collector:        Endpoint{"localhost", port, true},
				ReportingPeriod:  1 * time.Millisecond,
				ReportTimeout:    10 * time.Millisecond,
				MaxLogKeyLen:     10,
				MaxLogValueLen:   11,
				MaxBufferedSpans: 10,
			})

			// make sure the grpcServer is serving
			Eventually(func() int {
				return fakeCollector.ReportCallCount()
			}).ShouldNot(BeZero())
		})

		AfterEach(func() {
			errChan := make(chan error)
			go func() { errChan <- CloseTracer(tracer) }()
			Eventually(errChan).Should(Receive(BeNil()))
			grpcServer.Stop()
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
				lastCallCount := fakeCollector.ReportCallCount()
				Consistently(func() int {
					return fakeCollector.ReportCallCount()
				}, 3, 0.05).Should(Equal(lastCallCount))

				By("Allowing other tracers to reconnect to the server")
				tracer = NewTracer(Options{
					AccessToken:     "0987654321",
					Collector:       Endpoint{"localhost", port, true},
					ReportingPeriod: 1 * time.Millisecond,
					ReportTimeout:   10 * time.Millisecond,
				})

				Eventually(func() int {
					return fakeCollector.ReportCallCount()
				}).ShouldNot(Equal(lastCallCount))
			})
		})

		Describe("SpanBuffer", func() {
			It("should respect MaxBufferedSpans", func() {
				startNSpans(10, tracer)
				Eventually(func() []*collectorpb.Span {
					return (<-reportChan).GetSpans()
				}).Should(HaveLen(10))

				startNSpans(10, tracer)
				Eventually(func() []*collectorpb.Span {
					return (<-reportChan).GetSpans()
				}).Should(HaveLen(10))
			})
		})

		Describe("Logging", func() {
			var span ot.Span
			BeforeEach(func() {
				reportChan = make(chan *collectorpb.ReportRequest)
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
				expected := []*collectorpb.KeyValue{
					&collectorpb.KeyValue{Key: "donut", Value: &collectorpb.KeyValue_StringValue{"bacon"}},
					&collectorpb.KeyValue{Key: "key", Value: &collectorpb.KeyValue_JsonValue{string(obj)}},
					&collectorpb.KeyValue{Key: "donut arm…", Value: &collectorpb.KeyValue_StringValue{"OOOOOOOOOO…"}},
					&collectorpb.KeyValue{Key: "life", Value: &collectorpb.KeyValue_IntValue{42}},
				}

				Eventually(func() []*collectorpb.KeyValue {
					spans := (<-reportChan).GetSpans()
					if len(spans) > 0 {
						return spans[0].GetLogs()[0].GetKeyvalues()
					}
					return []*collectorpb.KeyValue{}
				}).Should(BeEquivalentTo(expected))

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
