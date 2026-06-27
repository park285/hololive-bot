GO ?= go
GOLANGCI_LINT ?= golangci-lint
GOLANGCI_CONFIG ?= ../../.golangci.yml
IMAGE_NAME ?= hololive-api

VERSION := $(shell cat VERSION)
VERSION_PARTS := $(subst ., ,$(VERSION))
MAJOR := $(word 1,$(VERSION_PARTS))
MINOR := $(word 2,$(VERSION_PARTS))
PATCH := $(word 3,$(VERSION_PARTS))

.PHONY: lint
lint:
	$(GOLANGCI_LINT) run -c $(GOLANGCI_CONFIG) ./...

.PHONY: test
test:
	$(GO) test ./...

.PHONY: build-bin
build-bin:
	mkdir -p bin
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags="-X main.Version=$(VERSION)" -o bin/hololive-api ./cmd/hololive-api

.PHONY: bump-patch
bump-patch:
	@echo "$(MAJOR).$(MINOR).$(shell expr $(PATCH) + 1)" > VERSION
	@echo "Version bumped to $$(cat VERSION)"

.PHONY: bump-minor
bump-minor:
	@echo "$(MAJOR).$(shell expr $(MINOR) + 1).0" > VERSION
	@echo "Version bumped to $$(cat VERSION)"

.PHONY: bump-major
bump-major:
	@echo "$(shell expr $(MAJOR) + 1).0.0" > VERSION
	@echo "Version bumped to $$(cat VERSION)"
