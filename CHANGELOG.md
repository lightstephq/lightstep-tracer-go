# Changelog

## v0.15.0
* Moved compiled proto files (*.pb.go) to another repository (github.com/lightstep/lightstep-tracer-protos)

## v0.14.0
* Flush buffer synchronously on Close
* Flush twice if a flush is already in flight.
* remove gogo in favor of golang/protobuf
* requires grpc-go >= 1.4.0

## v0.13.0
* BasicTracer has been removed.
* Tracer now takes a SpanRecorder as an option.
* Tracer interface now includes Close and Flush.
* Tests redone with ginkgo/gomega.

## v0.12.0 
* Added CloseTracer function to flush and close a lightstep recorder.

## v0.11.0 
* Thrift transport is now deprecated, gRPC is the default.