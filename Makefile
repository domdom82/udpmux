
ARCH ?= amd64
OS	 ?= linux

.PHONY: tidy
tidy:
	@GO111MODULE=on go mod tidy

.PHONY: clean
clean:
	@$(shell rm -f bin/*)

.PHONY: test
test:
	go test ./...

.PHONY: check
check:
	go fmt ./...
	go vet ./...

.PHONY: build
build: build-udp-proxy build-udp-mux

.PHONY: build-udp-proxy
build-udp-proxy:
	@CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build -o bin/udp-proxy  \
	    ./cmd/udp-proxy/main.go

.PHONY: build-udp-mux
build-udp-mux:
	@CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build -o bin/udp-mux  \
	    ./cmd/udp-mux/main.go
