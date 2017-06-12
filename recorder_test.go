package lightstep_test

import (
	"fmt"
	"net"
	"os"
	"time"

	"google.golang.org/grpc"

	. "github.com/lightstep/lightstep-tracer-go"
	"github.com/lightstep/lightstep-tracer-go/basictracer"
	"github.com/lightstep/lightstep-tracer-go/collectorpb"
	"github.com/lightstep/lightstep-tracer-go/collectorpb/collectorpbfakes"
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

		Context("With grpc enabled", func() {
			const port int = 9090

			var fakeCollector *collectorpbfakes.FakeCollectorServiceServer
			var grpcServer *grpc.Server
			var listener net.Listener

			BeforeEach(func() {
				var err error
				listener, err = net.Listen("tcp", fmt.Sprintf(":%v", port))
				if err != nil {
					fmt.Fprintf(os.Stderr, "Failed to start grpc test server. Error: %v", err)
					// Fail(fmt.Sprintf("Failed to start grpc test server. Error: %v", err))
				}

				fakeCollector = new(collectorpbfakes.FakeCollectorServiceServer)
				grpcServer = grpc.NewServer()
				collectorpb.RegisterCollectorServiceServer(grpcServer, fakeCollector)
				go grpcServer.Serve(listener)

				time.Sleep(2 * time.Second)

				tracer = NewTracer(Options{
					AccessToken:     "0987654321",
					Collector:       Endpoint{"localhost", port, true},
					UseGRPC:         true,
					ReportingPeriod: 1 * time.Millisecond,
					ReportTimeout:   10 * time.Millisecond,
				})
			})

			It("Should behave nicely when called", func(done Done) {
				defer GinkgoRecover()
				By("Not hanging")
				errChan := make(chan error)
				go func() { errChan <- CloseTracer(tracer) }()

				Eventually(errChan, 2, 0.05).Should(Receive(BeNil()))
				By("Stopping communication with the server")
				lastCallCount := fakeCollector.ReportCallCount()
				Consistently(func() int {
					return fakeCollector.ReportCallCount()
				}, 3, 0.05).Should(Equal(lastCallCount))
				By("Allowing other tracers to reconnect to the server")

				tracer = NewTracer(Options{
					AccessToken:     "0987654321",
					Collector:       Endpoint{"localhost", port, true},
					UseGRPC:         true,
					ReportingPeriod: 1 * time.Millisecond,
					ReportTimeout:   10 * time.Millisecond,
				})

				Eventually(func() int {
					return fakeCollector.ReportCallCount()
				}, 2, 0.05).ShouldNot(Equal(lastCallCount))
				fmt.Fprintf(os.Stderr, "6\n")

				close(done)
			}, 2000)

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
