# shellcheck shell=bash
# 저장소 전환의 배타 잠금과 배포 writer의 공유 잠금은 원격 helper가 소유한다.
native_storage_remote_bash() {
  local command
  printf -v command '%q ' sudo -n /usr/local/libexec/iris-storage writer \
    --config /etc/iris-storage/config.json -- /usr/bin/bash -s -- "$@"
  "${AP_SSH[@]}" "$command"
}

# rsync의 server 프로세스도 전송이 끝날 때까지 같은 writer 잠금을 유지한다.
# shellcheck disable=SC2034 # source한 배포 스크립트가 rsync --rsync-path로 읽습니다.
NATIVE_STORAGE_RSYNC='sudo -n /usr/local/libexec/iris-storage writer --config /etc/iris-storage/config.json -- /usr/bin/rsync'
