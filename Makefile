.PHONY: build deb test check clean

VERSION ?= development
DEB_VERSION ?= 0.1.0
LDFLAGS := -s -w -X main.version=$(VERSION)

build:
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/local-parental-control ./cmd/local-parental-control
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/lpctl ./cmd/lpctl

deb:
	LPC_VERSION=$(DEB_VERSION) ./scripts/build-deb.sh

test:
	go test ./...

check:
	test -z "$$(gofmt -l .)"
	go vet ./...
	go test -race ./...

clean:
	rm -rf bin dist coverage.out
