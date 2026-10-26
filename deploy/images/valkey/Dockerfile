# syntax=docker/dockerfile:1.28.0@sha256:bb22d9815c728170f72750f4e5b0d672e06176142e1d602c7e66c050100b7e5b
FROM valkey/valkey:9.1.2-alpine3.24@sha256:48332870af354a799964c0012ae1194a0bf2bf894eb508f945810596dc2d8d11
# 공식 서버·entrypoint·UID를 보존하고 Alpine의 musl·zlib 보안 수정만 적용합니다.
RUN apk add --no-cache --upgrade musl=1.2.6-r3 musl-utils=1.2.6-r3 zlib=1.3.2-r1
ARG REVISION=unknown
LABEL org.opencontainers.image.revision=$REVISION \
      org.opencontainers.image.source="https://github.com/park285/hololive-bot"
