# syntax=docker/dockerfile:1.28.0@sha256:bb22d9815c728170f72750f4e5b0d672e06176142e1d602c7e66c050100b7e5b
FROM --platform=$BUILDPLATFORM golang:1.27.3-alpine3.24@sha256:f2c872bcee949b7be9c490efd02b48a8a2fbf947cdf533f1c58525ec9d8170fd AS gosu
ENV CGO_ENABLED=0 GOTOOLCHAIN=local
ADD --checksum=sha256:a656a62069c1ac63dda1f52c3d69c7261dcd5466a22f11dd52d2c2e42fca86f0 https://proxy.golang.org/github.com/tianon/gosu/@v/v0.0.0-20260606051551-40506998e34a.zip /tmp/gosu.zip
RUN unzip -q /tmp/gosu.zip -d /src
WORKDIR /src/github.com/tianon/gosu@v0.0.0-20260606051551-40506998e34a
COPY dependencies.mod go.mod
COPY dependencies.sum go.sum
RUN test "$(go env GOVERSION)" = go1.27.3 && go mod download
ARG TARGETOS
ARG TARGETARCH
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH go build -mod=readonly -trimpath -buildvcs=false -o /out/gosu .

# 대상 PostgreSQL의 PGXS·헤더로 빌드하여 musl과 CPU 아키텍처를 맞춥니다.
FROM postgres:18.6-alpine@sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873 AS extensions
RUN apk add --no-cache build-base
ADD --checksum=sha256:d5da20b5d8808f60dc1427c94d69186ff1d48fa1235ff672f5d0457b370304fb https://codeload.github.com/powa-team/pg_stat_kcache/tar.gz/refs/tags/REL2_3_2 /tmp/pg_stat_kcache.tar.gz
ADD --checksum=sha256:7ecd40b02292cfa1ff43efe27c9742fc107f8b51ff8522f0f021067f8d5e8d95 https://codeload.github.com/postgrespro/pg_wait_sampling/tar.gz/refs/tags/v1.1.11 /tmp/pg_wait_sampling.tar.gz
RUN mkdir -p /src/pg_stat_kcache /src/pg_wait_sampling \
    && tar -xzf /tmp/pg_stat_kcache.tar.gz -C /src/pg_stat_kcache --strip-components=1 \
    && tar -xzf /tmp/pg_wait_sampling.tar.gz -C /src/pg_wait_sampling --strip-components=1 \
    && make -C /src/pg_stat_kcache -j2 PG_CONFIG=/usr/local/bin/pg_config with_llvm=no \
    && make -C /src/pg_stat_kcache PG_CONFIG=/usr/local/bin/pg_config with_llvm=no DESTDIR=/extensions install \
    && make -C /src/pg_wait_sampling -j2 USE_PGXS=1 PG_CONFIG=/usr/local/bin/pg_config with_llvm=no \
    && make -C /src/pg_wait_sampling USE_PGXS=1 PG_CONFIG=/usr/local/bin/pg_config with_llvm=no DESTDIR=/extensions install \
    && strip /extensions/usr/local/lib/postgresql/pg_stat_kcache.so /extensions/usr/local/lib/postgresql/pg_wait_sampling.so

FROM postgres:18.6-alpine@sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873 AS runtime-base
# 고정 base의 brotli·curl·musl·nghttp2·zlib 보안 수정과 eMudhra 차단 루트를 뺀 CA bundle을 적용하며 PostgreSQL 버전은 유지합니다.
RUN apk add --no-cache --upgrade brotli-libs=1.2.1-r0 ca-certificates-bundle=20261020-r0 libcurl=8.23.0-r0 musl=1.2.6-r3 musl-utils=1.2.6-r3 nghttp2-libs=1.70.0-r0 zlib=1.3.2-r1
ARG REVISION=unknown
LABEL org.opencontainers.image.revision=$REVISION \
      org.opencontainers.image.source="https://github.com/park285/hololive-bot" \
      io.hololive.gosu.source="40506998e34a7a679e6dae41374c1b7aa37eb73f" \
      io.hololive.gosu.toolchain="go1.27.3" \
      io.hololive.pg_stat_kcache.version="2.3.2" \
      io.hololive.pg_wait_sampling.version="1.1.11"
# 공식 gosu 1.19의 Go 1.24.6 취약 stdlib를 교체하며 entrypoint와 UID/GID 전환은 보존한다.
COPY --from=gosu --chmod=0755 /out/gosu /usr/local/bin/gosu
COPY --from=gosu /src/github.com/tianon/gosu@v0.0.0-20260606051551-40506998e34a/LICENSE /usr/local/share/licenses/gosu/LICENSE
COPY --from=extensions /extensions/usr/local/ /usr/local/
COPY --from=extensions /src/pg_stat_kcache/LICENSE /usr/local/share/licenses/pg_stat_kcache/LICENSE
COPY --from=extensions /src/pg_wait_sampling/LICENSE /usr/local/share/licenses/pg_wait_sampling/LICENSE
COPY enable-observability.sql disable-observability.sql read-observability.sql /usr/local/share/hololive/

# BuildKit의 대상 아키텍처 실행으로 ARM64도 kapu에서 시작·계측을 검증합니다.
FROM runtime-base AS test
COPY test.sh /test.sh
ENV PGDATA=/var/lib/postgresql/testdata
RUN --network=none POSTGRES_HOST_AUTH_METHOD=trust timeout 120 sh /test.sh
RUN --network=none mkdir -p /var/lib/postgresql/test-user \
    && chown 999:999 /var/lib/postgresql/test-user /var/run/postgresql \
    && PGDATA=/var/lib/postgresql/test-user POSTGRES_HOST_AUTH_METHOD=trust timeout 120 gosu 999:999 sh /test.sh

FROM runtime-base AS runtime
