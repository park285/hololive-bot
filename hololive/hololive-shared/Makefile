GO ?= go
GOLANGCI_LINT ?= golangci-lint

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

.PHONY: clean
clean:
	rm -rf coverage.out coverage.html
