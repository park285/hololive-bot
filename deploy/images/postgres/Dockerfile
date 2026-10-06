# syntax=docker/dockerfile:1.27.1@sha256:4edf897a3ffa55b89f906fc8cc78afdb3f1834cc9c7083565e611a8a7d5fe99e
FROM --platform=$BUILDPLATFORM golang:1.27.1-alpine3.24@sha256:8a5910f31396cd4d89662f56c68b3ae31d374308270a1c3bd96672ee5ed43414 AS gosu
ENV CGO_ENABLED=0 GOTOOLCHAIN=local
ADD --checksum=sha256:b7d2124c326cc8f2c891044b708483ce2f10c360b0b254a0ddfce345f97b6e19 https://proxy.golang.org/github.com/tianon/gosu/@v/v0.0.0-20250923190938-6456aaa0f3c8.zip /tmp/gosu.zip
RUN unzip -q /tmp/gosu.zip -d /src
WORKDIR /src/github.com/tianon/gosu@v0.0.0-20250923190938-6456aaa0f3c8
COPY dependencies.mod go.mod
COPY dependencies.sum go.sum
RUN test "$(go env GOVERSION)" = go1.27.1 && go mod download
ARG TARGETOS
ARG TARGETARCH
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH go build -mod=readonly -trimpath -buildvcs=false -o /out/gosu .

FROM postgres:18.6-alpine@sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873
ARG REVISION=unknown
LABEL org.opencontainers.image.revision=$REVISION \
      org.opencontainers.image.source="https://github.com/park285/hololive-bot" \
      io.hololive.gosu.source="6456aaa0f3c854d199d0f037f068eb97515b7513" \
      io.hololive.gosu.toolchain="go1.27.1"
# 공식 gosu 1.19의 Go 1.24.6 취약 stdlib를 교체하며 entrypoint와 UID/GID 전환은 보존한다.
COPY --from=gosu --chmod=0755 /out/gosu /usr/local/bin/gosu
COPY --from=gosu /src/github.com/tianon/gosu@v0.0.0-20250923190938-6456aaa0f3c8/LICENSE /usr/local/share/licenses/gosu/LICENSE
