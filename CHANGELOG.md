# Changelog

## v0.15.0
* Replaces the logger code with event handler code. You now have more flexibility on where you send your errors.
* Added context to the `tracer.Close` and `tracer.Flush` APIs to allow setting a deadline. (Take a look at `Tracerv0_14` for a backward compatible version).
* Added client support for upcoming http transport layer.
* Internal changes to increase performance and code read-ability.

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