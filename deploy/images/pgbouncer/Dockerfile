# syntax=docker/dockerfile:1.28.0@sha256:bb22d9815c728170f72750f4e5b0d672e06176142e1d602c7e66c050100b7e5b
FROM docker.io/library/alpine:3.24@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf AS build
RUN apk add --no-cache build-base libevent-dev openssl-dev
ADD --checksum=sha256:00886a2994b4844cd37110970df777772cfc42a74c400522c82fba509a3f42d9 https://www.pgbouncer.org/downloads/files/1.26.1/pgbouncer-1.26.1.tar.gz /tmp/pgbouncer.tar.gz
RUN mkdir -p /src/pgbouncer \
    && tar -xzf /tmp/pgbouncer.tar.gz -C /src/pgbouncer --strip-components=1
WORKDIR /src/pgbouncer
COPY unreported-startup-parameters.patch /tmp/unreported-startup-parameters.patch
RUN patch --batch --fuzz=0 -p1 < /tmp/unreported-startup-parameters.patch
RUN ./configure --prefix=/usr/local --with-openssl \
    && make -j2 pgbouncer \
    && strip pgbouncer

FROM docker.io/library/alpine:3.24@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf
RUN apk add --no-cache --upgrade libevent libssl3 postgresql18-client zlib=1.4-r0 \
    && addgroup -S -g 10001 pgbouncer \
    && adduser -S -D -H -u 10001 -G pgbouncer -s /sbin/nologin pgbouncer
ARG REVISION=unknown
LABEL org.opencontainers.image.title="iris-stack-pgbouncer" \
      org.opencontainers.image.version="1.26.1-guc1" \
      org.opencontainers.image.revision=$REVISION \
      org.opencontainers.image.source="https://github.com/park285/hololive-bot" \
      io.iris.pgbouncer.upstream-fix="22d0aa1ce20cf3a29eb8ed267caa5570a76a812f" \
      io.hololive.pgbouncer.source="https://www.pgbouncer.org/downloads/files/1.26.1/pgbouncer-1.26.1.tar.gz" \
      io.hololive.pgbouncer.sha256="00886a2994b4844cd37110970df777772cfc42a74c400522c82fba509a3f42d9"
COPY --from=build --chmod=0755 /src/pgbouncer/pgbouncer /usr/local/bin/pgbouncer
COPY --from=build /src/pgbouncer/COPYRIGHT /usr/local/share/licenses/pgbouncer/COPYRIGHT
COPY --chmod=0755 startup-health.sh /usr/local/bin/pgbouncer-startup-health
USER 10001:10001
EXPOSE 6432
ENTRYPOINT ["/usr/local/bin/pgbouncer"]
CMD ["/etc/pgbouncer/pgbouncer.ini"]
