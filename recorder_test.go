package lightstep_test

import (
	"fmt"
	"net"
	"time"

	"google.golang.org/grpc"

	. "github.com/lightstep/lightstep-tracer-go"
	"github.com/lightstep/lightstep-tracer-go/basictracer"
	"github.com/lightstep/lightstep-tracer-go/collectorpb"
	"github.com/lightstep/lightstep-tracer-go/collectorpb/collectorpbfakes"
	"github.com/lightstep/lightstep-tracer-go/lightstep_thrift"
	"github.com/lightstep/lightstep-tracer-go/lightstep_thrift/lightstep_thriftfakes"
	"github.com/lightstep/lightstep-tracer-go/thrift_0_9_2/lib/go/thrift"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	ot "github.com/opentracing/opentracing-go"
)

func makeSpanSlice(length int) []basictracer.RawSpan {
	return make([]basictracer.RawSpan, length)
}

var _ = Describe("Recorder", func() {

	var (
		tracer ot.Tracer
	)

	Describe("CloseTracer", func() {
		Context("With thrift enabled", func() {
			var thriftServer *thrift.TSimpleServer
			var fakeReportingHandler *lightstep_thriftfakes.FakeReportingService

			BeforeEach(func() {
				addr := "localhost:50051"
				transport, err := thrift.NewTServerSocket(addr)
				if err != nil {
					Fail("failed to start thrift test server")
				}
				fakeReportingHandler = new(lightstep_thriftfakes.FakeReportingService)

				processor := lightstep_thrift.NewReportingServiceProcessor(fakeReportingHandler)
				protocolFactory := thrift.NewTBinaryProtocolFactoryDefault()

				transportFactory := thrift.NewTTransportFactory()
				thriftServer = thrift.NewTSimpleServer4(processor, transport, transportFactory, protocolFactory)

				go func() {
					if err := thriftServer.Serve(); err != nil {
						fmt.Println("Error running server: ", err)
					}
				}()

				tracer = NewTracer(Options{
					AccessToken: "0987654321",
					Collector:   Endpoint{"localhost", 50051, true},
					UseGRPC:     false,
					UseThrift:   true,
					// ReportingPeriod: 1 * time.Millisecond,
					Verbose:       true,
					ReportTimeout: 5000 * time.Millisecond,
				})
			})

			It("Should behave nicely", func() {
				// TODO: tracer cannot yet connect to thrift server
				// Eventually(func() int {
				// 	return fakeReportingHandler.ReportCallCount()
				// }, 3, 1).ShouldNot(BeZero())
				// By("Not hanging")
				// errChan := make(chan error)
				// go func() { errChan <- CloseTracer(tracer) }()
				// Eventually(errChan).Should(Receive(BeNil()))
				//
				// By("Stopping communication with the server")
				// lastCallCount := fakeReportingHandler.ReportCallCount()
				// Consistently(func() int {
				// 	return fakeReportingHandler.ReportCallCount()
				// }, 1, 0.05).Should(Equal(lastCallCount))

				// By("Allowing other tracers to reconnect to the server")
				// tracer = NewTracer(Options{
				// 	AccessToken:     "0987654321",
				// 	Collector:       Endpoint{"localhost", 50051, true},
				// 	UseGRPC:         true,
				// 	ReportingPeriod: 1 * time.Millisecond,
				// 	ReportTimeout:   10 * time.Millisecond,
				// })
				//
				// Eventually(func() int {
				// 	return fakeReportingHandler.ReportCallCount()
				// }).ShouldNot(Equal(lastCallCount))
			})

			AfterEach(func() {
				errChan := make(chan error)
				go func() { errChan <- thriftServer.Stop() }()
				if <-errChan != nil {
					Fail("Failed to stop THIFT server")
				}
			})
		})

		Context("With grpc enabled", func() {
			const port string = ":50051"

			var fakeCollector *collectorpbfakes.FakeCollectorServiceServer
			var grpcServer *grpc.Server

			BeforeEach(func() {
				listener, err := net.Listen("tcp", port)
				if err != nil {
					Fail("failed to start grpc test server")
				}

				fakeCollector = new(collectorpbfakes.FakeCollectorServiceServer)
				grpcServer = grpc.NewServer()
				collectorpb.RegisterCollectorServiceServer(grpcServer, fakeCollector)
				go grpcServer.Serve(listener)

				tracer = NewTracer(Options{
					AccessToken:     "0987654321",
					Collector:       Endpoint{"localhost", 50051, true},
					UseGRPC:         true,
					ReportingPeriod: 1 * time.Millisecond,
					ReportTimeout:   10 * time.Millisecond,
				})
			})

			It("Should behave nicely when called", func() {
				By("Not hanging")
				errChan := make(chan error)
				go func() { errChan <- CloseTracer(tracer) }()
				Eventually(errChan).Should(Receive(BeNil()))

				By("Stopping communication with the server")
				lastCallCount := fakeCollector.ReportCallCount()
				Consistently(func() int {
					return fakeCollector.ReportCallCount()
				}, 1, 0.05).Should(Equal(lastCallCount))

				By("Allowing other tracers to reconnect to the server")
				tracer = NewTracer(Options{
					AccessToken:     "0987654321",
					Collector:       Endpoint{"localhost", 50051, true},
					UseGRPC:         true,
					ReportingPeriod: 1 * time.Millisecond,
					ReportTimeout:   10 * time.Millisecond,
				})

				Eventually(func() int {
					return fakeCollector.ReportCallCount()
				}).ShouldNot(Equal(lastCallCount))
			})

			AfterEach(func() {
				grpcServer.GracefulStop()
			})
		})
	})

	Context("When calling Close() twice", func() {
		Context("With grpc enabled", func() {
			BeforeEach(func() {
				tracer = NewTracer(Options{
					AccessToken: "0987654321",
					UseGRPC:     true,
				})
			})
			It("Should not fail", func() {
				err := CloseTracer(tracer)
				Expect(err).NotTo(HaveOccurred())

				err = CloseTracer(tracer)
				Expect(err).NotTo(HaveOccurred())
			})
		})

		Context("With thrift enabled", func() {
			BeforeEach(func() {
				tracer = NewTracer(Options{
					AccessToken: "0987654321",
					UseGRPC:     false,
				})
			})
			It("Should not fail", func() {
				err := CloseTracer(tracer)
				Expect(err).NotTo(HaveOccurred())

				err = CloseTracer(tracer)
				Expect(err).NotTo(HaveOccurred())
			})
		})
	})
})
