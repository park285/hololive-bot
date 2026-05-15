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


ARG GO_VERSION=1.26.3
ARG ALPINE_VERSION=3.23

# Stage 1: Build Backend
FROM golang:${GO_VERSION}-alpine${ALPINE_VERSION} AS builder

# 빌드 시 버전 주입 (docker-compose args 또는 --build-arg로 전달)
ARG VERSION=dev
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN apk add --no-cache git
RUN printf 'app:x:1000:1000:app:/tmp:/sbin/nologin\n' > /tmp/passwd && \
    printf 'app:x:1000:\n' > /tmp/group
ENV GOWORK=/workspace/hololive-bot/go.work

WORKDIR /workspace/hololive-bot

COPY go.mod go.work go.work.sum ./
COPY --from=shared_go_workspace . ./shared-go
COPY --from=iris_client_go_workspace . /workspace/iris-client-go
COPY hololive/hololive-llm-sched ./hololive/hololive-llm-sched
COPY hololive/hololive-stream-ingester ./hololive/hololive-stream-ingester
COPY hololive/hololive-shared ./hololive/hololive-shared
COPY hololive/hololive-kakao-bot-go ./hololive/hololive-kakao-bot-go
COPY hololive/hololive-admin-api ./hololive/hololive-admin-api
COPY hololive/hololive-alarm-worker ./hololive/hololive-alarm-worker

WORKDIR /workspace/hololive-bot/hololive/hololive-kakao-bot-go

RUN \
    --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -tags sonic -trimpath -buildvcs=false -ldflags="-s -w -buildid= -X main.Version=${VERSION}" -o /dist/bin/bot ./cmd/bot && \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath -buildvcs=false -ldflags="-s -w -buildid=" -o /dist/bin/healthcheck ../hololive-stream-ingester/cmd/runtime/healthcheck && \
    mkdir -p /dist/internal/domain && \
    cp -r ../hololive-shared/pkg/domain/data /dist/internal/domain/ && \
    cp config.example.json official_japanese_names.json /dist/

FROM gcr.io/distroless/static-debian12@sha256:20bc6c0bc4d625a22a8fde3e55f6515709b32055ef8fb9cfbddaa06d1760f838

ENV TZ=Asia/Seoul \
    USER=app \
    HOME=/tmp

WORKDIR /app

COPY --from=builder --link /tmp/passwd /etc/passwd
COPY --from=builder --link /tmp/group /etc/group
COPY --from=builder --link --chown=1000:1000 /dist ./

ENV GOGC=200 \
    GOMEMLIMIT=450MiB \
    GIN_MODE=release \
    HEALTHCHECK_CA_CERT_FILE=/run/hololive-bot/certs/hololive-h3.crt \
    HEALTHCHECK_SERVER_NAME=100.100.1.3

EXPOSE 30001

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD ["./bin/healthcheck", "https://127.0.0.1:30001/health"]

USER 1000:1000

ENTRYPOINT ["./bin/bot"]
