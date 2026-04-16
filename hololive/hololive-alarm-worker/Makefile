GO ?= go
GOLANGCI_LINT ?= golangci-lint
GOLANGCI_CONFIG ?= ../../.golangci.yml
IMAGE_NAME ?= hololive-alarm-worker

VERSION := $(shell cat VERSION)
VERSION_PARTS := $(subst ., ,$(VERSION))
MAJOR := $(word 1,$(VERSION_PARTS))
MINOR := $(word 2,$(VERSION_PARTS))
PATCH := $(word 3,$(VERSION_PARTS))

.PHONY: lint
lint:
	$(GOLANGCI_LINT) run -c $(GOLANGCI_CONFIG) ./...

.PHONY: fmt
fmt:
	$(GOLANGCI_LINT) run -c $(GOLANGCI_CONFIG) --fix ./...

.PHONY: test
test:
	$(GO) test ./...

.PHONY: build-bin
build-bin:
	mkdir -p bin
	CGO_ENABLED=0 $(GO) build -tags go_json -trimpath -ldflags="-X main.Version=$(VERSION)" -o bin/alarm-worker ./cmd/alarm-worker

.PHONY: version
version:
	@echo $(VERSION)

.PHONY: bump-patch
bump-patch:
	@echo "$(MAJOR).$(MINOR).$(shell expr $(PATCH) + 1)" > VERSION
	@echo "Version bumped to $$(cat VERSION)"
