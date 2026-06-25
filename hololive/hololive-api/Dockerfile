# syntax=docker/dockerfile:1.7

FROM golang:1.26.4-alpine3.23 AS builder

RUN apk add --no-cache ca-certificates git tzdata

WORKDIR /workspace/hololive-bot

COPY . .
COPY --from=shared_go_workspace . /workspace/shared-go
COPY --from=iris_client_go_workspace . /workspace/iris-client-go

ARG VERSION=dev
ENV CGO_ENABLED=0
ENV GOWORK=/workspace/hololive-bot/go.work

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -trimpath -ldflags="-s -w -X main.Version=${VERSION}" \
      -o /out/hololive-api ./hololive/hololive-api/cmd/hololive-api

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -trimpath -ldflags="-s -w" \
      -o /out/healthcheck ./hololive/hololive-kakao-bot-go/cmd/healthcheck

FROM alpine:3.23

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S -g 65532 app \
    && adduser -S -D -H -u 65532 -G app app

WORKDIR /app
COPY --from=builder /out/hololive-api /app/bin/hololive-api
COPY --from=builder /out/healthcheck /app/bin/healthcheck

ENV TZ=Asia/Seoul
USER 65532:65532

EXPOSE 30001/tcp 30001/udp 30003/tcp 30003/udp 30006/tcp 30006/udp 30061/tcp 30091/tcp

ENTRYPOINT ["/app/bin/hololive-api"]
