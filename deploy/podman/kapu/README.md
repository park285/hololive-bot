# kapu Podman collector

kapu(RHEL 10, SELinux Enforcing)는 2026-11-04부터 slot d를 rootful Podman Quadlet으로 실행한다(loopback 30015).
같은 날 osaka2는 공개 진입점으로 바뀌어 collector를 두지 않고, b는 iris-seoul Quadlet(`deploy/podman/iris-seoul/`)이 맡는다.
osaka 호스트를 다시 설치하는 동안에도 a를 kapu에 임시로 띄우지 않는다.

## 기존 direct 설치의 수동 전환

`scripts/deploy/kapu-d-cutover.py`는 **kapu rootful d의 기존 실행 pair**만 전환한다. 운영자가 서명된 변경 범위와 창을 확인하고, 검토한 release의 helper와 `deploy/podman/kapu/`를 함께 사용한다. 첫 설치, 호스트 복구, 타 슬롯, 이미지 구축·전송, PgBouncer/HBA/DB 변경은 별도 작업이다. 자동 시작/재시작을 먼저 하지 않는다.

```bash
# 매 작업마다 새 이름. 이 경로는 생성하지 않는다.
hold=/var/lib/hololive-collectors/d-cutover-CHANGE-ID
sudo -n python3 -I scripts/deploy/kapu-d-cutover.py quiesce "$hold"
# 양쪽 정지·소유 mask 증거가 확인된 뒤, 중앙 경로가 준비되면 같은 hold에서 딱 한 번:
sudo -n python3 -I scripts/deploy/kapu-d-cutover.py resume "$hold" \
  "$FULL_REVISION" "$COLLECTOR_SHA256_IMAGE_ID" "$ISSUER_SHA256_IMAGE_ID" \
  "$COLLECTOR_CONTAINER_FILE_SHA256" "$ISSUER_CONTAINER_FILE_SHA256"
```

revision은 40자리, image ID 두 개는 검증되어 rootful 저장소에 이미 있는 `sha256:<64 hex>`이고 마지막 두 값은 **해당 revision에서 승인한 두 `.container` 파일 원본의 SHA-256**(접두사 없음)이다. 실행 호스트 파일에서 기대 해시를 역산하지 않는다. helper 자체와 전달 파일의 provenance도 실행 전에 별도로 검증한다.
root만 수정할 수 있는 배포 트리에서 실행한다. 후보 적재 때 실제 `:prod` 두 개를 먼저 이동하면 이전 실행 증거를 잃으므로 helper는 거부한다. hold에는 비밀 env/key가 아닌 파일 식별 metadata·profile·unit·보존 태그 식별자만 남긴다.

- `quiesce`: 지금 d의 실제 `:prod`·실행 세대·direct endpoint·사용자·읽기 전용 profile bind·인증된 loaded hash를 대조한다. 원본 unit·허용된 drop-in·비밀 없는 profile과 실이미지별 새 보존 태그를 남긴다. 예전 incident 영수증이나 release에 남은 `pair.json`의 타 슬롯 image metadata는 d 증거가 아니다.
- `resume`: 동일 부팅의 두 소유 mask와 기존 컨테이너 부재를 확인한다. direct→pooled port 외에 검토되지 않은 설치 차이, profile/source drift, 숨은 override는 거부한다. 후보 unit과 **두 `docker.io/library/…:prod`**를 검증하고, issuer·collector가 계속 막혀 있는 상태에서 candidate의 profile 검사와 d 소스의 실제 `--check-pooled-database`를 수행한다. envfile은 Podman에만 주며 SQL probe에는 공개 DB CA만 읽기 전용으로 붙인다.
- issuer 단독 시작 후 같은 컨테이너·InvocationID의 healthy와 socket, 그다음 collector 단독 시작 후 같은 세대·profile/endpoint·`first_success`·양성 handoff·`NRestarts=0`이 필요하다. 기존 `Restart=always` 정책을 변경하지 않는다. 143은 해당 issuer를 명시적으로 정지시킨 동일 invocation의 종료 증거일 때만 인정한다.
- 실패나 중단은 성공이 아니다. `accepted.json` 없이 재개 명령을 반복하거나 `unmask`, `reset-failed`, `restart`, `:prod` 역이동을 하지 않는다. **두 mask와 컨테이너 부재를 새로 확인하기 전에는 정지 완료로 간주하지 않는다.** 영수증·태그·profile은 보존하고 수동 복구는 새 승인으로만 한다.

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
