# shellcheck shell=bash
# 최종 이미지 Trivy 스캔의 판본과 억제 금지 정책. run-final-image-scan.sh가 판본을 확인하고 배열을 그대로 쓰며,
# security.yml이 이 판본과 linux/amd64 배포본 SHA256으로 Trivy를 설치하고,
# check-recurring-security-scan-contract.sh가 정책 배열을 검증한다.
# shellcheck disable=SC2034 # source하는 쪽에서 사용한다.
readonly FINAL_IMAGE_TRIVY_VERSION=0.76.2 \
  FINAL_IMAGE_TRIVY_LINUX_AMD64_SHA256=bc0fc726e017b8927a62b2d3531d7a84ee01769daf5c99ed866dced54089fdbf
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
