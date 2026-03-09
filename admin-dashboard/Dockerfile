# syntax=docker/dockerfile:1.7

ARG GO_VERSION=1.26.1
ARG ALPINE_VERSION=3.23

# Stage 1: Build Frontend
FROM node:22-alpine AS frontend-builder
WORKDIR /app/frontend

# 상위 디렉토리에서 frontend 복사
COPY admin-dashboard/frontend/package.json admin-dashboard/frontend/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci --no-audit --no-fund

COPY admin-dashboard/frontend/ .
RUN npm run build

# Stage 2: Build Backend (with embedded frontend)
FROM golang:${GO_VERSION}-alpine${ALPINE_VERSION} AS builder

# 빌드 시 버전 주입 (docker-compose args 또는 --build-arg로 전달)
ARG VERSION=dev

WORKDIR /build

COPY admin-dashboard/backend/go.mod admin-dashboard/backend/go.sum ./admin-dashboard/backend/
# shared-go 복사 (go.mod replace 대응)
COPY shared-go/go.mod shared-go/go.sum ./shared-go/

WORKDIR /build/admin-dashboard/backend
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build go mod download

COPY admin-dashboard/backend/ .
COPY shared-go/ ../../shared-go/
# 프론트엔드 빌드 결과물 복사 (embed용)
COPY --from=frontend-builder /app/frontend/dist ./internal/static/admin-ui/dist


RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOEXPERIMENT=greenteagc \
    go build -tags=sonic -trimpath -buildvcs=false \
    -ldflags="-s -w -buildid= -X main.Version=${VERSION}" -o admin ./cmd/admin

# Stage 3: Production Runtime (Single Binary)
FROM alpine:${ALPINE_VERSION}

RUN apk add --no-cache ca-certificates tini tzdata

ENV TZ=Asia/Seoul

RUN addgroup -g 1000 appgroup && \
    adduser -u 1000 -G appgroup -s /bin/sh -D appuser

WORKDIR /app

COPY --from=builder --link --chown=appuser:appgroup /build/admin-dashboard/backend/admin ./

ENV GOGC=200 \
    GOMEMLIMIT=230MiB \
    GIN_MODE=release

USER appuser

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:${PORT:-30190}/health || exit 1

EXPOSE 30190

ENTRYPOINT ["/sbin/tini", "--"]
CMD ["./admin"]
