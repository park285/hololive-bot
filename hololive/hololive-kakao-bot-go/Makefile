GO ?= go
GOLANGCI_LINT ?= golangci-lint
IMAGE_NAME ?= hololive-kakao-bot

VERSION := $(shell cat VERSION)
VERSION_PARTS := $(subst ., ,$(VERSION))
MAJOR := $(word 1,$(VERSION_PARTS))
MINOR := $(word 2,$(VERSION_PARTS))
PATCH := $(word 3,$(VERSION_PARTS))

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

.PHONY: test-integration
test-integration:
	INTEGRATION_TEST=true $(GO) test -v -tags=integration ./...

.PHONY: build
build: lint build-bin

.PHONY: build-bin
build-bin:
	mkdir -p bin
	CGO_ENABLED=0 $(GO) build -tags go_json -trimpath -ldflags="-X main.Version=$(VERSION)" -o bin/bot ./cmd/bot

.PHONY: build-alarm-dispatcher
build-alarm-dispatcher:
	mkdir -p bin
	CGO_ENABLED=0 $(GO) build -tags go_json -trimpath -ldflags="-X main.Version=$(VERSION)" -o bin/alarm-dispatcher ../hololive-alarm/cmd/alarm-dispatcher

.PHONY: build-admin-api
build-admin-api:
	mkdir -p bin
	CGO_ENABLED=0 $(GO) build -tags go_json -trimpath -ldflags="-X main.Version=$(VERSION)" -o bin/admin-api ../hololive-admin/cmd/admin-api

.PHONY: build-llm-scheduler
build-llm-scheduler:
	mkdir -p bin
	CGO_ENABLED=0 $(GO) build -tags go_json -trimpath -ldflags="-X main.Version=$(VERSION)" -o bin/llm-scheduler ../hololive-llm-sched/cmd/llm-scheduler

.PHONY: build-stream-ingester
build-stream-ingester:
	mkdir -p bin
	CGO_ENABLED=0 $(GO) build -tags go_json -trimpath -ldflags="-X main.Version=$(VERSION)" -o bin/stream-ingester ../hololive-stream-ingester/cmd/stream-ingester

.PHONY: version
version:
	@echo $(VERSION)

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

.PHONY: docker-build
docker-build: bump-patch
	docker build -f Dockerfile -t $(IMAGE_NAME):$(shell cat VERSION) -t $(IMAGE_NAME):latest .
	@echo "Built $(IMAGE_NAME):$(shell cat VERSION)"

.PHONY: docker-build-no-bump
docker-build-no-bump:
	docker build -f Dockerfile -t $(IMAGE_NAME):$(VERSION) -t $(IMAGE_NAME):latest .

.PHONY: clean
clean:
	rm -rf bin/ coverage.out coverage.html
