.PHONY: build test lint fmt clean docker-build

build:
	cargo build --release

test:
	cargo test

lint:
	cargo clippy -- -D warnings

fmt:
	cargo fmt --check

clean:
	cargo clean

docker-build:
	cd ../.. && docker build -f admin-dashboard/Dockerfile -t admin-dashboard:latest .
