# syntax=docker/dockerfile:1.24.0@sha256:87999aa3d42bdc6bea60565083ee17e86d1f3339802f543c0d03998580f9cb89
FROM --platform=$BUILDPLATFORM node:24-alpine3.24@sha256:e67514e5d0f6c46656005e1b693b2ec9d52e80b641307de684d4a015ba7a4eaf AS dependencies
WORKDIR /app/xspaces-login
COPY hololive/hololive-alarm-worker/xspaces-login/package.json hololive/hololive-alarm-worker/xspaces-login/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci --omit=dev --ignore-scripts --no-audit --no-fund
COPY hololive/hololive-alarm-worker/xspaces-login/src ./src
RUN rm -f src/*.test.mjs

FROM --platform=$BUILDPLATFORM golang:1.27.1-alpine3.24@sha256:cf6fca6641884b8433441b2b0652976f975e1d0fdd26d177eaaf8596087f3125 AS build
ENV GOWORK=off
ARG TARGETOS
ARG TARGETARCH
WORKDIR /workspace/hololive-bot
COPY hololive/hololive-shared ./hololive/hololive-shared
COPY hololive/hololive-alarm-worker ./hololive/hololive-alarm-worker
COPY hololive/hololive-dbtest/go.mod hololive/hololive-dbtest/go.sum ./hololive/hololive-dbtest/
WORKDIR /workspace/hololive-bot/hololive/hololive-alarm-worker
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -pgo=off -trimpath -buildvcs=false -ldflags='-s -w -buildid=' -o /agent ./cmd/x-space-login-agent

# 브라우저와 OS 의존성은 로그인 서비스에만 두며 기존 알림 워커 이미지에 추가하지 않습니다.
FROM mcr.microsoft.com/playwright:v1.63.0-noble@sha256:eff16c30e6f3f4af0a03fa4b706120d5e9b0891c344a27d64559aff5900a4a27
ARG REVISION=unknown
LABEL org.opencontainers.image.title="hololive-x-space-login" \
      org.opencontainers.image.revision="${REVISION}"
WORKDIR /app
COPY --from=build /agent /app/bin/x-space-login-agent
COPY --from=dependencies /app/xspaces-login /app/xspaces-login
USER 1000:1000
ENV HOME=/tmp
ENTRYPOINT ["/app/bin/x-space-login-agent"]
