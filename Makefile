
ENSURE_GARDENER_MOD           := $(shell go get github.com/gardener/gardener@$$(go list -m -f "{{.Version}}" github.com/gardener/gardener))
ENSURE_GARDENER_TOOLS_MOD     := $(shell go get github.com/gardener/gardener/hack/tools@$$(go list -m -f "{{.Version}}" github.com/gardener/gardener/hack/tools))
GARDENER_HACK_DIR             := $(shell go list -m -f "{{.Dir}}" github.com/gardener/gardener)/hack
VERSION                       := $(shell cat VERSION)
REPO_ROOT                     := $(shell dirname $(realpath $(lastword $(MAKEFILE_LIST))))
HACK_DIR                      := $(REPO_ROOT)/hack
LD_FLAGS                      := "-w $(shell bash $(GARDENER_HACK_DIR)/get-build-ld-flags.sh k8s.io/component-base $(REPO_ROOT)/VERSION "udpmux")"

REGISTRY                      := local
UDP_MUX_IMAGE_REPOSITORY   	  := $(REGISTRY)/udp-mux
UDP_MUX_IMAGE_TAG             := $(VERSION)
UDP_PROXY_IMAGE_REPOSITORY    := $(REGISTRY)/udp-proxy
UDP_PROXY_IMAGE_TAG           := $(VERSION)

ARCH  ?= amd64
OS	  ?= linux
DEBUG ?= true

TOOLS_DIR := $(HACK_DIR)/tools
include $(GARDENER_HACK_DIR)/tools.mk


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
test: bench
	go test -race ./...

.PHONY: bench
bench:
	go test -bench=. -run=xxx ./...

.PHONY: check
check: sast-report
	go fmt ./...
	go vet ./...

.PHONY: generate
generate:
	@$(MAKE) format
	@GARDENER_HACK_DIR=$(GARDENER_HACK_DIR) RENOVATE_CONFIG=$(REPO_ROOT)/.github/renovate.json5 bash $(GARDENER_HACK_DIR)/generate-renovate-ignore-deps.sh

.PHONY: check-generate
check-generate:
	@bash $(GARDENER_HACK_DIR)/check-generate.sh $(REPO_ROOT)

.PHONY: format
format: $(GOIMPORTS) $(GOIMPORTSREVISER)
	@bash $(GARDENER_HACK_DIR)/format.sh ./cmd ./pkg

.PHONY: sast
sast: $(GOSEC)
	@./hack/sast.sh

.PHONY: sast-report
sast-report: $(GOSEC)
	@./hack/sast.sh --gosec-report true

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

.PHONY: build-test-images
build-test-images: docker-images
	@docker buildx build --platform=$(OS)/$(ARCH) -t local/vpn-node:latest -f test/e2e/common/Dockerfile.vpn-node test/e2e/common

.PHONY: gen-pki
gen-pki: build-test-images
	@bash test/e2e/common/pki/gen-pki.sh

.PHONY: test-e2e
test-e2e: build-test-images gen-pki
	@bash hack/run-with-retry.sh test/e2e/vpn-single-client/run.sh
	@bash hack/run-with-retry.sh test/e2e/vpn-double-client/run.sh
	@bash hack/run-with-retry.sh test/e2e/vpn-double-server/run.sh
