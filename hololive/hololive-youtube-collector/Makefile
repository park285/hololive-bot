GO ?= go
GOLANGCI_LINT ?= golangci-lint
GOLANGCI_CONFIG ?= ../../.golangci.yml
BINARY_NAME ?= youtube-collector
ROOT ?= $(abspath $(CURDIR)/../..)

.PHONY: lint
lint:
	$(GOLANGCI_LINT) run -c $(GOLANGCI_CONFIG) ./...

.PHONY: fmt
fmt:
	$(GOLANGCI_LINT) run -c $(GOLANGCI_CONFIG) --fix ./...

.PHONY: test
test:
	$(GO) test ./...

.PHONY: test-prod
test-prod:
	$(ROOT)/scripts/ci/public-pr-go-gate.sh hololive/hololive-youtube-collector test-prod

.PHONY: test-youtubejs
test-youtubejs:
	cd youtubejs && npm test

.PHONY: build-portable
build-portable:
	mkdir -p bin
	CGO_ENABLED=0 $(GO) build -trimpath -o bin/$(BINARY_NAME) ./cmd/runtime/$(BINARY_NAME)

.PHONY: build-prod
build-prod:
	$(ROOT)/scripts/ci/public-pr-go-gate.sh hololive/hololive-youtube-collector build-prod

.PHONY: clean
clean:
	rm -rf bin/ bin-prod/ coverage.out coverage.html
