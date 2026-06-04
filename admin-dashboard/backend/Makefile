.PHONY: build test lint fmt vet staticcheck vulncheck quality clean docker-build

GO ?= go
STATICCHECK ?= staticcheck
GOVULNCHECK ?= govulncheck

build:
	$(GO) build -trimpath -buildvcs=false ./cmd/admin-dashboard

test:
	$(GO) test -count=1 ./...

vet:
	$(GO) vet ./...

staticcheck:
	$(STATICCHECK) ./...

vulncheck:
	$(GOVULNCHECK) ./...

lint: fmt vet staticcheck

fmt:
	@test -z "$$($(GO)fmt -l $$(find . -name '*.go' -not -path './vendor/*'))" || (echo "gofmt required" >&2; $(GO)fmt -l $$(find . -name '*.go' -not -path './vendor/*') >&2; exit 1)

quality:
	../../scripts/ci/admin-dashboard-go-ci.sh

clean:
	rm -rf dist

docker-build:
	cd ../.. && docker build -f admin-dashboard/Dockerfile -t admin-dashboard:latest .
