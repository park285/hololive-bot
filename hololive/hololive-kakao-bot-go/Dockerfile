# syntax=docker/dockerfile:1.7

ARG GO_VERSION=1.26.0
ARG ALPINE_VERSION=3.23

# Stage 1: Build Backend
FROM golang:${GO_VERSION}-alpine${ALPINE_VERSION} AS builder

# 빌드 시 버전 주입 (docker-compose args 또는 --build-arg로 전달)
ARG VERSION=dev

WORKDIR /app

COPY shared-go/go.mod shared-go/go.sum ./shared-go/
COPY hololive/hololive-shared/go.mod hololive/hololive-shared/go.sum ./hololive/hololive-shared/
COPY hololive/hololive-kakao-bot-go/go.mod hololive/hololive-kakao-bot-go/go.sum ./hololive/hololive-kakao-bot-go/

WORKDIR /app/hololive/hololive-kakao-bot-go
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build go mod download

WORKDIR /app
COPY shared-go ./shared-go
COPY hololive/hololive-shared ./hololive/hololive-shared
COPY hololive/hololive-kakao-bot-go ./hololive/hololive-kakao-bot-go

WORKDIR /app/hololive/hololive-kakao-bot-go

RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOEXPERIMENT=greenteagc \
    go build -tags sonic -trimpath -buildvcs=false -ldflags="-s -w -buildid= -X main.Version=${VERSION}" -o /dist/bin/bot ./cmd/bot && \
    mkdir -p /dist/internal/domain && \
    cp -r ../hololive-shared/pkg/domain/data /dist/internal/domain/ && \
    cp config.example.json config.json official_japanese_names.json /dist/

# Stage 2: Production Runtime
FROM alpine:${ALPINE_VERSION}

RUN apk add --no-cache ca-certificates tini tzdata

ENV TZ=Asia/Seoul

RUN addgroup -g 1000 appuser && \
    adduser -D -u 1000 -G appuser appuser

WORKDIR /app

COPY --from=builder --link --chown=appuser:appuser /dist ./

ENV GOGC=200 \
    GOMEMLIMIT=450MiB \
    GIN_MODE=release

EXPOSE 30001

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD wget -q --spider http://127.0.0.1:30001/health || exit 1

USER appuser

ENTRYPOINT ["/sbin/tini", "--"]
CMD ["./bin/bot"]
