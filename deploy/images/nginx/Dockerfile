# syntax=docker/dockerfile:1.29.0@sha256:572cebbc2b15e6f84c077742952674889cb63cd5e9b80abdffcbe587b6f19183
FROM docker.io/library/nginx:1.31.9-alpine-slim@sha256:5bfced78d24d644270bedb0a9968697042e38d5c03fb6354603df025279c1cbe
# Alpine 3.24의 CVE-2026-4738(zlib)·CVE-2026-107891(musl) 수정과 eMudhra 차단 루트를 뺀 CA bundle만 적용하고
# 공식 entrypoint·설정·UID를 보존합니다.
RUN apk add --no-cache --upgrade ca-certificates-bundle=20261020-r0 musl=1.2.6-r3 musl-utils=1.2.6-r3 zlib=1.4-r0
ARG REVISION=unknown
LABEL org.opencontainers.image.revision=$REVISION \
      org.opencontainers.image.source="https://github.com/park285/hololive-bot"
