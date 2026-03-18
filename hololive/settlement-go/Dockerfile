# syntax=docker/dockerfile:1.7
ARG GO_VERSION=1.26.1
ARG ALPINE_VERSION=3.23

FROM golang:${GO_VERSION}-alpine${ALPINE_VERSION} AS builder

ARG VERSION=dev

WORKDIR /app

COPY shared-go/go.mod shared-go/go.sum ./shared-go/
COPY hololive/hololive-shared/go.mod hololive/hololive-shared/go.sum ./hololive/hololive-shared/
COPY hololive/settlement-go/go.mod hololive/settlement-go/go.sum ./hololive/settlement-go/

WORKDIR /app/hololive/settlement-go
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build go mod download

WORKDIR /app
COPY shared-go ./shared-go
COPY hololive/hololive-shared ./hololive/hololive-shared
COPY hololive/settlement-go ./hololive/settlement-go

WORKDIR /app/hololive/settlement-go

RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -buildvcs=false -ldflags="-s -w -buildid=" -o /dist/bin/settlement ./cmd/settlement

FROM alpine:${ALPINE_VERSION}

RUN apk add --no-cache ca-certificates tini tzdata

ENV TZ=Asia/Seoul

RUN addgroup -g 1000 appuser && \
    adduser -D -u 1000 -G appuser appuser

WORKDIR /app

COPY --from=builder --link --chown=1000:1000 /dist ./

EXPOSE 30002

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD wget -q --spider http://127.0.0.1:30002/health || exit 1

USER appuser

ENTRYPOINT ["/sbin/tini", "--"]
CMD ["./bin/settlement"]
