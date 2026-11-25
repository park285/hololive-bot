# syntax=docker/dockerfile:1.29.0@sha256:572cebbc2b15e6f84c077742952674889cb63cd5e9b80abdffcbe587b6f19183
FROM docker.io/valkey/valkey:9.1.3-alpine3.24@sha256:bf09d58e96d6b4524a15396b65b242bbe18beebbcc15a04d8ee75ddac62fae0b
# 공식 서버·UID와 보안 패키지 고정값을 유지하며 영속 상태는 서비스 사용자만 읽고 쓴다.
RUN apk add --no-cache --upgrade 'ca-certificates-bundle>=20261020-r0' 'libcrypto3>=3.5.9-r1' 'libssl3>=3.5.9-r1' 'musl>=1.2.6-r3' 'musl-utils>=1.2.6-r3' 'openssl>=3.5.9-r1' 'zlib>=1.4-r0' \
    && chmod 0700 /data
COPY --chmod=0755 valkey-runtime.sh /usr/local/bin/valkey-runtime
ARG REVISION=unknown
LABEL org.opencontainers.image.revision=$REVISION \
      org.opencontainers.image.source="https://github.com/park285/hololive-bot"
