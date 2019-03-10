# tools
GO=go
DOCKER_PRESENT = $(shell command -v docker 2> /dev/null)

LOCAL_GOPATH = $(PWD)/../../../../

.PHONY: default build test

default: build

test: lightstepfakes/fake_recorder.go
ifeq ($(DOCKER_PRESENT),)
	$(error "docker not found. Please install from https://www.docker.com/")
endif
	docker run --rm -v $(LOCAL_GOPATH):/usergo lightstep/gobuild:latest \
	  ginkgo -race -p /usergo/src/github.com/lightstep/lightstep-tracer-go
	bash -c "! git grep -q '[g]ithub.com/golang/glog'"

build: version.go lightstepfakes/fake_recorder.go
ifeq ($(DOCKER_PRESENT),)
	$(error "docker not found. Please install from https://www.docker.com/")
endif
	${GO} build github.com/lightstep/lightstep-tracer-go

# When releasing significant changes, make sure to update the semantic
# version number in `./VERSION`, merge changes, then run `make release_tag`.
version.go: VERSION
	./tag_version.sh

release_tag:
	git tag -a v`cat ./VERSION`
	git push origin v`cat ./VERSION`
