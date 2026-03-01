CARGO ?= cargo

.PHONY: lint
lint:
	$(CARGO) clippy --workspace --all-targets -- -D warnings

.PHONY: fmt
fmt:
	$(CARGO) fmt --all

.PHONY: fmt-check
fmt-check:
	$(CARGO) fmt --all -- --check

.PHONY: test
test:
	$(CARGO) test --workspace

.PHONY: check
check:
	$(CARGO) check --workspace --all-targets

.PHONY: build
build: lint
	$(CARGO) build --workspace --release

.PHONY: build-only
build-only:
	$(CARGO) build --workspace --release

.PHONY: audit
audit:
	$(CARGO) audit

.PHONY: deny
deny:
	$(CARGO) deny check

.PHONY: clean
clean:
	$(CARGO) clean
