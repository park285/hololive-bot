# syntax=docker/dockerfile:1.27.1@sha256:4edf897a3ffa55b89f906fc8cc78afdb3f1834cc9c7083565e611a8a7d5fe99e
FROM --platform=$BUILDPLATFORM golang:1.27.1-alpine3.24@sha256:8a5910f31396cd4d89662f56c68b3ae31d374308270a1c3bd96672ee5ed43414 AS source
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
    -ldflags="-w -X main.version=37e5bd4-security-refresh -X main.commit=$REVISION" \
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
