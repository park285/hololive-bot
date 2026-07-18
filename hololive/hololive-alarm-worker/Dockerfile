# syntax=docker/dockerfile:1.7
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
ARG ALPINE_VERSION=3.23

FROM golang:${GO_VERSION}-alpine${ALPINE_VERSION}@sha256:5e9acfbe29a783d9e2295bc88d30b7c3556e31c8bd21de10a887c4572f58dbd9 AS builder

ARG VERSION=dev
ARG TARGETOS
ARG TARGETARCH
# GOAMD64/UPX opt-in: 기본은 현 동작(v1, 압축 없음). 배포 호스트가 AVX2(2013+)면
# --build-arg GOAMD64=v3, 이미지 축소가 필요하면 --build-arg UPX_ENABLE=1 로 활성. 빼면 즉시 비활성.
ARG GOAMD64=v1
ARG UPX_ENABLE=
# Release images are PGO-off until a verifiable adoption design is implemented.
RUN apk add --no-cache git
RUN printf 'app:x:1000:1000:app:/tmp:/sbin/nologin\n' > /tmp/passwd && \
    printf 'app:x:1000:\n' > /tmp/group
ENV GOWORK=off

WORKDIR /workspace/hololive-bot

COPY --from=shared_go_workspace go.mod go.sum /workspace/shared-go/
COPY --from=iris_client_go_workspace go.mod go.sum /workspace/iris-client-go/
COPY hololive/hololive-dbtest/go.mod hololive/hololive-dbtest/go.sum ./hololive/hololive-dbtest/
COPY hololive/hololive-shared/go.mod hololive/hololive-shared/go.sum ./hololive/hololive-shared/
COPY hololive/hololive-alarm-worker/go.mod hololive/hololive-alarm-worker/go.sum ./hololive/hololive-alarm-worker/

WORKDIR /workspace/hololive-bot/hololive/hololive-alarm-worker

# Build against the same active sibling workspaces that build-all.sh resolves via additional_contexts.
# This avoids fetching private/local shared modules from the network during container builds.
RUN go mod edit -replace github.com/park285/shared-go=/workspace/shared-go && \
    go mod edit -replace github.com/park285/iris-client-go=/workspace/iris-client-go

RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

WORKDIR /workspace/hololive-bot

COPY --from=shared_go_workspace pkg /workspace/shared-go/pkg
COPY --from=iris_client_go_workspace iris /workspace/iris-client-go/iris
COPY --from=iris_client_go_workspace internal /workspace/iris-client-go/internal
COPY --from=iris_client_go_workspace webhook /workspace/iris-client-go/webhook
COPY hololive/hololive-shared ./hololive/hololive-shared
COPY hololive/hololive-alarm-worker ./hololive/hololive-alarm-worker

WORKDIR /workspace/hololive-bot/hololive/hololive-alarm-worker

RUN go mod edit -replace github.com/park285/shared-go=/workspace/shared-go && \
    go mod edit -replace github.com/park285/iris-client-go=/workspace/iris-client-go

RUN \
    --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-$(go env GOARCH)} GOAMD64=${GOAMD64} \
    go build -pgo=off -tags sonic -trimpath -buildvcs=false -ldflags="-s -w -buildid= -X main.Version=${VERSION}" -o /dist/bin/alarm-worker ./cmd/alarm-worker && \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-$(go env GOARCH)} GOAMD64=${GOAMD64} \
    go build -pgo=off -trimpath -buildvcs=false -ldflags="-s -w -buildid=" -o /dist/bin/healthcheck ./cmd/healthcheck && \
    if [ -n "${UPX_ENABLE}" ]; then apk add --no-cache upx && upx --best --lzma /dist/bin/alarm-worker /dist/bin/healthcheck; fi && \
    mkdir -p /dist/internal/domain && \
    cp -r ../hololive-shared/pkg/domain/internal/model/data /dist/internal/domain/

FROM gcr.io/distroless/static-debian12@sha256:20bc6c0bc4d625a22a8fde3e55f6515709b32055ef8fb9cfbddaa06d1760f838

ARG VERSION=dev
LABEL org.opencontainers.image.title="hololive-alarm-worker" \
      org.opencontainers.image.source="https://github.com/park285/hololive-bot" \
      org.opencontainers.image.version="${VERSION}"

ENV TZ=Asia/Seoul \
    USER=app \
    HOME=/tmp

WORKDIR /app

COPY --from=builder --link /tmp/passwd /etc/passwd
COPY --from=builder --link /tmp/group /etc/group
COPY --from=builder --link --chown=1000:1000 /dist ./

ENV GOGC=100 \
    GOMEMLIMIT=320MiB \
    GIN_MODE=release

EXPOSE 30007/tcp 30007/udp 30097/tcp 30067/tcp

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD ["./bin/healthcheck", "https://127.0.0.1:30007/health"]

USER 1000:1000

ENTRYPOINT ["./bin/alarm-worker"]
