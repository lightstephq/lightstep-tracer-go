package main

import (
	"context"
	"flag"
	"fmt"
	"time"

	"github.com/lightstep/lightstep-tracer-go"
)

var (
	flagAccessToken = flag.String("access_token", "", "Access token to use when reporting spans")
	flagHost        = flag.String("collector_host", "", "Hostname of the collector to which reports should be sent")
	flagPort        = flag.Int("collector_port", 0, "Port of the collector to which reports should be sent")
	flagSecure      = flag.Bool("secure", true, "Whether or not to use TLS")
	flagTransport   = flag.String("transport", "grpc", "The transport mechanism to use. Valid values are: thrift, http, grpc")
	flagOperation   = flag.String("operation_name", "test-operation", "The operation to use for the test span")
)

func init() {
	lightstep.SetGlobalEventHandler(func(event lightstep.Event) {
		fmt.Println(event)
	})
}

func main() {
	flag.Parse()

	var useThrift bool
	var useHTTP bool
	var useGRPC bool

	switch *flagTransport {
	case "thrift":
		useThrift = true
	case "http":
		useHTTP = true
	case "grpc":
		useGRPC = true
	default:
		useGRPC = true
	}

	t := lightstep.NewTracer(
		lightstep.Options{
			AccessToken: *flagAccessToken,
			Collector: lightstep.Endpoint{
				Host:      *flagHost,
				Port:      *flagPort,
				Plaintext: !*flagSecure,
			},
			UseThrift: useThrift,
			UseHttp:   useHTTP,
			UseGRPC:   useGRPC,
		},
	)

	fmt.Println("Sending span...")
	span := t.StartSpan(*flagOperation)
	time.Sleep(100 * time.Millisecond)
	span.Finish()

	fmt.Println("Flushing tracer...")
	lightstep.Flush(context.Background(), t)
	fmt.Println("Done!")
}
