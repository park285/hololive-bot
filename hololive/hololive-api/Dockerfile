# syntax=docker/dockerfile:1.7
# Copyright 2025 Kapu
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

ARG GO_VERSION=1.26
ARG ALPINE_VERSION=3.23

FROM golang:${GO_VERSION}-alpine${ALPINE_VERSION}@sha256:18b460dd17542c2ba43299a633cf6ebfc1115101509531471d7cfce1019af083 AS builder

ARG VERSION=dev
ARG TARGETOS
ARG TARGETARCH
# GOAMD64/UPX opt-in: 기본은 현 동작(v1, 압축 없음). 배포 호스트가 AVX2(2013+)면
# --build-arg GOAMD64=v3, 이미지 축소가 필요하면 --build-arg UPX_ENABLE=1 로 활성. 빼면 즉시 비활성.
ARG GOAMD64=v1
ARG UPX_ENABLE=
# PGO opt-in: 빈 값이면 -pgo 미적용. 기준 경로를 워커/프로듀서와 동일하게 "모듈 상대"로 통일했다.
# 예: --build-arg GO_PGO_FILE=cmd/hololive-api/default.pgo
ARG GO_PGO_FILE=

RUN apk add --no-cache git
RUN printf 'app:x:1000:1000:app:/tmp:/sbin/nologin\n' > /tmp/passwd && \
    printf 'app:x:1000:\n' > /tmp/group

# go.work 대신 모듈 단독 빌드(GOWORK=off + replace): 워커/프로듀서/대시보드와 전략을 통일하고,
# 형제 서비스 소스 변경이 이 이미지의 COPY 레이어 캐시를 무효화하지 않도록 필요한 것만 좁게 복사한다.
ENV GOWORK=off

WORKDIR /workspace/hololive-bot

COPY --from=shared_go_workspace go.mod go.sum /workspace/shared-go/
COPY --from=iris_client_go_workspace go.mod go.sum /workspace/iris-client-go/
COPY hololive/hololive-shared/go.mod hololive/hololive-shared/go.sum ./hololive/hololive-shared/
COPY hololive/hololive-api/go.mod hololive/hololive-api/go.sum ./hololive/hololive-api/

WORKDIR /workspace/hololive-bot/hololive/hololive-api

# go.mod의 `replace github.com/kapu/hololive-shared => ../hololive-shared` 상대 경로는
# 위 COPY 레이아웃으로 그대로 성립한다. 외부 형제 워크스페이스 두 개만 로컬 경로로 치환한다.
RUN go mod edit -replace github.com/park285/shared-go=/workspace/shared-go && \
    go mod edit -replace github.com/park285/iris-client-go=/workspace/iris-client-go

RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

WORKDIR /workspace/hololive-bot

COPY --from=shared_go_workspace pkg /workspace/shared-go/pkg
COPY --from=iris_client_go_workspace iris /workspace/iris-client-go/iris
COPY --from=iris_client_go_workspace internal /workspace/iris-client-go/internal
COPY --from=iris_client_go_workspace webhook /workspace/iris-client-go/webhook
# alarm-worker 목록과 달리 valkeydedup이 추가로 필요하다: bot 플레인이 non-test로 import한다.
COPY --from=iris_client_go_workspace valkeydedup /workspace/iris-client-go/valkeydedup
COPY hololive/hololive-shared ./hololive/hololive-shared
COPY hololive/hololive-api ./hololive/hololive-api

WORKDIR /workspace/hololive-bot/hololive/hololive-api

RUN go mod edit -replace github.com/park285/shared-go=/workspace/shared-go && \
    go mod edit -replace github.com/park285/iris-client-go=/workspace/iris-client-go

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-$(go env GOARCH)} GOAMD64=${GOAMD64} \
    go build ${GO_PGO_FILE:+-pgo=${GO_PGO_FILE}} -tags sonic -trimpath -buildvcs=false \
      -ldflags="-s -w -buildid= -X main.Version=${VERSION}" \
      -o /dist/bin/hololive-api ./cmd/hololive-api && \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-$(go env GOARCH)} GOAMD64=${GOAMD64} \
    go build -trimpath -buildvcs=false -ldflags="-s -w -buildid=" \
      -o /dist/bin/db-migrate ./cmd/db-migrate && \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-$(go env GOARCH)} GOAMD64=${GOAMD64} \
    go build -trimpath -buildvcs=false -ldflags="-s -w -buildid=" \
      -o /dist/bin/healthcheck ./cmd/healthcheck && \
    if [ -n "${UPX_ENABLE}" ]; then apk add --no-cache upx && upx --best --lzma /dist/bin/hololive-api /dist/bin/healthcheck; fi && \
    mkdir -p /dist/internal/domain && \
    cp -r ../hololive-shared/pkg/domain/internal/model/data /dist/internal/domain/

FROM gcr.io/distroless/static-debian12@sha256:20bc6c0bc4d625a22a8fde3e55f6515709b32055ef8fb9cfbddaa06d1760f838

ARG VERSION=dev
LABEL org.opencontainers.image.title="hololive-api" \
      org.opencontainers.image.source="https://github.com/park285/hololive-bot" \
      org.opencontainers.image.version="${VERSION}"

ENV TZ=Asia/Seoul \
    USER=app \
    HOME=/tmp \
    GIN_MODE=release \
    HEALTHCHECK_CA_CERT_FILE=/run/hololive-bot/certs/hololive-h3.crt \
    HEALTHCHECK_SERVER_NAME=127.0.0.1

WORKDIR /app
COPY --from=builder --link /tmp/passwd /etc/passwd
COPY --from=builder --link /tmp/group /etc/group
COPY --from=builder --link --chown=1000:1000 /dist ./

EXPOSE 30001/tcp 30001/udp 30003/tcp 30003/udp 30006/tcp 30006/udp 30061/tcp 30091/tcp

# compose 헬스체크와 동일한 3-엔드포인트 검사를 이미지에도 내장해 단독 `docker run` 시에도 동작을 일치시킨다.
# (API_SECRET_KEY 미지정 단독 실행 시 unhealthy가 정상 — compose 환경에서는 env_file이 주입한다)
HEALTHCHECK --interval=30s --timeout=5s --start-period=45s --retries=3 \
    CMD ["./bin/healthcheck", "--api-key-env", "API_SECRET_KEY", \
         "https://127.0.0.1:30001/health", "https://127.0.0.1:30003/internal/ready", "https://127.0.0.1:30006/health"]

USER 1000:1000
ENTRYPOINT ["./bin/hololive-api"]
