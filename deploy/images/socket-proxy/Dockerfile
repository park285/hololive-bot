# syntax=docker/dockerfile:1.28.0@sha256:bb22d9815c728170f72750f4e5b0d672e06176142e1d602c7e66c050100b7e5b
FROM --platform=$BUILDPLATFORM golang:1.27.3-alpine3.24@sha256:f2c872bcee949b7be9c490efd02b48a8a2fbf947cdf533f1c58525ec9d8170fd AS source
ENV CGO_ENABLED=0 GOTOOLCHAIN=local
ADD --checksum=sha256:96bfdc7bd730b7717e052649b7df7afbe6eb98462680631e4c46f48f13ee1904 https://codeload.github.com/wollomatic/socket-proxy/tar.gz/refs/tags/1.13.1 /tmp/source.tar.gz
RUN mkdir /src && tar -xzf /tmp/source.tar.gz --strip-components=1 -C /src
WORKDIR /src
RUN test "$(go env GOVERSION)" = go1.27.3

FROM source AS test
RUN --network=none go test ./...

FROM source AS build
ARG TARGETOS
ARG TARGETARCH
# 공식 소스·컴파일 옵션을 유지하고 취약한 표준 라이브러리만 현재 Go로 재빌드합니다.
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH go build -tags=netgo -gcflags=all=-d=checkptr \
    -ldflags="-w -s -X main.version=1.13.1" -trimpath -buildvcs=false -o /out/ ./...

FROM scratch
ARG REVISION=unknown
LABEL org.opencontainers.image.revision=$REVISION \
      org.opencontainers.image.source="https://github.com/park285/hololive-bot" \
      org.opencontainers.image.licenses=MIT \
      io.hololive.socket-proxy.version="1.13.1" \
      io.hololive.socket-proxy.toolchain="go1.27.3"
USER 65534:65534
VOLUME /var/run/docker.sock
EXPOSE 2375
ENTRYPOINT ["/socket-proxy"]
COPY --from=build /out/healthcheck /out/socket-proxy /
COPY --from=source /src/LICENSE /licenses/socket-proxy/LICENSE
