.PHONY: default build test
default: build

# Thrift
ifeq (,$(wildcard $(LIGHTSTEP_HOME)/go/src/crouton/crouton.thrift))
# LightStep-specific: rebuilds the LightStep thrift protocol files.  Assumes
# the command is run within the LightStep development environment (i.e. the
# LIGHTSTEP_HOME environment variable is set).
lightstep_thrift/constants.go: $(LIGHTSTEP_HOME)/go/src/crouton/crouton.thrift
	thrift --gen go:package_prefix='github.com/lightstep/lightstep-tracer-go/',thrift_import='github.com/lightstep/lightstep-tracer-go/thrift_0_9_2/lib/go/thrift' -out . $(LIGHTSTEP_HOME)/go/src/crouton/crouton.thrift
	rm -rf lightstep_thrift/reporting_service-remote
else
lightstep_thrift/constants.go:
endif

# gRPC
ifeq (,$(wildcard lightstep-tracer-common/collector.proto))
collectorpb/collector.pb.go:
else
collectorpb/collector.pb.go: lightstep-tracer-common/collector.proto
	docker run --rm -v $(shell pwd)/lightstep-tracer-common:/input:ro -v $(shell pwd)/collectorpb:/output \
	  lightstep/protoc:latest \
	  protoc --go_out=plugins=grpc:/output --proto_path=/input /input/collector.proto
endif

test: lightstep_thrift/constants.go collectorpb/collector.pb.go
	go test github.com/lightstep/lightstep-tracer-go/...

build: thrift proto
	go build github.com/lightstep/lightstep-tracer-go/...
