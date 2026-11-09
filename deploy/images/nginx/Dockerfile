# syntax=docker/dockerfile:1.28.0@sha256:bb22d9815c728170f72750f4e5b0d672e06176142e1d602c7e66c050100b7e5b
FROM docker.io/library/nginx:1.31.8-alpine-slim@sha256:f45171d36c76a8b2f5ef97108a3a8a2294248a6578a0a180edc2d78f7d2edc26
# Alpine 3.24의 CVE-2026-4738(zlib)·CVE-2026-107891(musl) 수정과 eMudhra 차단 루트를 뺀 CA bundle만 적용하고
# 공식 entrypoint·설정·UID를 보존합니다.
RUN apk add --no-cache --upgrade ca-certificates-bundle=20261020-r0 musl=1.2.6-r3 musl-utils=1.2.6-r3 zlib=1.4-r0
ARG REVISION=unknown
LABEL org.opencontainers.image.revision=$REVISION \
      org.opencontainers.image.source="https://github.com/park285/hololive-bot"
