XGOARCH := amd64
XGOOS := linux
XBIN := $(XGOOS)_$(XGOARCH)/zdns

all: lint test-race install

test:
	go test ./...

test-race:
	go test -race ./...

vet:
	go vet ./...

# https://github.com/golang/go/issues/25922
# https://github.com/golang/go/wiki/Modules#how-can-i-track-tool-dependencies-for-a-module
tools:
	go generate -tags tools ./...

fmt:
	bash -c "diff --line-format='%L' <(echo -n) <(gofmt -d -s .)"

lint: fmt vet tools

install:
	go install ./...

xinstall:
# TODO: Switch to -static flag once 1.14 is released.
# https://github.com/golang/go/issues/26492
	env GOOS=$(XGOOS) GOARCH=$(XGOARCH) CGO_ENABLED=1 \
CC=x86_64-linux-musl-gcc go install -ldflags '-extldflags "-static"' ./...

publish:
ifndef DEST_PATH
	$(error DEST_PATH must be set when publishing)
endif
	rsync -az $(GOPATH)/bin/$(XBIN) $(DEST_PATH)/$(XBIN)
	@sha256sum $(GOPATH)/bin/$(XBIN)
