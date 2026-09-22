# 예외 없는 인프라 이미지 빌드

취약 패키지와 바이너리 자체를 수정한다. 2026-09-14의 비도달 예외 문서는 과거 근거이며 현재 allowlist가 아니다. 네 Trivy 예외 YAML과 예외 전용 checker를 제거했고 최종 scanner는 모든 심각도를 검사한다. `/dev/null` config·ignorefile과 `ignore-unfixed=false`를 사용하며 보고가 있으면 실패한다.

## 소유 경로

- nginx는 공식 `1.31.6-alpine-slim`의 multi-arch index `80149a0e5bc9fa0b8beaff5b8a453f71ba8ba038895d418381297ffa5cd57782`를 사용한다.
- socket-proxy는 공식 `1.13.1` index `3935b709275e4ec35d6ed5a5c4a1f0d01ed31eec5e7234efc3357ecd47689002`를 사용한다.
- `deploy/images/postgres`는 공식 PostgreSQL 18.6 index `77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873`를 기반으로 한다. gosu 1.19 source `6456aaa0f3c854d199d0f037f068eb97515b7513`를 Go 1.27.1·x/sys v0.48.0으로 재빌드해 `/usr/local/bin/gosu`만 교체한다. 기존 PostgreSQL entrypoint·PGDATA·사용자·initdb contract는 유지한다.
- `deploy/images/deunhealth`는 upstream source `37e5bd45036a29867fa71e68db3975c0b971c708`를 checksum으로 고정한다. `client.patch`는 Docker/Moby 의존성을 분리된 API v1.56.0·client v0.6.0으로 이행하고 새 client의 기본 API negotiation을 사용한다. daemon 코드와 취약한 x/net graph를 제거하며 OTel은 1.46.0으로 갱신한다. 이벤트/label/health 필터·재시작·취소·healthcheck는 보존한다.

`dependencies.mod`·`dependencies.sum`은 재빌드의 module graph 정본이다. upstream module zip은 Go proxy에서 받고 Dockerfile의 SHA-256으로 검증한다. Docker build에서는 `GOTOOLCHAIN=local`, compiler identity 검사, `-mod=readonly`로 추가 버전 선택을 막는다. 업스트림 라이선스는 최종 이미지에 포함한다.

## 빌드·검증·전달

모든 빌드와 검증은 kapu에서 수행한다. `scripts/ci/test-infra-images.sh`는 fake Docker API의 negotiation·filter·event·restart 실패·취소 race test와 임시 PostgreSQL root/UID 999 시작·SQL 검사를 소유하며 recurring security job이 실행한다. PostgreSQL 테스트는 `--network none`, tmpfs와 bounded PID/memory만 사용하고 Docker socket·운영 저장소·운영 credential을 연결하지 않는다. 테스트의 trust 인증은 이 격리된 일회성 DB에만 적용된다.

`build-all.sh --no-bump --build-only`는 기존 서비스와 함께 `hololive-postgres:prod`·`hololive-deunhealth:prod`를 빌드하고 실제 소스 revision label을 검증한다. 게시용 빌드는 깨끗한 reviewed revision, ARM64, provenance·SBOM 및 전체 검증이 필요하다. validation-only 빌드의 `working-tree`·`test` revision은 배포용 근거가 아니다.

중앙과 standby는 같은 소스 빌드 PostgreSQL 이미지를 소비한다. 인프라를 교체할 때는 두 인프라 이미지도 검증·전달한 후에만 기존 no-build cutover를 사용할 수 있다. standby에서 빌드하거나 이전 공식 이미지로 자동 대체하지 않는다. 부분 배포는 승인된 서비스만 `--no-build --no-deps`로 교체하며 이미지 목록 갱신만으로 실행 중 인프라의 취약점이 해소됐다고 보고하지 않는다.

최종 이미지 검증에서는 scanner DB 시각과 coverage 한계를 보존하며 알려지지 않은 취약점까지 없다고 주장하지 않는다. upstream의 수정된 호환 공식 이미지가 제공되면 이 재빌드·patch를 제거할 수 있는지 재검토한다.
