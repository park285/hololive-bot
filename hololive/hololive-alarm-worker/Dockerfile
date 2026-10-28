# syntax=docker/dockerfile:1.28.0@sha256:bb22d9815c728170f72750f4e5b0d672e06176142e1d602c7e66c050100b7e5b
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

ARG GO_VERSION=1.27.3
ARG ALPINE_VERSION=3.24
ARG NODE_VERSION=26.12.1

FROM gcr.io/distroless/static-debian13@sha256:15f8637509792e05b97d83451b0b6ccfa4abd548f997e406ac1e72c87fe1fa78 AS runtime-base

FROM --platform=$BUILDPLATFORM node:${NODE_VERSION}-alpine${ALPINE_VERSION}@sha256:9bd229b644e391df00fb989efa732335aefc6170a05d8c163c5c8ca9870da401 AS xspaces-build
WORKDIR /app/xspaces
# checkout umask와 무관하게 runtime uid가 읽도록 context 파일 모드를 정규화한다.
# 기호 COPY --chmod는 BuildKit 전용이라 CONTAINER_CLI=podman(buildah)에서도 같은 결과가 나오도록 RUN chmod로 맞춘다.
COPY hololive/hololive-alarm-worker/xspaces/package.json hololive/hololive-alarm-worker/xspaces/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm chmod u=rwX,go=rX package.json package-lock.json && \
    npm exec --yes --package=corepack@0.36.0 -- corepack npm ci --omit=dev --ignore-scripts --no-audit --no-fund
COPY hololive/hololive-alarm-worker/xspaces/src ./src
RUN chmod -R u=rwX,go=rX ./src && rm -f ./src/*.test.mjs

FROM --platform=$TARGETPLATFORM node:${NODE_VERSION}-alpine${ALPINE_VERSION}@sha256:9bd229b644e391df00fb989efa732335aefc6170a05d8c163c5c8ca9870da401 AS xspaces-runtime

# node base(alpine 3.24.2)의 musl 1.2.6-r2 로더 대신 alpine 3.24.3의 musl 1.2.6-r3(CVE-2026-107891) 로더를 쓴다.
# 고정 이미지에서 복사만 하므로 build-time apk 설치와 대상 아키텍처 실행이 없다.
# node base가 musl 1.2.6-r3 이상을 담으면 이 단계를 지우고 로더도 node 단계에서 복사한다.
FROM --platform=$TARGETPLATFORM alpine:3.24.3@sha256:be2656061b536c1883c4034790e21d0a09c5aaaf51968b89368a3c0371ea2c9a AS musl-loader

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine${ALPINE_VERSION}@sha256:f2c872bcee949b7be9c490efd02b48a8a2fbf947cdf533f1c58525ec9d8170fd AS builder

ARG VERSION=dev
ARG REVISION=unknown
ARG TARGETOS
ARG TARGETARCH
# GOAMD64 opt-in: 기본은 v1. 배포 호스트가 AVX2(2013+)면 --build-arg GOAMD64=v3로 활성화한다.
ARG GOAMD64=v1
# 검증 가능한 도입 설계가 마련되기 전까지 릴리스 이미지는 PGO를 끈다.
RUN if [ "${REVISION}" != "unknown" ]; then test "${#REVISION}" -eq 40 && case "${REVISION}" in *[!0-9a-f]*) exit 1;; esac; fi
RUN printf 'app:x:1000:1000:app:/tmp:/sbin/nologin\n' > /tmp/passwd && \
    printf 'app:x:1000:\n' > /tmp/group
ENV GOWORK=off \
    GOPROXY=https://proxy.golang.org

WORKDIR /workspace/hololive-bot

COPY hololive/hololive-dbtest/go.mod hololive/hololive-dbtest/go.sum ./hololive/hololive-dbtest/
COPY hololive/hololive-shared/go.mod hololive/hololive-shared/go.sum ./hololive/hololive-shared/
COPY hololive/hololive-alarm-worker/go.mod hololive/hololive-alarm-worker/go.sum ./hololive/hololive-alarm-worker/

WORKDIR /workspace/hololive-bot/hololive/hololive-alarm-worker

RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

WORKDIR /workspace/hololive-bot

COPY hololive/hololive-shared ./hololive/hololive-shared
COPY hololive/hololive-alarm-worker ./hololive/hololive-alarm-worker

WORKDIR /workspace/hololive-bot/hololive/hololive-alarm-worker

RUN \
    --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-$(go env GOARCH)} GOAMD64=${GOAMD64} \
    go build -pgo=off -trimpath -buildvcs=false -ldflags="-w -buildid= -X main.Version=${VERSION}" -o /dist/bin/alarm-worker ./cmd/alarm-worker && \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-$(go env GOARCH)} GOAMD64=${GOAMD64} \
    go build -pgo=off -trimpath -buildvcs=false -ldflags="-w -buildid=" -o /dist/bin/healthcheck ./cmd/healthcheck

FROM runtime-base

ARG VERSION=dev
ARG REVISION=unknown
LABEL org.opencontainers.image.title="hololive-alarm-worker" \
      org.opencontainers.image.source="https://github.com/park285/hololive-bot" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${REVISION}"

ENV TZ=Asia/Seoul \
    USER=app \
    HOME=/tmp

WORKDIR /app

COPY --from=builder --link /tmp/passwd /etc/passwd
COPY --from=builder --link /tmp/group /etc/group
COPY --from=builder --link --chown=1000:1000 /dist ./
COPY --from=musl-loader --link /lib/ld-musl-*.so.1 /lib/
COPY --from=xspaces-runtime --link /usr/lib/libgcc_s.so.1 /usr/lib/
COPY --from=xspaces-runtime --link /usr/lib/libstdc++.so.6* /usr/lib/
COPY --from=xspaces-runtime --link /usr/lib/libatomic.so.1* /usr/lib/
COPY --from=xspaces-runtime --link /usr/local/bin/node /usr/local/bin/node
COPY --from=xspaces-build --link --chown=1000:1000 /app/xspaces ./xspaces

ENV GOGC=100 \
    GOMEMLIMIT=320MiB \
    GIN_MODE=release

EXPOSE 30007/tcp 30007/udp 30097/tcp 30067/tcp

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD ["./bin/healthcheck", "https://127.0.0.1:30007/health"]

USER 1000:1000

ENTRYPOINT ["./bin/alarm-worker"]
