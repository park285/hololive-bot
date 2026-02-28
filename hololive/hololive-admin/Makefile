GO ?= go
GOLANGCI_LINT ?= golangci-lint
BINARY_NAME ?= admin-api

.PHONY: lint
lint:
	$(GOLANGCI_LINT) run ./...

.PHONY: fmt
fmt:
	$(GOLANGCI_LINT) run --fix ./...

.PHONY: test
test:
	$(GO) test ./...

.PHONY: vulncheck
vulncheck:
	govulncheck ./...

.PHONY: test-coverage
test-coverage:
	$(GO) test ./... -coverprofile=coverage.out
	$(GO) tool cover -html=coverage.out -o coverage.html

.PHONY: build
build: lint build-bin

.PHONY: build-bin
build-bin:
	mkdir -p bin
	CGO_ENABLED=0 $(GO) build -tags go_json -trimpath -o bin/$(BINARY_NAME) ./cmd/$(BINARY_NAME)

.PHONY: clean
clean:
	rm -rf bin/ coverage.out coverage.html
