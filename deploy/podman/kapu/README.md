# kapu Podman collector

kapu(RHEL 10, SELinux Enforcing)는 2026-11-04부터 slot d를 rootful Podman Quadlet으로 실행한다(loopback 30015).
같은 날 osaka2는 공개 진입점으로 바뀌어 collector를 두지 않고, b는 iris-seoul Quadlet(`deploy/podman/iris-seoul/`)이 맡는다.
osaka 호스트를 다시 설치하는 동안에도 a를 kapu에 임시로 띄우지 않는다.

## 기존 실행 pair의 수동 전환

`scripts/deploy/kapu-d-cutover.py`는 **kapu rootful d의 기존 실행 pair**만 전환한다. 실행 전에 승인한 **동일 revision**의 `scripts/deploy/kapu-d-cutover.py`, `scripts/deploy/lib/systemd-stop-guard.py`, `deploy/podman/kapu/`를 root 전용 배포 트리에 **이 상대 경로 그대로** 준비하고 provenance를 검증한다. 단독 스크립트 복사나 다른 revision의 가드 대체는 지원하지 않는다. 아래 명령은 서명된 운영 변경 범위와 창을 별도로 확인한 경우에만 사용한다. 첫 설치, 호스트 복구, 타 슬롯, 이미지 구축·전송, PgBouncer/HBA/DB 변경은 별도 작업이다. 자동 시작/재시작을 먼저 하지 않는다.

**최초 풀링 전환(5433→6433):** 옵션 없이 실행한다. 이미 6433이면 자동으로 다른 모드로 전환하지 않는다.

```bash
# quiesce가 root 전용 보관 디렉터리와 새 hold를 만든다. hold를 미리 만들지 않는다.
hold=/var/lib/hololive-collector-d-cutover/d-cutover-CHANGE-ID
sudo -n python3 -I scripts/deploy/kapu-d-cutover.py quiesce "$hold"
# 양쪽 정지·소유 mask 증거가 확인된 뒤, 중앙 경로가 준비되면 같은 hold에서 딱 한 번:
sudo -n python3 -I scripts/deploy/kapu-d-cutover.py resume "$hold" \
  "$FULL_REVISION" "$COLLECTOR_SHA256_IMAGE_ID" "$ISSUER_SHA256_IMAGE_ID" \
  "$COLLECTOR_CONTAINER_FILE_SHA256" "$ISSUER_CONTAINER_FILE_SHA256"
```

**카드 창(6433→6433):** 첫 창의 풀링 수용 후에만 사용한다. 첫 창의 hold 대신 새 hold를 만들고, **정지 전에** 카드 창의 승인된 산출물 목록에서 full revision, 두 이미지 ID와 두 unit SHA-256을 가져온다. 두 명령에 `--already-pooled`와 같은 후보 식별자를 전달한다.

```bash
hold="/var/lib/hololive-collector-d-cutover/d-cutover-card-${CHANGE_ID:?}"
candidate=("$FULL_REVISION" "$COLLECTOR_SHA256_IMAGE_ID" "$ISSUER_SHA256_IMAGE_ID"
           "$COLLECTOR_CONTAINER_FILE_SHA256" "$ISSUER_CONTAINER_FILE_SHA256")
sudo -n python3 -I scripts/deploy/kapu-d-cutover.py quiesce --already-pooled "$hold" "${candidate[@]}"
# 모든 writer 정지·복구 시점 확인과 승인된 카드 마이그레이션이 끝난 뒤:
sudo -n python3 -I scripts/deploy/kapu-d-cutover.py resume --already-pooled "$hold" "${candidate[@]}"
```

