# syntax=docker/dockerfile:1.7

ARG GO_VERSION=1.26.4
ARG ALPINE_VERSION=3.23

# Stage 1: Build Frontend
FROM node:22-alpine AS frontend-builder
WORKDIR /app/frontend

COPY admin-dashboard/frontend/package.json admin-dashboard/frontend/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci --no-audit --no-fund

COPY admin-dashboard/frontend/ .
RUN npm run build

# Stage 2: Build Backend (Go)
# shared-go는 additional context(shared_go_workspace)로 주입되어
# go.mod의 replace ../../../shared-go가 /workspace/shared-go로 해석된다.
FROM golang:${GO_VERSION}-alpine${ALPINE_VERSION} AS builder

ARG VERSION=dev
ARG TARGETOS=linux
ARG TARGETARCH=amd64

RUN apk add --no-cache ca-certificates tzdata

ENV GOWORK=off

WORKDIR /workspace/hololive-bot/admin-dashboard/backend

COPY --from=shared_go_workspace . /workspace/shared-go
COPY admin-dashboard/backend/go.mod admin-dashboard/backend/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

COPY admin-dashboard/backend/ ./
COPY --from=frontend-builder /app/frontend/dist /frontend-dist
RUN rm -rf internal/static/static/dist && \
    mkdir -p internal/static/static/dist && \
    cp -a /frontend-dist/. internal/static/static/dist/

RUN printf 'app:x:1000:1000:app:/tmp:/sbin/nologin\n' > /tmp/passwd && \
    printf 'app:x:1000:\n' > /tmp/group

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -tags sonic -trimpath -buildvcs=false -ldflags="-s -w -buildid= -X main.Version=${VERSION}" -o /dist/bin/admin-dashboard ./cmd/admin-dashboard && \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath -buildvcs=false -ldflags="-s -w -buildid=" -o /dist/bin/healthcheck ./cmd/healthcheck

FROM gcr.io/distroless/static-debian12@sha256:20bc6c0bc4d625a22a8fde3e55f6515709b32055ef8fb9cfbddaa06d1760f838

ENV TZ=Asia/Seoul \
    USER=app \
    HOME=/tmp

WORKDIR /app

COPY --from=builder --link /tmp/passwd /etc/passwd
COPY --from=builder --link /tmp/group /etc/group
COPY --from=builder --link --chown=1000:1000 /dist/bin/admin-dashboard ./admin-dashboard
COPY --from=builder --link --chown=1000:1000 /dist/bin/healthcheck ./bin/healthcheck

USER 1000:1000

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD ["./bin/healthcheck", "http://127.0.0.1:30190/health"]

EXPOSE 30190
ENTRYPOINT ["./admin-dashboard"]
