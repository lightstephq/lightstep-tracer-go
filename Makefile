# tools
GO=go
DOCKER_PRESENT = $(shell command -v docker 2> /dev/null)

default: build
.PHONY: default build test

# generate_fake: runs counterfeiter in docker container to generate fake classes
# $(1) output file path
# $(2) input file path
# $(3) class name
define generate_fake
	docker run --rm -v $(GOPATH):/usergo \
	  lightstep/gobuild:latest /bin/bash -c "\
	  cd /usergo/src/github.com/lightstep/lightstep-tracer-go; \
	  counterfeiter -o $(1) $(2) $(3)"
endef
# Thrift
ifeq (,$(wildcard $(GOPATH)/src/github.com/lightstep/common-go/crouton.thrift))
internal/lightstep_thrift/constants.go:
else
# LightStep-specific: rebuilds the LightStep thrift protocol files.
# Assumes the command is run within the LightStep development
# environment (i.e., private repos are cloned in GOPATH).
internal/lightstep_thrift/constants.go: $(GOPATH)/src/github.com/lightstep/common-go/crouton.thrift
	docker run --rm -v "$(GOPATH)/src/github.com/lightstep/common-go:/data" -v "$(PWD):/out" thrift:0.9.2 \
	  thrift --gen go:package_prefix='github.com/lightstep/lightstep-tracer-go/',thrift_import='github.com/lightstep/lightstep-tracer-go/internal/thrift_0_9_2/lib/go/thrift' -out /out /data/crouton.thrift
	rm -rf internal/lightstep_thrift/reporting_service-remote
endif

lightstepfakes/fake_recorder.go: interfaces.go
	$(call generate_fake,lightstepfakes/fake_recorder.go,interfaces.go,SpanRecorder)

internal/lightstep_thrift/internal/lightstep_thriftfakes/fake_reporting_service.go: internal/lightstep_thrift/reportingservice.go
	$(call generate_fake,internal/lightstep_thrift/internal/lightstep_thriftfakes/fake_reporting_service.go,internal/lightstep_thrift/reportingservice.go,ReportingService)

internal/collectorpb/internal/collectorpbfakes/fake_collector_service_client.go: internal/collectorpb/collector.pb.go
	$(call generate_fake,internal/collectorpb/internal/collectorpbfakes/fake_collector_service_client.go,internal/collectorpb/collector.pb.go,CollectorServiceClient)

# gRPC
ifeq (,$(wildcard lightstep-tracer-common/collector.proto))
internal/collectorpb/collector.pb.go:
else
internal/collectorpb/collector.pb.go: lightstep-tracer-common/collector.proto
	docker run --rm -v $(shell pwd)/lightstep-tracer-common:/input:ro -v $(shell pwd)/internal/collectorpb:/output \
	  lightstep/protoc:latest \
	  protoc --go_out=plugins=grpc:/output --proto_path=/input /input/collector.proto
endif

# gRPC
ifeq (,$(wildcard lightstep-tracer-common/collector.proto))
internal/lightsteppb/lightstep_carrier.pb.go:
else
internal/lightsteppb/lightstep_carrier.pb.go: lightstep-tracer-common/lightstep_carrier.proto
	docker run --rm -v $(shell pwd)/lightstep-tracer-common:/input:ro -v $(shell pwd)/internal/lightsteppb:/output \
	  lightstep/protoc:latest \
	  protoc --go_out=plugins=grpc:/output --proto_path=/input /input/lightstep_carrier.proto
endif

test: internal/lightstep_thrift/constants.go internal/collectorpb/collector.pb.go internal/lightsteppb/lightstep_carrier.pb.go \
		internal/collectorpb/internal/collectorpbfakes/fake_collector_service_client.go \
		internal/lightstep_thrift/internal/lightstep_thriftfakes/fake_reporting_service.go lightstepfakes/fake_recorder.go
ifeq ($(DOCKER_PRESENT),)
	$(error "docker not found. Please install from https://www.docker.com/")
endif
	docker run --rm -v $(GOPATH):/usergo lightstep/gobuild:latest \
	  ginkgo -race -p /usergo/src/github.com/lightstep/lightstep-tracer-go
	docker run --rm -v $(GOPATH):/input:ro lightstep/noglog:latest noglog github.com/lightstep/lightstep-tracer-go

build: internal/lightstep_thrift/constants.go internal/collectorpb/collector.pb.go internal/lightsteppb/lightstep_carrier.pb.go \
		internal/collectorpb/internal/collectorpbfakes/fake_collector_service_client.go version.go \
		internal/lightstep_thrift/internal/lightstep_thriftfakes/fake_reporting_service.go lightstepfakes/fake_recorder.go
ifeq ($(DOCKER_PRESENT),)
       	$(error "docker not found. Please install from https://www.docker.com/")
endif	
	${GO} build github.com/lightstep/lightstep-tracer-go/...

# When releasing significant changes, make sure to update the semantic
# version number in `./VERSION`, merge changes, then run `make release_tag`.
version.go: VERSION
	./tag_version.sh

release_tag:
	git tag -a v`cat ./VERSION`
	git push origin v`cat ./VERSION`
