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

fmt:
	@sh -c "test -z $$(gofmt -l .)" || { echo "one or more files need to be formatted: try make fmt to fix this automatically"; exit 1; }

lint: fmt vet

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
