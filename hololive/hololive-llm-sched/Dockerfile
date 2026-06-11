# syntax=docker/dockerfile:1.7
# Copyright (c) 2025 Kapu
#
# Permission is hereby granted, free of charge, to any person obtaining a copy
# of this software and associated documentation files (the "Software"), to deal
# in the Software without restriction, including without limitation the rights
# to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
# copies of the Software, and to permit persons to whom the Software is
# furnished to do so, subject to the following conditions:
#
# The above copyright notice and this permission notice shall be included in
# all copies or substantial portions of the Software.
#
# THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
# IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
# FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
# AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
# LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
# OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
# SOFTWARE.


ARG GO_VERSION=1.26
ARG ALPINE_VERSION=3.23

FROM golang:${GO_VERSION}-alpine${ALPINE_VERSION} AS builder

ARG VERSION=dev
ARG TARGETOS=linux
ARG TARGETARCH=amd64
# GOAMD64/UPX opt-in: 기본은 현 동작(v1, 압축 없음). 배포 호스트가 AVX2(2013+)면
# --build-arg GOAMD64=v3, 이미지 축소가 필요하면 --build-arg UPX_ENABLE=1 로 활성. 빼면 즉시 비활성.
ARG GOAMD64=v1
ARG UPX_ENABLE=
# PGO opt-in: 빈 값이면 -pgo 미적용(현 빌드와 동일). 모듈 기준 상대경로 프로파일을 주면
# --build-arg GO_PGO_FILE=cmd/llm-scheduler/default.pgo 처럼 활성. 빼면 즉시 비활성.
ARG GO_PGO_FILE=
RUN apk add --no-cache git
RUN printf 'app:x:1000:1000:app:/tmp:/sbin/nologin\n' > /tmp/passwd && \
    printf 'app:x:1000:\n' > /tmp/group
ENV GOWORK=/workspace/hololive-bot/go.work

WORKDIR /workspace/hololive-bot

COPY go.mod go.work go.work.sum ./
COPY --from=shared_go_workspace . /workspace/shared-go
COPY --from=iris_client_go_workspace . /workspace/iris-client-go
COPY hololive/hololive-kakao-bot-go ./hololive/hololive-kakao-bot-go
COPY hololive/hololive-youtube-producer ./hololive/hololive-youtube-producer
COPY admin-dashboard/backend ./admin-dashboard/backend
COPY hololive/hololive-shared ./hololive/hololive-shared
COPY hololive/hololive-llm-sched ./hololive/hololive-llm-sched
COPY hololive/hololive-admin-api ./hololive/hololive-admin-api
COPY hololive/hololive-alarm-worker ./hololive/hololive-alarm-worker

WORKDIR /workspace/hololive-bot/hololive/hololive-llm-sched

RUN \
    --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} GOAMD64=${GOAMD64} \
    go build ${GO_PGO_FILE:+-pgo=${GO_PGO_FILE}} -tags sonic -trimpath -buildvcs=false -ldflags="-s -w -buildid= -X main.Version=${VERSION}" -o /dist/bin/llm-scheduler ./cmd/llm-scheduler && \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} GOAMD64=${GOAMD64} \
    go build -trimpath -buildvcs=false -ldflags="-s -w -buildid=" -o /dist/bin/healthcheck ../hololive-youtube-producer/cmd/runtime/healthcheck && \
    if [ -n "${UPX_ENABLE}" ]; then apk add --no-cache upx && upx --best --lzma /dist/bin/llm-scheduler /dist/bin/healthcheck; fi && \
    mkdir -p /dist/internal/domain && \
    cp -r ../hololive-shared/pkg/domain/internal/model/data /dist/internal/domain/

FROM gcr.io/distroless/static-debian12@sha256:20bc6c0bc4d625a22a8fde3e55f6515709b32055ef8fb9cfbddaa06d1760f838

ARG VERSION=dev
LABEL org.opencontainers.image.title="hololive-llm-sched" \
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
    GOMEMLIMIT=192MiB \
    GIN_MODE=release

EXPOSE 30003

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD ["./bin/healthcheck", "https://127.0.0.1:30003/health"]

USER 1000:1000

ENTRYPOINT ["./bin/llm-scheduler"]
