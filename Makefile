# tools
GO=go
DOCKER_PRESENT = $(shell command -v docker 2> /dev/null)

default: build
.PHONY: default build test

# Thrift
ifeq (,$(wildcard $(GOPATH)/src/github.com/lightstep/common-go/crouton.thrift))
lightstep_thrift/constants.go:
else
# LightStep-specific: rebuilds the LightStep thrift protocol files.
# Assumes the command is run within the LightStep development
# environment (i.e., private repos are cloned in GOPATH).
lightstep_thrift/constants.go: $(GOPATH)/src/github.com/lightstep/common-go/crouton.thrift
	docker run --rm -v "$(GOPATH)/src/github.com/lightstep/common-go:/data" -v "$(PWD):/out" thrift:0.9.2 \
	  thrift --gen go:package_prefix='github.com/lightstep/lightstep-tracer-go/',thrift_import='github.com/lightstep/lightstep-tracer-go/thrift_0_9_2/lib/go/thrift' -out /out /data/crouton.thrift
	rm -rf lightstep_thrift/reporting_service-remote
endif

test: lightstep_thrift/constants.go \
		lightstep_thrift/lightstep_thriftfakes/fake_reporting_service.go lightstepfakes/fake_recorder.go
ifeq ($(DOCKER_PRESENT),)
	$(error "docker not found. Please install from https://www.docker.com/")
endif
	docker run --rm -v $(GOPATH):/usergo lightstep/gobuild:latest \
	  ginkgo -race -p /usergo/src/github.com/lightstep/lightstep-tracer-go
	docker run --rm -v $(GOPATH):/input:ro lightstep/noglog:latest noglog github.com/lightstep/lightstep-tracer-go

build: lightstep_thrift/constants.go version.go \
		lightstep_thrift/lightstep_thriftfakes/fake_reporting_service.go lightstepfakes/fake_recorder.go
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
