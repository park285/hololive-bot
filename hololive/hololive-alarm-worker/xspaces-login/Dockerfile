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

# 로그인은 Chromium headless shell만 사용합니다. 대상 이미지는 실행하지 않고
# 동일 Playwright rootfs에서 쓰지 않는 브라우저를 제거한 뒤 새 layer로 묶습니다.
FROM mcr.microsoft.com/playwright:v1.63.0-noble@sha256:eff16c30e6f3f4af0a03fa4b706120d5e9b0891c344a27d64559aff5900a4a27 AS playwright_upstream
FROM node:24.21.0-bookworm-slim@sha256:0e0ff40c39bc087845bfb27465a0df4ea419520094bc35842ff83dd8cbe6f9b6 AS node_runtime
FROM --platform=$BUILDPLATFORM golang:1.27.1-alpine3.24@sha256:cf6fca6641884b8433441b2b0652976f975e1d0fdd26d177eaaf8596087f3125 AS filtered_playwright
COPY --from=playwright_upstream / /rootfs/
COPY --from=node_runtime /usr/local/bin/node /rootfs/usr/bin/node
RUN test -d /rootfs/ms-playwright/chromium_headless_shell-1243 && \
    test -d /rootfs/ms-playwright/chromium-1243 && \
    test -d /rootfs/ms-playwright/firefox-1543 && \
    test -d /rootfs/ms-playwright/webkit-2359 && \
    test -x /rootfs/usr/bin/node && \
    rm -rf /rootfs/ms-playwright/chromium-1243 /rootfs/ms-playwright/firefox-1543 /rootfs/ms-playwright/webkit-2359 \
           /rootfs/usr/lib/node_modules && \
    rm -f /rootfs/usr/bin/npm /rootfs/usr/bin/npx /rootfs/usr/bin/corepack /rootfs/usr/bin/yarn /rootfs/usr/bin/yarnpkg

FROM scratch
ARG REVISION=unknown
LABEL org.opencontainers.image.title="hololive-x-space-login" \
      org.opencontainers.image.revision="${REVISION}"
WORKDIR /app
COPY --from=filtered_playwright /rootfs/ /
COPY --from=build /agent /app/bin/x-space-login-agent
COPY --from=dependencies /app/xspaces-login /app/xspaces-login
USER 1000:1000
ENV PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
    LANG=C.UTF-8 \
    LC_ALL=C.UTF-8 \
    PLAYWRIGHT_BROWSERS_PATH=/ms-playwright \
    HOME=/tmp
ENTRYPOINT ["/app/bin/x-space-login-agent"]
