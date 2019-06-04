all: lint test install

deps:
	go get ./...

test: deps
	go test ./...

vet: deps
	go vet ./...

golint: deps
	golint 2> /dev/null; if [ $$? -eq 127 ]; then \
		GO111MODULE=off go get golang.org/x/lint/golint; \
	fi
	golint ./...

errcheck: deps
	errcheck ./... 2> /dev/null; if [ $$? -eq 127 ]; then \
		GO111MODULE=off go get github.com/kisielk/errcheck; \
		errcheck ./...; \
	fi

staticcheck: deps
	staticcheck 2> /dev/null; if [ $$? -eq 127 ]; then \
		GO111MODULE=off go get honnef.co/go/tools/cmd/staticcheck; \
	fi
	staticcheck ./...

check-fmt:
	bash -c "diff --line-format='%L' <(echo -n) <(gofmt -d -s .)"

lint: check-fmt vet golint errcheck

install: deps
	go install ./...
