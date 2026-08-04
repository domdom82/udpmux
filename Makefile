
ENSURE_GARDENER_MOD           := $(shell go get github.com/gardener/gardener@$$(go list -m -f "{{.Version}}" github.com/gardener/gardener))
GARDENER_HACK_DIR             := $(shell go list -m -f "{{.Dir}}" github.com/gardener/gardener)/hack
VERSION                       := $(shell cat VERSION)
REPO_ROOT                     := $(shell dirname $(realpath $(lastword $(MAKEFILE_LIST))))
LD_FLAGS                      := "-w $(shell bash $(GARDENER_HACK_DIR)/get-build-ld-flags.sh k8s.io/component-base $(REPO_ROOT)/VERSION "udpmux")"

REGISTRY                      := local
UDP_MUX_IMAGE_REPOSITORY   	  := $(REGISTRY)/udp-mux
UDP_MUX_IMAGE_TAG             := $(VERSION)
UDP_PROXY_IMAGE_REPOSITORY    := $(REGISTRY)/udp-proxy
UDP_PROXY_IMAGE_TAG           := $(VERSION)

ARCH  ?= amd64
OS	  ?= linux
DEBUG ?= true

.PHONY: tidy
tidy:
	@GO111MODULE=on go mod tidy

.PHONY: clean
clean:
	@$(shell rm -f bin/*)

.PHONY: udp-proxy-docker-image
udp-proxy-docker-image:
	@docker buildx build --platform=$(OS)/$(ARCH) --build-arg DEBUG=$(DEBUG) -t $(UDP_PROXY_IMAGE_REPOSITORY):$(UDP_PROXY_IMAGE_TAG) -f Dockerfile --target udp-proxy --rm .

.PHONY: udp-mux-docker-image
udp-mux-docker-image:
	@docker buildx build --platform=$(OS)/$(ARCH) --build-arg DEBUG=$(DEBUG) -t $(UDP_MUX_IMAGE_REPOSITORY):$(UDP_MUX_IMAGE_TAG) -f Dockerfile --target udp-mux --rm .

.PHONY: docker-images
docker-images: udp-proxy-docker-image udp-mux-docker-image

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
		-ldflags $(LD_FLAGS)\
	    ./cmd/udp-proxy/main.go

.PHONY: build-udp-mux
build-udp-mux:
	@CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build -o bin/udp-mux  \
		-ldflags $(LD_FLAGS)\
	    ./cmd/udp-mux/main.go
