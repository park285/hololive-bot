# syntax=docker/dockerfile:1.7

ARG GO_VERSION=1.26
ARG ALPINE_VERSION=3.23

FROM golang:${GO_VERSION}-alpine${ALPINE_VERSION} AS builder

ARG VERSION=dev
ARG TARGETOS
ARG TARGETARCH
ARG GOAMD64=v1
# GO_PGO_FILE is repo-root relative because this Dockerfile builds from /workspace/hololive-bot.
# Example: --build-arg GO_PGO_FILE=hololive/hololive-api/cmd/hololive-api/default.pgo
ARG GO_PGO_FILE=
# Optional binary compression. Keep disabled by default for predictable startup/debugging.
ARG UPX_ENABLE=

RUN apk add --no-cache git
RUN printf 'app:x:1000:1000:app:/tmp:/sbin/nologin\n' > /tmp/passwd && \
    printf 'app:x:1000:\n' > /tmp/group

WORKDIR /workspace/hololive-bot

# Keep the build context narrow. The old COPY . . invalidated this image cache whenever
# unrelated repository files changed. These paths are the workspaces referenced by go.work.
COPY go.mod go.work go.work.sum ./
COPY --from=shared_go_workspace . /workspace/shared-go
COPY --from=iris_client_go_workspace . /workspace/iris-client-go
COPY admin-dashboard/backend ./admin-dashboard/backend
COPY hololive/hololive-shared ./hololive/hololive-shared
COPY hololive/hololive-api ./hololive/hololive-api
COPY hololive/hololive-alarm-worker ./hololive/hololive-alarm-worker
COPY hololive/hololive-youtube-producer ./hololive/hololive-youtube-producer

ENV GOWORK=/workspace/hololive-bot/go.work

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-$(go env GOARCH)} GOAMD64=${GOAMD64} \
    go build ${GO_PGO_FILE:+-pgo=${GO_PGO_FILE}} -tags sonic -trimpath -buildvcs=false \
      -ldflags="-s -w -buildid= -X main.Version=${VERSION}" \
      -o /dist/bin/hololive-api ./hololive/hololive-api/cmd/hololive-api && \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-$(go env GOARCH)} GOAMD64=${GOAMD64} \
    go build -trimpath -buildvcs=false -ldflags="-s -w -buildid=" \
      -o /dist/bin/healthcheck ./hololive/hololive-api/cmd/healthcheck && \
    if [ -n "${UPX_ENABLE}" ]; then apk add --no-cache upx && upx --best --lzma /dist/bin/hololive-api /dist/bin/healthcheck; fi && \
    mkdir -p /dist/internal/domain && \
    cp -r hololive/hololive-shared/pkg/domain/internal/model/data /dist/internal/domain/

FROM gcr.io/distroless/static-debian12@sha256:20bc6c0bc4d625a22a8fde3e55f6515709b32055ef8fb9cfbddaa06d1760f838

ARG VERSION=dev
LABEL org.opencontainers.image.title="hololive-api" \
      org.opencontainers.image.source="https://github.com/park285/hololive-bot" \
      org.opencontainers.image.version="${VERSION}"

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

USER 1000:1000
ENTRYPOINT ["./bin/hololive-api"]
