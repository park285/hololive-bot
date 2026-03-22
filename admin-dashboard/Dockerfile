# syntax=docker/dockerfile:1.7

ARG RUST_VERSION=1.94
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

# Stage 3: Production Runtime
FROM alpine:${ALPINE_VERSION}

RUN apk add --no-cache ca-certificates tini tzdata

ENV TZ=Asia/Seoul

RUN addgroup -g 1000 appgroup && \
    adduser -u 1000 -G appgroup -s /bin/sh -D appuser

WORKDIR /app

COPY --from=builder --link --chown=1000:1000 /build/admin-dashboard-bin ./admin-dashboard

USER appuser

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:${PORT:-30190}/health || exit 1

EXPOSE 30190

ENTRYPOINT ["/sbin/tini", "--"]
CMD ["./admin-dashboard"]