카드 창은 두 후보 이미지가 각각 정지할 실행 이미지와 다른 ID이며 다른 revision인지 확인한다. 이미지에 기록된 revision·역할·아키텍처와 unit 원본 digest를 검증한 뒤에만 이 후보와 현재 실행 세대·hold inode를 `previous.json`에 함께 보존한다. 카드 창의 두 unit은 풀링 수용 시와 같은 바이트여도 된다. `resume`은 다른 hold, 정지 전 후보가 없는 영수증, 후보 인수 중 하나라도 달라진 요청을 거부한다. 새 태그나 재빌드 이미지를 같은 revision이라는 이유로 대체할 수 없다.
revision은 40자리, image ID 두 개는 검증되어 rootful 저장소에 이미 있는 `sha256:<64 hex>`이고 마지막 두 값은 **해당 revision에서 승인한 두 `.container` 파일 원본의 SHA-256**(접두사 없음)이다. 실행 호스트 파일에서 기대 해시를 역산하지 않는다. helper 자체와 전달 파일의 provenance도 실행 전에 별도로 검증한다.
root만 수정할 수 있는 배포 트리에서 실행한다. 후보 적재 때 실제 `:prod` 두 개를 먼저 이동하면 이전 실행 증거를 잃으므로 helper는 거부한다. hold에는 비밀 env/key가 아닌 파일 식별 metadata·profile·unit·보존 태그 식별자만 남긴다.
영수증은 `/var/lib/hololive-collector-d-cutover`(`root`, `0700`)에 보관한다. 이 경로의 소유권·권한이 맞지 않으면 인수하거나 고치지 않고 중단한다. `resume`은 없는 경로를 만들지 않는다. 수집기 데이터·로그가 있는 `/var/lib/hololive-collectors`의 권한은 바꾸지 않는다.

- `quiesce`: 지금 d의 실제 `:prod`·실행 세대·선택한 원래 endpoint·사용자·읽기 전용 profile bind·인증된 loaded hash를 대조한다. 원본 unit·허용된 drop-in·비밀 없는 profile과 실이미지별 새 보존 태그를 남긴다. 두 unit 각각에 소유 inode를 기록한 `/dev/null` anchor를 만들고, **양쪽 모두** `ConditionPathExists=!<anchor>`가 manager에 반영된 뒤에만 정지한다. 이때 원래 `ExecStop`·`KillMode`가 그대로 로드돼 있어야 한다. issuer를 단일 manager job으로 먼저 정지해 `Requires=`에 의한 collector 동반 정지를 피한다. collector는 정지 직전에도 원래 실행 세대·endpoint·profile이어야 한다. 각 정지의 journal·종료 결과·두 PID 0·컨테이너 부재를 mask 설치 **전**에 증명한다. 두 결과를 모두 확인한 뒤에만 원 anchor의 hardlink로 최종 mask를 설치하고 모드가 명시된 `kapu-d-guarded-stop-v2` 영수증을 남긴다. 모드가 없는 v1·이전 실패 hold·타 슬롯 `pair.json`을 새 영수증으로 바꾸거나 다른 창에서 재사용하지 않는다.
  정상 종료에는 정지 요청 시각 이후 **그 CID 하나의 Podman `died` 이벤트와 exit 0**도 필요하다. manager가 비활성 unit을 수거해 `InvocationID`나 종료 필드를 비워도 이를 성공으로 추정하지 않는다. 예외인 issuer 143도 같은 CID의 exit 143과 아래 manager 증거가 모두 필요하다.
