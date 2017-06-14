package lightstep_test

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	"google.golang.org/grpc"

	. "github.com/lightstep/lightstep-tracer-go"
	cpb "github.com/lightstep/lightstep-tracer-go/collectorpb"
	cpbfakes "github.com/lightstep/lightstep-tracer-go/collectorpb/collectorpbfakes"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	ot "github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/log"
	"golang.org/x/net/context"
)

func newGrpcServer(port int) (net.Listener, *cpbfakes.FakeCollectorServiceServer, *grpc.Server, error) {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%v", port))
	if err != nil {
		return listener, nil, nil, err
	}
	fakeCollector := new(cpbfakes.FakeCollectorServiceServer)
	grpcServer := grpc.NewServer()
	cpb.RegisterCollectorServiceServer(grpcServer, fakeCollector)
	return listener, fakeCollector, grpcServer, nil
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
		var fakeCollector *cpbfakes.FakeCollectorServiceServer
		var grpcServer *grpc.Server
		var latestSpans func() []*cpb.Span

		BeforeEach(func() {
			var listener net.Listener
			var err error
			if listener, fakeCollector, grpcServer, err = newGrpcServer(port); err != nil {
				Fail("failed to start grpc server")
			}

			// setup a channel to grab the latest report to the Collector
			reportChan := make(chan *cpb.ReportRequest)
			fakeCollector.ReportStub = func(context context.Context, reportRequest *cpb.ReportRequest) (*cpb.ReportResponse, error) {
				reportChan <- reportRequest
				return nil, nil
			}
			latestSpans = func() []*cpb.Span { return (<-reportChan).GetSpans() }
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
				Consistently(fakeCollector.ReportCallCount, 3, 0.05).Should(Equal(lastCallCount))

				By("Allowing other tracers to reconnect to the server")
				tracer = NewTracer(Options{
					AccessToken:     "0987654321",
					Collector:       Endpoint{"localhost", port, true},
					ReportingPeriod: 1 * time.Millisecond,
					ReportTimeout:   10 * time.Millisecond,
				})

				Eventually(fakeCollector.ReportCallCount).ShouldNot(Equal(lastCallCount))
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

				Eventually(func() []*cpb.KeyValue {
					spans := latestSpans()
					if len(spans) > 0 {
						return spans[0].GetLogs()[0].GetKeyvalues()
					}
					return []*cpb.KeyValue{}
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
