# syntax=docker/dockerfile:1.29.0@sha256:572cebbc2b15e6f84c077742952674889cb63cd5e9b80abdffcbe587b6f19183
# 대상 PostgreSQL의 PGXS·헤더로 빌드하여 musl과 CPU 아키텍처를 맞춥니다.
FROM docker.io/library/postgres:19.0-alpine@sha256:8b0ff4cdf221f0c53d7fcda681d3b461be725c492cd881c5ec9c0ad25023f188 AS extensions
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

FROM docker.io/library/postgres:19.0-alpine@sha256:8b0ff4cdf221f0c53d7fcda681d3b461be725c492cd881c5ec9c0ad25023f188 AS runtime-base
# 고정 base의 brotli·curl·musl·nghttp2·zlib 보안 수정과 eMudhra 차단 루트를 뺀 CA bundle을 적용하며 PostgreSQL 버전은 유지합니다.
RUN apk add --no-cache --upgrade 'brotli-libs>=1.2.1-r0' 'ca-certificates-bundle>=20261020-r0' 'libcurl>=8.23.0-r0' 'libxml2>=2.13.9-r3' 'musl>=1.2.6-r3' 'musl-utils>=1.2.6-r3' 'nghttp2-libs>=1.70.0-r0' 'zlib>=1.4-r0' \
    && rm /usr/local/bin/gosu
ARG REVISION=unknown
LABEL org.opencontainers.image.revision=$REVISION \
      org.opencontainers.image.source="https://github.com/park285/hololive-bot" \
      io.hololive.pg_stat_kcache.version="2.3.2" \
      io.hololive.pg_wait_sampling.version="1.1.11"
COPY --from=extensions /extensions/usr/local/ /usr/local/
COPY --from=extensions /src/pg_stat_kcache/LICENSE /usr/local/share/licenses/pg_stat_kcache/LICENSE
COPY --from=extensions /src/pg_wait_sampling/LICENSE /usr/local/share/licenses/pg_wait_sampling/LICENSE
COPY enable-observability.sql disable-observability.sql read-observability.sql /usr/local/share/hololive/
USER 999:999

# BuildKit의 대상 아키텍처 실행으로 ARM64도 kapu에서 시작·계측을 검증합니다.
FROM runtime-base AS test
USER 0:0
COPY test.sh /test.sh
RUN mkdir -p /var/lib/postgresql/testdata && chown 999:999 /var/lib/postgresql/testdata /var/run/postgresql
USER 999:999
ENV PGDATA=/var/lib/postgresql/testdata
RUN --network=none POSTGRES_HOST_AUTH_METHOD=trust timeout 120 sh /test.sh

FROM runtime-base AS runtime
