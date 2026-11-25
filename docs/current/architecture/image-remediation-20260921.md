# 예외 없는 인프라 이미지 빌드

취약 패키지와 바이너리 자체를 수정한다. 2026-09-14의 비도달 예외 문서는 과거 근거이며 현재 allowlist가 아니다. 네 Trivy 예외 YAML과 예외 전용 checker를 제거했고 최종 scanner는 모든 심각도를 검사한다. `/dev/null` config·ignorefile과 `ignore-unfixed=false`를 사용한다. OS·비Go 패키지 보고는 차단하고, Go 모듈 보고는 아래 실제 바이너리의 패키지 존재 판정을 추가한다.

## Go 모듈 보고와 실제 패키지 판정

Trivy의 Go 모듈 버전 보고만으로는 같은 모듈 안의 미포함 패키지를 구분할 수 없다. Go 릴리스 빌드는 `-s`를 사용하지 않아 심볼을 보존한다. `-w`로 DWARF만 제거할 수 있지만 `govulncheck -mode=extract`에 실제 패키지·심볼 목록이 없으면 검사를 실패시킨다. govulncheck의 stripped-binary module fallback을 안전 근거로 사용하지 않는다.

scanner는 로컬 태그를 불변 image ID로 고정하고, Trivy가 보고한 바이너리를 실행하지 않고 추출한다. 고정 govulncheck v1.8.0의 `-mode=binary -scan=package -format=openvex`로 각 바이너리의 증거를 생성한다. 모든 Go 보고의 advisory ID 또는 공식 alias와 정확한 module/version PURL이 `not_affected / vulnerable_code_not_present`에 대응해야만 모듈 단위 과잉 판정으로 분류한다. 패키지가 포함됐으나 호출되지 않는다는 `vulnerable_code_not_in_execute_path`는 허용하지 않는다. 모르는 advisory, 누락된 증거, 잘못된 아키텍처, 실제 취약 패키지와 분석 오류는 차단한다.

수동 CVE allowlist나 저장된 VEX 입력을 받지 않는다. 원시 Trivy finding을 삭제하거나 심각도를 제외하지 않으며, 매 실행마다 새 evidence 디렉터리에 image/target identity, 바이너리 SHA-256, 추출 심볼, 생성된 VEX와 원본 보고를 보존한다. 출력의 `Go package-absence verified`는 해당 패키지가 실제 artifact에 없다는 뜻이며 원본 Trivy finding 수가 0이라는 뜻은 아니다.

## 인프라 image

PostgreSQL·Valkey·nginx는 2026-11-25부터 재빌드하지 않고 고정 digest의 공식 image를 그대로 쓴다([compose README](../../../deploy/compose/README.md#infra-images)). 세 image는 최종 이미지 스캔 manifest에서 빠졌고, 공식 image에 남은 Alpine 패키지와 PostgreSQL gosu의 Go 표준 라이브러리 보고는 upstream의 새 digest로 옮겨 해소한다. 이 문서 작성 당시 쓰던 socket-proxy 공식 image와 `deploy/images/deunhealth` 재빌드 image는 2026-11-04 중앙 런타임의 Podman Quadlet 전환(#735)에서 제거했다.

## 검증 한계

인프라 image(PostgreSQL·Valkey·nginx) 교체는 [Quadlet README](../../../deploy/quadlet/README.md#이미지)의 load → build host identity 대조 → `compose.env` 갱신 → 승인된 restart 순서를 따른다. 이미지 목록 갱신만으로 실행 중 인프라의 취약점이 해소됐다고 보고하지 않는다.

최종 이미지 검증에서는 scanner DB 시각과 coverage 한계를 보존하며 알려지지 않은 취약점까지 없다고 주장하지 않는다.
