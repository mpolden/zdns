XGOARCH := amd64
XGOOS := linux
XBIN := $(XGOOS)_$(XGOARCH)/zdns

all: lint test install

test:
ifdef TRAVIS
	go test -race ./...
else
	go test ./...
endif

vet:
	go vet ./...

golint: install-tools
	golint ./...

staticcheck: install-tools
	staticcheck ./...

fmt:
	bash -c "diff --line-format='%L' <(echo -n) <(gofmt -d -s .)"

lint: fmt vet golint staticcheck

install-tools:
	cd tools && \
		go list -tags tools -f '{{range $$i := .Imports}}{{printf "%s\n" $$i}}{{end}}' | xargs go install

install:
	go install ./...

xinstall:
# TODO: Switch to -static flags once 1.14 is released.
# https://github.com/golang/go/issues/26492
	env GOOS=$(XGOOS) GOARCH=$(XGOARCH) CGO_ENABLED=1 \
CC=x86_64-linux-musl-gcc go install -ldflags '-extldflags "-static"' ./...

publish:
ifndef DEST_PATH
	$(error DEST_PATH must be set when publishing)
endif
	rsync -az $(GOPATH)/bin/$(XBIN) $(DEST_PATH)/$(XBIN)
	@sha256sum $(GOPATH)/bin/$(XBIN)
