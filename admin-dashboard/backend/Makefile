GO ?= go
GOLANGCI_LINT ?= golangci-lint
SWAG ?= swag
IMAGE_NAME ?= admin-dashboard

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

.PHONY: test-coverage
test-coverage:
	$(GO) test ./... -coverprofile=coverage.out
	$(GO) tool cover -html=coverage.out -o coverage.html

.PHONY: build
build: lint build-bin

.PHONY: build-bin
build-bin:
	mkdir -p bin
	CGO_ENABLED=0 $(GO) build -tags go_json -trimpath -ldflags="-X main.Version=$(VERSION)" -o bin/admin ./cmd/admin

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
	docker build -t $(IMAGE_NAME):$(shell cat VERSION) -t $(IMAGE_NAME):latest .
	@echo "Built $(IMAGE_NAME):$(shell cat VERSION)"

.PHONY: docker-build-no-bump
docker-build-no-bump:
	docker build -t $(IMAGE_NAME):$(VERSION) -t $(IMAGE_NAME):latest .

.PHONY: clean
clean:
	rm -rf bin/ coverage.out coverage.html docs/

.PHONY: swagger
swagger:
	$(SWAG) init -g cmd/admin/main.go -o ./docs --parseDependency --parseInternal
	@echo "OpenAPI spec generated in docs/"

.PHONY: swagger-fmt
swagger-fmt:
	$(SWAG) fmt -g cmd/admin/main.go
