# tools
GO=go

.PHONY: default build test install

default: build

build: version.go
	go build ./...

test: build
	go test ./...

install: test
	go install ./...

# When releasing significant changes, make sure to update the semantic
# version number in `./VERSION`, merge changes, then run `make release_tag`.
version.go: VERSION
	./tag_version.sh

release_tag:
	git tag -a v`cat ./VERSION`
	git push origin v`cat ./VERSION`
