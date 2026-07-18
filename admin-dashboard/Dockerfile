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
ARG NODE_VERSION=22

FROM node:${NODE_VERSION}-alpine${ALPINE_VERSION}@sha256:8516dce0483394d5708d4b2ee6cacb79fb1d617ea4e2787c2120bcca92ce372e AS frontend-builder
WORKDIR /app/frontend

COPY admin-dashboard/frontend/package.json admin-dashboard/frontend/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci --no-audit --no-fund

COPY admin-dashboard/frontend/ .
RUN npm run build

FROM golang:${GO_VERSION}-alpine${ALPINE_VERSION}@sha256:5e9acfbe29a783d9e2295bc88d30b7c3556e31c8bd21de10a887c4572f58dbd9 AS builder

ARG VERSION=dev
ARG TARGETOS
ARG TARGETARCH
ARG GOAMD64=v1
ARG UPX_ENABLE=
RUN apk add --no-cache git
RUN printf 'app:x:1000:1000:app:/tmp:/sbin/nologin\n' > /tmp/passwd && \
    printf 'app:x:1000:\n' > /tmp/group

ENV GOWORK=off

WORKDIR /workspace/hololive-bot

COPY --from=shared_go_workspace go.mod go.sum /workspace/shared-go/
COPY admin-dashboard/backend/go.mod admin-dashboard/backend/go.sum ./admin-dashboard/backend/
COPY hololive/hololive-dbtest/go.mod hololive/hololive-dbtest/go.sum ./hololive/hololive-dbtest/
COPY hololive/hololive-shared/go.mod hololive/hololive-shared/go.sum ./hololive/hololive-shared/

WORKDIR /workspace/hololive-bot/admin-dashboard/backend

# Build against the same active shared-go workspace that build-all.sh resolves via additional_contexts.
RUN go mod edit -replace github.com/park285/shared-go=/workspace/shared-go

RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

WORKDIR /workspace/hololive-bot

COPY --from=shared_go_workspace pkg /workspace/shared-go/pkg
COPY admin-dashboard/backend ./admin-dashboard/backend
COPY hololive/hololive-shared ./hololive/hololive-shared

WORKDIR /workspace/hololive-bot/admin-dashboard/backend

RUN go mod edit -replace github.com/park285/shared-go=/workspace/shared-go

COPY --from=frontend-builder /app/frontend/dist /frontend-dist
RUN rm -rf internal/static/static/dist && \
    mkdir -p internal/static/static/dist && \
    cp -a /frontend-dist/. internal/static/static/dist/

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-$(go env GOARCH)} GOAMD64=${GOAMD64} \
    go build -tags sonic -trimpath -buildvcs=false -ldflags="-s -w -buildid= -X main.Version=${VERSION}" -o /dist/bin/admin-dashboard ./cmd/admin-dashboard && \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-$(go env GOARCH)} GOAMD64=${GOAMD64} \
    go build -trimpath -buildvcs=false -ldflags="-s -w -buildid=" -o /dist/bin/healthcheck ./cmd/healthcheck && \
    if [ -n "${UPX_ENABLE}" ]; then apk add --no-cache upx && upx --best --lzma /dist/bin/admin-dashboard /dist/bin/healthcheck; fi

FROM gcr.io/distroless/static-debian12@sha256:20bc6c0bc4d625a22a8fde3e55f6515709b32055ef8fb9cfbddaa06d1760f838

ARG VERSION=dev
LABEL org.opencontainers.image.title="admin-dashboard" \
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
    GOMEMLIMIT=96MiB \
    GIN_MODE=release

EXPOSE 30190

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD ["./bin/healthcheck", "http://127.0.0.1:30190/health"]

USER 1000:1000

ENTRYPOINT ["./bin/admin-dashboard"]
