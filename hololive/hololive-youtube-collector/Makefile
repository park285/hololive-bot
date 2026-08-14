GO ?= go
GOLANGCI_LINT ?= golangci-lint
GOLANGCI_CONFIG ?= ../../.golangci.yml
BINARY_NAME ?= youtube-collector

.PHONY: lint
lint:
	$(GOLANGCI_LINT) run -c $(GOLANGCI_CONFIG) ./...

.PHONY: fmt
fmt:
	$(GOLANGCI_LINT) run -c $(GOLANGCI_CONFIG) --fix ./...

.PHONY: test
test:
	$(GO) test ./...

.PHONY: test-youtubejs
test-youtubejs:
	cd youtubejs && npm test

.PHONY: build-bin
build-bin:
	mkdir -p bin
	CGO_ENABLED=0 $(GO) build -tags go_json -trimpath -o bin/$(BINARY_NAME) ./cmd/runtime/$(BINARY_NAME)

.PHONY: clean
clean:
	rm -rf bin/ coverage.out coverage.html
