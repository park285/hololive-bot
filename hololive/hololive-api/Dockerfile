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

ARG GO_VERSION=1.26.5
ARG ALPINE_VERSION=3.24

FROM golang:${GO_VERSION}-alpine${ALPINE_VERSION}@sha256:0178a641fbb4858c5f1b48e34bdaabe0350a330a1b1149aabd498d0699ff5fb2 AS builder

ARG VERSION=dev
ARG REVISION=unknown
ARG TARGETOS
ARG TARGETARCH
# GOAMD64 opt-in: 기본은 v1. 배포 호스트가 AVX2(2013+)면 --build-arg GOAMD64=v3로 활성화한다.
ARG GOAMD64=v1
# Release images are PGO-off until a verifiable adoption design is implemented.

RUN if [ "${REVISION}" != "unknown" ]; then test "${#REVISION}" -eq 40 && case "${REVISION}" in *[!0-9a-f]*) exit 1;; esac; fi

RUN printf 'app:x:1000:1000:app:/tmp:/sbin/nologin\n' > /tmp/passwd && \
    printf 'app:x:1000:\n' > /tmp/group

ENV GOWORK=off \
    GOPROXY=https://proxy.golang.org

WORKDIR /workspace/hololive-bot

COPY hololive/hololive-dbtest/go.mod hololive/hololive-dbtest/go.sum ./hololive/hololive-dbtest/
COPY hololive/hololive-shared/go.mod hololive/hololive-shared/go.sum ./hololive/hololive-shared/
COPY hololive/hololive-api/go.mod hololive/hololive-api/go.sum ./hololive/hololive-api/

WORKDIR /workspace/hololive-bot/hololive/hololive-api

RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

WORKDIR /workspace/hololive-bot

COPY hololive/hololive-shared ./hololive/hololive-shared
COPY hololive/hololive-api ./hololive/hololive-api

WORKDIR /workspace/hololive-bot/hololive/hololive-api

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-$(go env GOARCH)} GOAMD64=${GOAMD64} \
    go build -pgo=off -tags sonic -trimpath -buildvcs=false \
      -ldflags="-s -w -buildid= -X main.Version=${VERSION}" \
      -o /dist/bin/hololive-api ./cmd/hololive-api && \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-$(go env GOARCH)} GOAMD64=${GOAMD64} \
    go build -pgo=off -trimpath -buildvcs=false -ldflags="-s -w -buildid=" \
      -o /dist/bin/db-migrate ./cmd/db-migrate && \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-$(go env GOARCH)} GOAMD64=${GOAMD64} \
    go build -pgo=off -trimpath -buildvcs=false -ldflags="-s -w -buildid=" \
      -o /dist/bin/healthcheck ./cmd/healthcheck && \
    mkdir -p /dist/internal/domain && \
    cp -r ../hololive-shared/pkg/domain/internal/model/data /dist/internal/domain/

FROM gcr.io/distroless/static-debian13@sha256:9197324ba51d9cd071af8505989365c006adf9d6d2067eada25aef00abbb5278

ARG VERSION=dev
ARG REVISION=unknown
LABEL org.opencontainers.image.title="hololive-api" \
      org.opencontainers.image.source="https://github.com/park285/hololive-bot" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${REVISION}"

ENV TZ=Asia/Seoul \
    USER=app \
    HOME=/tmp \
    GIN_MODE=release \
    HEALTHCHECK_CA_CERT_FILE=/run/hololive-bot/certs/hololive-h3.crt \
    HEALTHCHECK_SERVER_NAME=127.0.0.1

WORKDIR /app
COPY --from=builder --link /tmp/passwd /etc/passwd
COPY --from=builder --link /tmp/group /etc/group
COPY --from=builder --link --chown=1000:1000 /dist ./

EXPOSE 30001/tcp 30001/udp 30003/tcp 30003/udp 30006/tcp 30006/udp 30061/tcp 30091/tcp

# compose 헬스체크와 동일한 3-엔드포인트 검사를 이미지에도 내장해 단독 `docker run` 시에도 동작을 일치시킨다.
# (API_SECRET_KEY 미지정 단독 실행 시 unhealthy가 정상 — compose 환경에서는 env_file이 주입한다)
HEALTHCHECK --interval=30s --timeout=5s --start-period=45s --retries=3 \
    CMD ["./bin/healthcheck", "--api-key-env", "API_SECRET_KEY", \
         "https://127.0.0.1:30001/health", "https://127.0.0.1:30003/internal/ready", "https://127.0.0.1:30006/health"]

USER 1000:1000
ENTRYPOINT ["./bin/hololive-api"]
