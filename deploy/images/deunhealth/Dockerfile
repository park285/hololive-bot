# syntax=docker/dockerfile:1.24.0@sha256:87999aa3d42bdc6bea60565083ee17e86d1f3339802f543c0d03998580f9cb89
FROM --platform=$BUILDPLATFORM golang:1.27.1-alpine3.24@sha256:cf6fca6641884b8433441b2b0652976f975e1d0fdd26d177eaaf8596087f3125 AS source
ENV CGO_ENABLED=0 GOTOOLCHAIN=local
ADD --checksum=sha256:d58544678e9f687b40e6e6fe5009b28dd14595460be334c28e795d47d420a765 https://proxy.golang.org/github.com/qdm12/deunhealth/@v/v0.3.1-0.20251221192600-37e5bd45036a.zip /tmp/deunhealth.zip
RUN unzip -q /tmp/deunhealth.zip -d /src
WORKDIR /src/github.com/qdm12/deunhealth@v0.3.1-0.20251221192600-37e5bd45036a
COPY client.patch /tmp/client.patch
RUN apk add --no-cache patch && patch --batch --fuzz=0 -p1 < /tmp/client.patch
COPY dependencies.mod go.mod
COPY dependencies.sum go.sum
COPY docker_test.go.in internal/docker/docker_test.go
RUN test "$(go env GOVERSION)" = go1.27.1 && go mod download

FROM source AS test
RUN apk add --no-cache gcc musl-dev && CGO_ENABLED=1 go test -mod=readonly -race ./internal/docker

FROM source AS build
ARG TARGETOS
ARG TARGETARCH
ARG REVISION=unknown
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH go build -mod=readonly -trimpath -buildvcs=false \
    -ldflags="-s -w -X main.version=37e5bd4-security-refresh -X main.commit=$REVISION" \
    -o /out/app ./cmd/app

FROM scratch
ARG REVISION=unknown
LABEL org.opencontainers.image.revision=$REVISION \
      org.opencontainers.image.source="https://github.com/park285/hololive-bot" \
      io.hololive.deunhealth.source="37e5bd45036a29867fa71e68db3975c0b971c708" \
      io.hololive.deunhealth.toolchain="go1.27.1"
ENV LOG_LEVEL=info HEALTH_SERVER_ADDRESS=127.0.0.1:9999 TZ=America/Montreal
COPY --from=build --chown=1000 /out/app /app
COPY --from=source /src/github.com/qdm12/deunhealth@v0.3.1-0.20251221192600-37e5bd45036a/LICENSE /licenses/deunhealth/LICENSE
ENTRYPOINT ["/app"]
HEALTHCHECK --interval=10s --timeout=5s --start-period=5s --retries=2 CMD ["/app", "healthcheck"]
