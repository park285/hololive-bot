# syntax=docker/dockerfile:1.24.0@sha256:87999aa3d42bdc6bea60565083ee17e86d1f3339802f543c0d03998580f9cb89
# Copyright 2025 Kapu
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

ARG GO_VERSION=1.27.0
ARG ALPINE_VERSION=3.24
ARG NODE_VERSION=24

FROM --platform=$BUILDPLATFORM node:${NODE_VERSION}-alpine${ALPINE_VERSION}@sha256:a0b9bf06e4e6193cf7a0f58816cc935ff8c2a908f81e6f1a95432d679c54fbfd AS youtubejs
WORKDIR /app/youtubejs
COPY hololive/hololive-youtube-collector/youtubejs/package.json hololive/hololive-youtube-collector/youtubejs/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm corepack npm ci --omit=dev --no-audit --no-fund
COPY hololive/hololive-youtube-collector/youtubejs/src ./src
RUN rm -f ./src/*.test.mjs

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine${ALPINE_VERSION}@sha256:4c9fe60190a2a3350ddc51de80d0224b8a6698d12bdfc999fee45ea9d6c46dbc AS builder

ARG VERSION=dev
ARG REVISION=unknown
ARG TARGETOS
ARG TARGETARCH
ARG GOAMD64=v1
RUN if [ "${REVISION}" != "unknown" ]; then test "${#REVISION}" -eq 40 && case "${REVISION}" in *[!0-9a-f]*) exit 1;; esac; fi
RUN printf 'app:x:1000:1000:app:/tmp:/sbin/nologin\n' > /tmp/passwd && \
    printf 'app:x:1000:\n' > /tmp/group
ENV GOWORK=off \
    GOPROXY=https://proxy.golang.org

WORKDIR /workspace/hololive-bot

COPY hololive/hololive-dbtest/go.mod hololive/hololive-dbtest/go.sum ./hololive/hololive-dbtest/
COPY hololive/hololive-shared/go.mod hololive/hololive-shared/go.sum ./hololive/hololive-shared/
COPY hololive/hololive-youtube-collector/go.mod hololive/hololive-youtube-collector/go.sum ./hololive/hololive-youtube-collector/

WORKDIR /workspace/hololive-bot/hololive/hololive-youtube-collector

RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

WORKDIR /workspace/hololive-bot

COPY hololive/hololive-shared ./hololive/hololive-shared
COPY hololive/hololive-youtube-collector ./hololive/hololive-youtube-collector
COPY scripts/build ./scripts/build

WORKDIR /workspace/hololive-bot/hololive/hololive-youtube-collector

RUN \
    --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    extra=; \
    if [ "${REVISION}" = "unknown" ]; then extra=--allow-unknown-revision; fi; \
    sh ../../scripts/build/build-youtube-collector-go.sh \
      --output-dir /dist \
      --version "${VERSION}" \
      --revision "${REVISION}" \
      --goos "${TARGETOS:-linux}" \
      --goarch "${TARGETARCH:-$(go env GOARCH)}" \
      --goamd64 "${GOAMD64}" \
      $extra && \
    mkdir -p /dist/internal/domain && \
    cp -r ../hololive-shared/pkg/domain/internal/model/data /dist/internal/domain/

FROM scratch

ARG VERSION=dev
ARG REVISION=unknown
LABEL org.opencontainers.image.title="hololive-youtube-collector" \
      org.opencontainers.image.source="https://github.com/park285/hololive-bot" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${REVISION}"

ENV TZ=Asia/Seoul \
    USER=app \
    HOME=/tmp \
    YOUTUBEJS_NODE=/usr/local/bin/node \
    YOUTUBEJS_SCRIPT=/app/youtubejs/src/server.mjs

WORKDIR /app

COPY --from=builder --link /tmp/passwd /etc/passwd
COPY --from=builder --link /tmp/group /etc/group
COPY --from=builder --link /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=youtubejs --link /lib/ld-musl-*.so.1 /lib/
COPY --from=youtubejs --link /usr/lib/libgcc_s.so.1 /usr/lib/
COPY --from=youtubejs --link /usr/lib/libstdc++.so.6* /usr/lib/
COPY --from=youtubejs --link /usr/local/bin/node /usr/local/bin/node
COPY --from=youtubejs --link --chmod=1777 /tmp /tmp
COPY --from=builder --link --chown=1000:1000 /dist ./
COPY --from=youtubejs --chown=1000:1000 /app/youtubejs ./youtubejs

ENV GOGC=100 \
    GOMEMLIMIT=320MiB \
    GIN_MODE=release

EXPOSE 30025/tcp 30025/udp 30096/tcp

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD ["./bin/healthcheck", "https://127.0.0.1:30025/ready"]

USER 1000:1000

ENTRYPOINT ["./bin/youtube-collector"]
