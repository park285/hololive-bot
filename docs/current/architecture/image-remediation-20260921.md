# 예외 없는 인프라 이미지 빌드

취약 패키지와 바이너리 자체를 수정한다. 2026-09-14의 비도달 예외 문서는 과거 근거이며 현재 allowlist가 아니다. 네 Trivy 예외 YAML과 예외 전용 checker를 제거했고 최종 scanner는 모든 심각도를 검사한다. `/dev/null` config·ignorefile과 `ignore-unfixed=false`를 사용한다. OS·비Go 패키지 보고는 차단하고, Go 모듈 보고는 아래 실제 바이너리의 패키지 존재 판정을 추가한다.

## Go 모듈 보고와 실제 패키지 판정

Trivy의 Go 모듈 버전 보고만으로는 같은 모듈 안의 미포함 패키지를 구분할 수 없다. Go 릴리스 빌드는 `-s`를 사용하지 않아 심볼을 보존한다. `-w`로 DWARF만 제거할 수 있지만 `govulncheck -mode=extract`에 실제 패키지·심볼 목록이 없으면 검사를 실패시킨다. govulncheck의 stripped-binary module fallback을 안전 근거로 사용하지 않는다.

scanner는 로컬 태그를 불변 image ID로 고정하고, Trivy가 보고한 바이너리를 실행하지 않고 추출한다. 고정 govulncheck v1.8.0의 `-mode=binary -scan=package -format=openvex`로 각 바이너리의 증거를 생성한다. 모든 Go 보고의 advisory ID 또는 공식 alias와 정확한 module/version PURL이 `not_affected / vulnerable_code_not_present`에 대응해야만 모듈 단위 과잉 판정으로 분류한다. 패키지가 포함됐으나 호출되지 않는다는 `vulnerable_code_not_in_execute_path`는 허용하지 않는다. 모르는 advisory, 누락된 증거, 잘못된 아키텍처, 실제 취약 패키지와 분석 오류는 차단한다.

수동 CVE allowlist나 저장된 VEX 입력을 받지 않는다. 원시 Trivy finding을 삭제하거나 심각도를 제외하지 않으며, 매 실행마다 새 evidence 디렉터리에 image/target identity, 바이너리 SHA-256, 추출 심볼, 생성된 VEX와 원본 보고를 보존한다. 출력의 `Go package-absence verified`는 해당 패키지가 실제 artifact에 없다는 뜻이며 원본 Trivy finding 수가 0이라는 뜻은 아니다.

## 소유 경로

- `deploy/images/nginx`는 공식 `1.31.8-alpine-slim`의 multi-arch index `f45171d36c76a8b2f5ef97108a3a8a2294248a6578a0a180edc2d78f7d2edc26`를 기반으로 Alpine 보안 패키지(`ca-certificates-bundle`·`musl`·`zlib`) 고정값만 더하고 공식 entrypoint·설정·UID를 보존한다.
- `deploy/images/valkey`는 공식 `9.1.3-alpine3.24` index `bf09d58e96d6b4524a15396b65b242bbe18beebbcc15a04d8ee75ddac62fae0b`를 기반으로 같은 보안 패키지 고정값과 `valkey-runtime.sh`를 더하고 공식 서버·UID를 유지한다.
- `deploy/images/postgres`는 공식 PostgreSQL 18.6 index `77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873`를 기반으로 한다. gosu 1.19 source `40506998e34a7a679e6dae41374c1b7aa37eb73f`를 Go 1.27.2·x/sys v0.49.0·moby/sys/user v0.4.1로 재빌드해 `/usr/local/bin/gosu`만 교체한다. 기존 PostgreSQL entrypoint·PGDATA·사용자·initdb contract는 유지한다.
- 이 문서 작성 당시 쓰던 socket-proxy 공식 image와 `deploy/images/deunhealth` 재빌드 image는 2026-11-04 중앙 런타임의 Podman Quadlet 전환(#735)에서 제거했다.

`dependencies.mod`·`dependencies.sum`은 재빌드의 module graph 정본이다. upstream module zip은 Go proxy에서 받고 Dockerfile의 SHA-256으로 검증한다. Docker build에서는 `GOTOOLCHAIN=local`, compiler identity 검사, `-mod=readonly`로 추가 버전 선택을 막는다. 업스트림 라이선스는 최종 이미지에 포함한다.

## 빌드·검증·전달

모든 빌드와 검증은 kapu에서 수행한다. `scripts/ci/test-infra-images.sh`는 임시 PostgreSQL을 root와 UID 999로 시작해 `deploy/images/postgres/test.sh`의 SQL 검사를 실행하며 recurring security job이 실행한다. PostgreSQL 테스트는 `--network none`, tmpfs와 bounded PID/memory만 사용하고 Docker socket·운영 저장소·운영 credential을 연결하지 않는다. 테스트의 trust 인증은 이 격리된 일회성 DB에만 적용된다.

`build-all.sh --no-bump --build-only`는 기존 서비스와 함께 인프라 image `hololive-postgres:prod`·`iris-stack-valkey:9.1.3-zlib1.4-r0`·`iris-stack-nginx:1.31.8-zlib1.4-r0`를 빌드하고 실제 소스 revision label을 검증한다. 게시용 빌드는 깨끗한 reviewed revision, ARM64, provenance·SBOM 및 전체 검증이 필요하다. validation-only 빌드의 `working-tree`·`test` revision은 배포용 근거가 아니다.

중앙 primary와 비활성 standby 참고 unit(`deploy/quadlet/hololive-bot-standby/`)은 같은 `POSTGRES_IMAGE`를 쓴다. 인프라 image(PostgreSQL·Valkey·nginx) 교체는 [Quadlet README](../../../deploy/quadlet/README.md#이미지)의 load → build host identity 대조 → `compose.env` 갱신 → 승인된 restart 순서를 따른다. standby에서 빌드하거나 이전 공식 이미지로 자동 대체하지 않으며, 이미지 목록 갱신만으로 실행 중 인프라의 취약점이 해소됐다고 보고하지 않는다.

최종 이미지 검증에서는 scanner DB 시각과 coverage 한계를 보존하며 알려지지 않은 취약점까지 없다고 주장하지 않는다. upstream의 수정된 호환 공식 이미지가 제공되면 이 재빌드와 보안 패키지 고정값을 제거할 수 있는지 재검토한다.