- `resume`: `previous.json`의 원래 endpoint, 동일 부팅의 두 소유 mask와 기존 컨테이너 부재를 확인한다. 최초 전환에서는 direct→pooled port 외의 차이를, 카드 창에서는 **어떤 기본 unit 변경도** 허용하지 않는다. 양쪽 모두 profile/source drift와 숨은 override를 거부한다. 후보 unit과 **두 `docker.io/library/…:prod`**를 검증하고, issuer·collector가 계속 막혀 있는 상태에서 candidate의 profile 검사와 d 소스의 실제 `--check-pooled-database`를 수행한다. envfile은 Podman에만 주며 SQL probe에는 공개 DB CA만 읽기 전용으로 붙인다.
- issuer와 collector를 순서대로 한 번씩 동기 시작한다. `Type=notify` 기동 중 TERM/HUP은 해당 요청이 끝나고 원래 start job의 InvocationID·CID·이미지·cgroup을 확보한 뒤 처리한다. issuer는 같은 세대의 healthy와 socket, collector는 같은 세대·profile/endpoint·`first_success`·양성 handoff·`NRestarts=0`이 필요하다. 운영 base unit의 `Restart=always`는 수정하지 않는다. hold 중 가드는 `[Service]`의 실효 `Restart=no`와 빈 `RestartForceExitStatus=`를 대조하며, 가드를 해제하면 base unit 정책이 다시 적용된다. 143은 해당 issuer의 이번 명시적 stop과 동일 invocation의 `failed/exit-code/1/143`일 때만 인정한다. 이전 실행 세대의 143은 후보 정지 증거가 아니며, **collector의 143은 job이 완료돼도 정상 종료가 아니다.**
- 후보 실패 시 실제로 연 unit만 가드로 다시 차단한다. 열지 않은 peer는 기존 소유 mask와 부재만 확인한다. 예외적으로 mask unlink 직후 아직 manager에 원래 mask가 로드된 채 reload가 실패했고, 시작을 요청하지 않았으며 진행 중 job·PID·컨테이너가 없고 원 inode가 확인되면 같은 mask 링크만 복원한다. 그 외에는 원래 후보 unit이 로드된 상태에서 가드를 먼저 적용한다. 실행 세대·정상 종료를 증명할 수 없으면 가드를 유지하고 미확인으로 남긴다. 자동으로 후보나 이전 이미지를 시작하지 않는다.
  start 클라이언트가 먼저 끝나 manager에 job이 남으면 가드 아래에서 해당 unit의 실효 `TimeoutStartUSec` 안에 같은 job만 관찰한다. 같은 실행 세대가 확인되면 정지한다. 부분 unlink 후 이전 CID의 부재나 정지 영수증을 다시 입증하지 못하면 최종 mask 대신 재부팅 후에도 남는 기동 차단을 설치한다. 동료가 미확인이라도 이미 확인한 후보는 정지하되, 새 hold나 성공 영수증을 발행하지 않는다.
- anchor만 있거나 가드 파일·조건·소유 inode가 불일치하는 경우, 가드만 있고 실행이 남은 경우, mask 설치·reload·가드 정리 중 끊긴 경우는 모두 **완료된 hold가 아니다**. `quiesced` 없는 정지나 `accepted.json` 없는 전환을 반복하지 않는다. 외부/잔여 가드·mask를 인수·삭제하거나 `unmask`, `reset-failed`, `restart`, `:prod` 역이동을 하지 않는다. 양쪽 소유 mask와 컨테이너 부재만으로 이전의 비정상 종료를 정상으로 바꿀 수 없다. 영수증·가드·태그·profile은 보존하고 수동 복구는 새 승인으로만 한다.

## 호스트 전제

- 이미지: rootful 저장소에 검증된 linux/amd64 collector와 PO issuer가 모두 있어야 한다. 두 Quadlet이 실제 읽는 위치는 `docker.io/library/hololive-youtube-collector:prod`와 `docker.io/library/hololive-youtube-po-sandbox:prod`다. 실행 pair는 `localhost/<컨테이너명>:runtime-before-<hold명>`에 보존한다. `localhost/…:prod` 이동만으로 교체되지 않는다.
- 비밀: stack-secrets `hosts/kapu/hololive-bot/collector-d/` → `/etc/stack-secrets/hololive-bot/collector-d/`.
  compose 보간이 없어 env 파일에 `POSTGRES_PASSWORD`를 둔다.
- 비밀이 아닌 release profile: `/etc/hololive-bot/collector-d/worker-profile-release/`(`SHA256SUMS` 검증).
- 쓰기 경로: `/var/lib/hololive-collectors/<slot>/{data,logs}`, SELinux `container_file_t`(semanage fcontext).
- 네트워크: Podman 빅뱅 전환으로 kapu에서 Docker를 걷어 내 netavark는 기본 nftables driver를 쓴다(Docker FORWARD DROP과
  공존하려던 `50-kapu-netavark-iptables.conf`는 제거). metrics는 게시하지 않고 Prometheus(`obs-prometheus`)가 고정 IP
  (`10.89.15.<slot>`)로 수집한다.
- DB: `100.100.1.8:6433`의 PgBouncer frontend HBA가 `hololive_collector_d`와 source `100.100.1.3/32`를 함께 제한합니다. PostgreSQL에는 pooler 주소가 보입니다. 최초 전환·direct 경로 복구는 [중앙 전환 절차](../../quadlet/README.md#최초-운영-전환)와 함께 수행하며, 이전 image만 새 HBA 위에서 시작하지 않습니다.
- HealthCmd는 distroless 이미지라 JSON 배열로 쓴다(문자열은 `/bin/sh -c`로 감싸져 실패한다).
