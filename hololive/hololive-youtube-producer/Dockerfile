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

ARG GO_VERSION=1.26
ARG ALPINE_VERSION=3.23

FROM golang:${GO_VERSION}-alpine${ALPINE_VERSION} AS builder

ARG VERSION=dev
ARG TARGETOS
ARG TARGETARCH
# GOAMD64/UPX는 opt-in: 기본은 현 동작과 동일(v1, 압축 없음).
# 배포 호스트 CPU가 AVX2(Haswell 2013+)면 --build-arg GOAMD64=v3,
# 이미지 축소가 필요하면 --build-arg UPX_ENABLE=1 로 켠다. 문제 시 인자만 빼면 즉시 비활성.
ARG GOAMD64=v1
ARG UPX_ENABLE=
# PGO opt-in: 빈 값이면 -pgo 미적용(현 빌드와 동일). 모듈 기준 상대경로 프로파일을 주면
# --build-arg GO_PGO_FILE=cmd/runtime/youtube-producer/default.pgo 처럼 활성. 빼면 즉시 비활성.
ARG GO_PGO_FILE=
RUN apk add --no-cache git
RUN printf 'app:x:1000:1000:app:/tmp:/sbin/nologin\n' > /tmp/passwd && \
    printf 'app:x:1000:\n' > /tmp/group
ENV GOWORK=off

WORKDIR /workspace/hololive-bot

COPY go.mod go.work go.work.sum ./
COPY scripts/deploy/ap-rsync-files.txt ./scripts/deploy/ap-rsync-files.txt
COPY --from=shared_go_workspace go.mod go.sum /workspace/shared-go/
COPY hololive/hololive-shared/go.mod hololive/hololive-shared/go.sum ./hololive/hololive-shared/
COPY hololive/hololive-youtube-producer/go.mod hololive/hololive-youtube-producer/go.sum ./hololive/hololive-youtube-producer/

WORKDIR /workspace/hololive-bot/hololive/hololive-youtube-producer

# Build against the same active shared-go workspace that build-all.sh resolves via additional_contexts.
RUN go mod edit -replace github.com/park285/shared-go=/workspace/shared-go

RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

WORKDIR /workspace/hololive-bot

COPY --from=shared_go_workspace pkg /workspace/shared-go/pkg
COPY hololive/hololive-shared ./hololive/hololive-shared
COPY hololive/hololive-youtube-producer ./hololive/hololive-youtube-producer
RUN set -eu; \
    find hololive/hololive-shared hololive/hololive-youtube-producer -type f -print | \
      while IFS= read -r file; do \
        grep -Fxq "$file" scripts/deploy/ap-rsync-files.txt || rm -f "$file"; \
      done

WORKDIR /workspace/hololive-bot/hololive/hololive-youtube-producer

RUN go mod edit -replace github.com/park285/shared-go=/workspace/shared-go

RUN \
    --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-$(go env GOARCH)} GOAMD64=${GOAMD64} \
    go build ${GO_PGO_FILE:+-pgo=${GO_PGO_FILE}} -tags sonic -trimpath -buildvcs=false -ldflags="-s -w -buildid= -X main.Version=${VERSION}" -o /dist/bin/youtube-producer ./cmd/runtime/youtube-producer && \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-$(go env GOARCH)} GOAMD64=${GOAMD64} \
    go build -trimpath -buildvcs=false -ldflags="-s -w -buildid=" -o /dist/bin/healthcheck ./cmd/runtime/healthcheck && \
    if [ -n "${UPX_ENABLE}" ]; then apk add --no-cache upx && upx --best --lzma /dist/bin/youtube-producer /dist/bin/healthcheck; fi && \
    mkdir -p /dist/internal/domain && \
    cp -r ../hololive-shared/pkg/domain/internal/model/data /dist/internal/domain/

FROM gcr.io/distroless/static-debian12@sha256:20bc6c0bc4d625a22a8fde3e55f6515709b32055ef8fb9cfbddaa06d1760f838

ARG VERSION=dev
LABEL org.opencontainers.image.title="hololive-youtube-producer" \
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

EXPOSE 30005/tcp 30005/udp 30095/tcp

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD ["./bin/healthcheck", "https://127.0.0.1:30005/health"]

USER 1000:1000

ENTRYPOINT ["./bin/youtube-producer"]
