# shellcheck shell=bash
# 최종 이미지 Trivy 스캔의 억제 금지 정책. run-final-image-scan.sh가 이 배열을 그대로 쓰고,
# check-recurring-security-scan-contract.sh가 정책 배열을 검증한다.
# shellcheck disable=SC2034 # source하는 쪽에서 사용한다.
readonly FINAL_IMAGE_TRIVY_VERSION=0.75.0
# shellcheck disable=SC2034
readonly -a FINAL_IMAGE_TRIVY_ARGS=(
  image
  --config /dev/null
  --ignorefile /dev/null
  --ignore-unfixed=false
  --exit-code 1
  --no-progress
  --scanners vuln
  --severity "UNKNOWN,LOW,MEDIUM,HIGH,CRITICAL"
)
