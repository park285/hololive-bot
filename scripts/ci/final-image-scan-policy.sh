# shellcheck shell=bash
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
