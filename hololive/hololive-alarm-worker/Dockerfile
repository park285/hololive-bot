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

ARG GO_VERSION=1.26.2
ARG ALPINE_VERSION=3.23

FROM golang:${GO_VERSION}-alpine${ALPINE_VERSION} AS builder

ARG VERSION=dev
RUN apk add --no-cache git
ENV GOWORK=/workspace/hololive-bot/go.work

WORKDIR /workspace/hololive-bot

COPY go.mod go.work go.work.sum ./
COPY --from=shared_go_workspace . ./shared-go
COPY --from=iris_client_go_workspace . /workspace/iris-client-go
COPY hololive/hololive-dispatcher-go ./hololive/hololive-dispatcher-go
COPY hololive/hololive-llm-sched ./hololive/hololive-llm-sched
COPY hololive/hololive-stream-ingester ./hololive/hololive-stream-ingester
COPY hololive/hololive-shared ./hololive/hololive-shared
COPY hololive/hololive-kakao-bot-go ./hololive/hololive-kakao-bot-go
COPY hololive/hololive-admin-api ./hololive/hololive-admin-api
COPY hololive/hololive-alarm-worker ./hololive/hololive-alarm-worker

WORKDIR /workspace/hololive-bot/hololive/hololive-alarm-worker

RUN \
    --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOEXPERIMENT=greenteagc \
    go build -tags sonic -trimpath -buildvcs=false -ldflags="-s -w -buildid= -X main.Version=${VERSION}" -o /dist/bin/alarm-worker ./cmd/alarm-worker && \
    mkdir -p /dist/internal/domain && \
    cp -r ../hololive-shared/pkg/domain/data /dist/internal/domain/ && \
    cp ../hololive-kakao-bot-go/config.example.json ../hololive-kakao-bot-go/official_japanese_names.json /dist/

FROM alpine:${ALPINE_VERSION}

RUN apk add --no-cache ca-certificates tini tzdata

ENV TZ=Asia/Seoul

RUN addgroup -g 1000 appuser && \
    adduser -D -u 1000 -G appuser appuser

WORKDIR /app

COPY --from=builder --link --chown=1000:1000 /dist ./

ENV GOGC=100 \
    GOMEMLIMIT=320MiB \
    GIN_MODE=release

EXPOSE 30007

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD wget -q --spider http://127.0.0.1:30007/health || exit 1

USER appuser

ENTRYPOINT ["/sbin/tini", "--"]
CMD ["./bin/alarm-worker"]
