# syntax=docker/dockerfile:1.7

ARG RUST_VERSION=1.94
ARG GO_VERSION=1.26
ARG ALPINE_VERSION=3.23

# Stage 1: Build Frontend
FROM node:22-alpine AS frontend-builder
WORKDIR /app/frontend

COPY admin-dashboard/frontend/package.json admin-dashboard/frontend/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci --no-audit --no-fund

COPY admin-dashboard/frontend/ .
RUN npm run build

# Stage 2: Build Backend (Rust)
FROM rust:${RUST_VERSION}-alpine${ALPINE_VERSION} AS builder

RUN apk add --no-cache musl-dev curl

WORKDIR /build/admin-dashboard/backend

# 의존성 캐시 레이어
COPY admin-dashboard/backend/Cargo.toml admin-dashboard/backend/Cargo.lock ./
RUN --mount=type=cache,target=/usr/local/cargo/registry \
    --mount=type=cache,target=/build/admin-dashboard/backend/target \
    mkdir -p src && echo 'fn main() {}' > src/main.rs && \
    cargo build --release && \
    rm -rf src

COPY admin-dashboard/backend/src/ ./src/

# 프론트엔드 빌드 결과물 복사 (rust-embed용)
COPY --from=frontend-builder /app/frontend/dist ./static/dist/

RUN --mount=type=cache,target=/usr/local/cargo/registry \
    --mount=type=cache,target=/build/admin-dashboard/backend/target \
    touch src/main.rs && \
    cargo build --release && \
    cp target/release/admin-dashboard /build/admin-dashboard-bin

FROM golang:${GO_VERSION}-alpine${ALPINE_VERSION} AS healthcheck-builder

ARG TARGETOS=linux
ARG TARGETARCH=amd64

WORKDIR /workspace/hololive-bot/hololive/hololive-youtube-producer

COPY hololive/hololive-youtube-producer/go.mod hololive/hololive-youtube-producer/go.sum ./
COPY hololive/hololive-youtube-producer/cmd/runtime/healthcheck ./cmd/runtime/healthcheck

RUN printf 'app:x:1000:1000:app:/tmp:/sbin/nologin\n' > /tmp/passwd && \
    printf 'app:x:1000:\n' > /tmp/group

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath -buildvcs=false -ldflags="-s -w -buildid=" -o /dist/bin/healthcheck ./cmd/runtime/healthcheck

FROM gcr.io/distroless/static-debian12@sha256:20bc6c0bc4d625a22a8fde3e55f6515709b32055ef8fb9cfbddaa06d1760f838

ENV TZ=Asia/Seoul \
    USER=app \
    HOME=/tmp

WORKDIR /app

COPY --from=healthcheck-builder --link /tmp/passwd /etc/passwd
COPY --from=healthcheck-builder --link /tmp/group /etc/group
COPY --from=builder --link --chown=1000:1000 /build/admin-dashboard-bin ./admin-dashboard
COPY --from=healthcheck-builder --link --chown=1000:1000 /dist/bin/healthcheck ./bin/healthcheck

USER 1000:1000

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD ["./bin/healthcheck", "http://127.0.0.1:30190/health"]

EXPOSE 30190

ENTRYPOINT ["./admin-dashboard"]
