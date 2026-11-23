# kapu Podman collector

kapu(RHEL 10, SELinux Enforcing)는 2026-11-04부터 slot d를 rootful Podman Quadlet으로 실행한다(loopback 30015).
같은 날 osaka2는 공개 진입점으로 바뀌어 collector를 두지 않고, b는 iris-seoul Quadlet(`deploy/podman/iris-seoul/`)이 맡는다.
osaka 호스트를 다시 설치하는 동안에도 a를 kapu에 임시로 띄우지 않는다.

## 기존 실행 pair의 수동 전환

이 릴리스의 `scripts/deploy/kapu-d-cutover.py`는 이미 직접 TLS 5433으로 실행 중인 kapu rootful d를 같은 endpoint의 후보로 교체하는 전용 도구다. PgBouncer 6433 원점은 받지 않는다. [퇴역 전환](../../quadlet/README.md#pgbouncer-퇴역-전환)은 직전 릴리스의 도구로 한다. 아래 명령은 운영 변경 범위와 창을 별도로 승인한 경우에만 사용한다. 첫 설치, 호스트 복구, 타 슬롯, 이미지 구축·전송, PgBouncer/HBA/DB 변경은 별도 작업이다.

**카드 창 입력 장착:** 검증을 마친 카드 릴리스 산출물에서 봉인한 오프라인 d custody tar의 출처와 무결성을 확인하고, 새 root 전용 디렉터리에 푼다. 아래 예에서 tar는 다음 경로들을 루트 기준으로 포함한다. 파일은 root 소유 일반 파일이어야 하며 부모 경로까지 root 외의 쓰기 권한이나 심볼릭 링크가 없어야 한다.

- `.source-revision`: 검증된 카드 소스의 40자리 SHA 한 줄.
- `d-card-candidate.json`: `revision`은 같은 SHA, `images`와 `units`의 키는 각각 `hololive-collector-d`와 `hololive-youtube-po-d`. 값은 해당 컨테이너의 검증된 `sha256:<64 hex>` 이미지 ID와 `.container` 원본의 SHA-256(접두사 없음)이다.
- `scripts/deploy/kapu-d-cutover.py`, `scripts/deploy/lib/systemd-stop-guard.py`, `scripts/deploy/lib/collector-slots.tsv`, `deploy/podman/kapu/`의 두 `.container`: 위 승인과 같은 소스 트리의 상대 경로를 유지한다.

```bash
release="/root/d-card-${FULL_REVISION:?}"
sudo -n mkdir -m 0700 -- "$release" &&
  sudo -n tar --extract --file="${D_CUSTODY_TAR:?}" --directory="$release" \
    --no-same-owner --no-same-permissions --no-overwrite-dir
```

기존 디렉터리에 덮어쓰거나 현장 checkout·실행 이미지·호출 인수로 두 파일을 다시 만들지 않는다. 아래 다섯 인수도 승인된 카드 산출물 목록에서 가져온다. 두 명령 모두 같은 트리·같은 인수가 필요하다. hold는 미리 만들지 않는다.

```bash
hold="/var/lib/hololive-collector-d-cutover/d-cutover-card-${CHANGE_ID:?}"
candidate=("$FULL_REVISION" "$COLLECTOR_SHA256_IMAGE_ID" "$ISSUER_SHA256_IMAGE_ID"
           "$COLLECTOR_CONTAINER_FILE_SHA256" "$ISSUER_CONTAINER_FILE_SHA256")
sudo -n python3 -I -B "$release/scripts/deploy/kapu-d-cutover.py" quiesce "$hold" "${candidate[@]}"
# 정지 뒤 다른 슬롯 정지를 미뤘다가 이어 가기 전:
sudo -n python3 -I -B "$release/scripts/deploy/kapu-d-cutover.py" check "$hold" "${candidate[@]}"
# 모든 writer 정지·복구 시점 확인과 승인된 카드 마이그레이션이 끝난 뒤:
sudo -n python3 -I -B "$release/scripts/deploy/kapu-d-cutover.py" resume "$hold" "${candidate[@]}"
```

`quiesce`는 새 hold를 만들거나 서비스를 정지하기 전에 배포 트리의 두 승인 파일과 인수를 대조한다. 실행 중인 pair와 ID·revision이 다른 과거 릴리스라도 승인한 카드 후보가 아니면 거부한다. 실행 pair와 다른 두 후보 이미지의 revision·역할·아키텍처와 unit 원본 digest를 확인한 뒤에만 후보와 실행 세대·hold inode를 `previous.json`에 함께 보존한다. 카드 창의 두 unit은 첫 창과 같은 바이트여도 된다.
`resume`은 배포 트리의 두 파일을 다시 읽고, 요청한 후보가 이번 hold에서 정지 전에 보존한 후보와도 일치해야 진행한다. 같은 revision의 재빌드 이미지를 대체할 수 없다. 후보 적재 때 실제 `:prod` 두 개를 미리 이동하지 않는다. hold에는 비밀 env/key가 아닌 파일 식별 metadata·profile·unit·보존 태그 식별자만 남긴다.
영수증은 `/var/lib/hololive-collector-d-cutover`(`root`, `0700`)에 보관한다. 이 경로의 소유권·권한이 맞지 않으면 인수하거나 고치지 않고 중단한다. `resume`은 없는 경로를 만들지 않는다. 수집기 데이터·로그가 있는 `/var/lib/hololive-collectors`의 권한은 바꾸지 않는다.

- `quiesce`: 지금 d의 실제 `:prod`·실행 세대·원점 endpoint·사용자·읽기 전용 profile bind·인증된 loaded hash를 대조한다. 원본 unit·허용된 drop-in·비밀 없는 profile과 실이미지별 새 보존 태그를 남긴다. 두 unit 각각에 소유 inode를 기록한 `/dev/null` anchor를 만들고, **양쪽 모두** `ConditionPathExists=!<anchor>`가 manager에 반영된 뒤에만 정지한다. 이때 원래 `ExecStop`·`KillMode`가 그대로 로드돼 있어야 한다. issuer를 단일 manager job으로 먼저 정지해 `Requires=`에 의한 collector 동반 정지를 피한다. collector는 정지 직전에도 원래 실행 세대·endpoint·profile이어야 한다. 각 정지의 journal·종료 결과·두 PID 0·컨테이너 부재를 mask 설치 **전**에 증명한다. 두 결과를 모두 확인한 뒤에만 원 anchor의 hardlink로 최종 mask를 설치하고 이 hold의 스냅샷에 결부된 영수증(`kapu-d-guarded-stop-v5 already-direct`)을 남긴다. 첫 창·이전 실패 hold·타 슬롯 영수증을 재사용하지 않는다.
  정상 종료에는 정지 요청 시각 이후 **그 CID 하나의 Podman `died` 이벤트와 exit 0**도 필요하다. manager가 비활성 unit을 수거해 `InvocationID`나 종료 필드를 비워도 이를 성공으로 추정하지 않는다. 예외인 issuer 143도 같은 CID의 exit 143과 아래 manager 증거가 모두 필요하다.
- `resume`: `previous.json`의 원점 endpoint, 동일 부팅의 두 소유 mask와 기존 컨테이너 부재를 확인한다. 기본 unit 변경, profile/source drift, 숨은 override를 거부한다. 후보 unit과 **두 `docker.io/library/…:prod`**를 검증하고, issuer·collector가 계속 막혀 있는 상태에서 candidate의 profile 검사와 d 소스의 실제 `--check-database-endpoint`를 수행한다. `--check-worker-profile`은 실제 runtime 환경과 release profile·공개 CA를 사용하고 네트워크가 없다. `--check-database-endpoint`는 d의 실제 경로에서 별도로 실행하며 공개 CA만 붙인다. envfile은 두 검사 모두 Podman만 읽고, H3 개인키·공유 소켓·업무 데이터는 검사에 붙이지 않는다.
- issuer와 collector를 순서대로 한 번씩 동기 시작한다. `Type=notify` 기동 중 TERM/HUP은 해당 요청이 끝나고 원래 start job의 InvocationID·CID·이미지·cgroup을 확보한 뒤 처리한다. issuer는 같은 세대의 healthy와 socket, collector는 같은 세대·profile/endpoint·`first_success`·양성 handoff·`NRestarts=0`이 필요하다. 운영 base unit의 `Restart=always`는 수정하지 않는다. hold 중 가드는 `[Service]`의 실효 `Restart=no`와 빈 `RestartForceExitStatus=`를 대조하며, 가드를 해제하면 base unit 정책이 다시 적용된다. 143은 해당 issuer의 이번 명시적 stop과 동일 invocation의 `failed/exit-code/1/143`일 때만 인정한다. 이전 실행 세대의 143은 후보 정지 증거가 아니며, **collector의 143은 job이 완료돼도 정상 종료가 아니다.**
- 후보 실패 시 실제로 연 unit만 가드로 다시 차단한다. 열지 않은 peer는 기존 소유 mask와 부재만 확인한다. 예외적으로 mask unlink 직후 아직 manager에 원래 mask가 로드된 채 reload가 실패했고, 시작을 요청하지 않았으며 진행 중 job·PID·컨테이너가 없고 원 inode가 확인되면 같은 mask 링크만 복원한다. 그 외에는 원래 후보 unit이 로드된 상태에서 가드를 먼저 적용한다. 실행 세대·정상 종료를 증명할 수 없으면 가드를 유지하고 미확인으로 남긴다. 자동으로 후보나 이전 이미지를 시작하지 않는다.
  start 클라이언트가 먼저 끝나 manager에 job이 남으면 가드 아래에서 해당 unit의 실효 `TimeoutStartUSec` 안에 같은 job만 관찰한다. 같은 실행 세대가 확인되면 정지한다. 부분 unlink 후 이전 CID의 부재나 정지 영수증을 다시 입증하지 못하면 최종 mask 대신 재부팅 후에도 남는 기동 차단을 설치한다. 동료가 미확인이라도 이미 확인한 후보는 정지하되, 새 hold나 성공 영수증을 발행하지 않는다.
- `check`: `resume`이 후보를 열기 전에 하는 검사(같은 hold와 부팅, `quiesced`와 정지 영수증, 소유 mask와 정지 상태, 보존 unit·profile·태그, 후보 입력)를 그대로 실행하고, 아무것도 기록하거나 바꾸지 않는다. 거부하면 이 hold로 다른 슬롯 정지를 이어 가지 않는다.
- anchor만 있거나 가드 파일·조건·소유 inode가 불일치하는 경우, 가드만 있고 실행이 남은 경우, mask 설치·reload·가드 정리 중 끊긴 경우는 모두 **완료된 hold가 아니다**. `quiesced` 없는 정지나 `accepted.json` 없는 전환을 반복하지 않는다. 외부/잔여 가드·mask를 인수·삭제하거나 `unmask`, `reset-failed`, `restart`, `:prod` 역이동을 하지 않는다. 양쪽 소유 mask와 컨테이너 부재만으로 이전의 비정상 종료를 정상으로 바꿀 수 없다. 영수증·가드·태그·profile은 보존하고 수동 복구는 새 승인으로만 한다.

## 호스트 전제

- 이미지: rootful 저장소에 검증된 linux/amd64 collector와 PO issuer가 모두 있어야 한다. 두 Quadlet이 실제 읽는 위치는 `docker.io/library/hololive-collector:prod`와 `docker.io/library/hololive-youtube-po-sandbox:prod`다. 실행 pair는 `localhost/<컨테이너명>:runtime-before-<hold명>`에 보존한다. `localhost/…:prod` 이동만으로 교체되지 않는다.
- 비밀: stack-secrets `hosts/kapu/hololive-bot/collector-d/` → `/etc/stack-secrets/hololive-bot/collector-d/`.
  compose 보간이 없어 env 파일에 `POSTGRES_PASSWORD`를 둔다.
- 비밀이 아닌 release profile: `/etc/hololive-bot/collector-d/worker-profile-release/`(`SHA256SUMS` 검증).
- 쓰기 경로: `/var/lib/hololive-collectors/<slot>/{data,logs}`, SELinux `container_file_t`(semanage fcontext).
- 네트워크: Podman 빅뱅 전환으로 kapu에서 Docker를 걷어 내 netavark는 기본 nftables driver를 쓴다(Docker FORWARD DROP과
  공존하려던 `50-kapu-netavark-iptables.conf`는 제거). metrics는 게시하지 않고 Prometheus(`obs-prometheus`)가 고정 IP
  (`10.89.15.<slot>`)로 수집한다.
- DB: `100.100.1.8:5433`에 `verify-full` TLS로 직접 접속합니다. PostgreSQL HBA가 `hololive_collector_d`와 source `100.100.1.3/32`를 함께 제한합니다. PgBouncer에서의 전환은 [퇴역 전환 절차](../../quadlet/README.md#pgbouncer-퇴역-전환)를 따릅니다.
- HealthCmd는 distroless 이미지라 JSON 배열로 쓴다(문자열은 `/bin/sh -c`로 감싸져 실패한다).
