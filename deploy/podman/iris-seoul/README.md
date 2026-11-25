# iris-seoul Podman collector b

2026-11-06 `collector-b`와 PO issuer b를 iris-seoul(Ubuntu 26.04 arm64)의 Docker Compose `oracle` profile에서
rootful Podman Quadlet으로 옮겼다. 같은 호스트의 Redroid만 Docker에 남는다. 배포·롤백·완료 검사는 중앙 collector c와
같은 `scripts/deploy/po-central-cutover.sh`의 slot `b`가 소유한다.

## 설치

unit 정의는 배포 도구가 설치하지 않는다. 바뀐 정의는 이 디렉터리에서 설치하고 다음 배포 전에 daemon-reload한다.

```bash
sudo install -d -m 0755 /etc/containers/systemd/hololive-bot
sudo install -m 0644 deploy/podman/iris-seoul/*.network deploy/podman/iris-seoul/*.volume \
  deploy/podman/iris-seoul/*.container /etc/containers/systemd/hololive-bot/
sudo systemctl daemon-reload
```

## 호스트 전제

- Podman 런타임은 runc다(`/etc/containers/containers.conf.d/51-iris-runtime.conf`). Ubuntu 26.04의 crun AppArmor
  프로필은 `no-new-privileges` 컨테이너에서 `containers-default`와 겹쳐(`//&crun`) 컨테이너 안 프로세스 사이의 신호를
  거부한다. collector는 youtube.js helper를, issuer는 worker process group을 정지하지 못한다(2026-11-06 실측).
- netavark는 Docker의 `FORWARD` DROP과 공존하려고 iptables driver를 쓴다(`50-iris-firewall.conf`).
- 이미지: `localhost/hololive-collector:prod`·`localhost/hololive-youtube-po-sandbox:prod`(arm64). 실행 image는
  `hololive-collector-b.container.d/50-worker-profile.conf`의 `Image=sha256:<id>`가 고정한다.
- 비밀: stack-secrets `hosts/iris-seoul/hololive-bot/` → `/etc/stack-secrets/hololive-bot/`. compose 보간이 없어
  `collector.env`에 `POSTGRES_PASSWORD`를 둔다.
- 배포 루트: `/opt/hololive-bot/collector-b/{current,backups,po-current-b}`. `current/artifacts/worker-profile/`에
  release pair와 활성 포인터가 있고, `po-current-b/`는 실행 중 issuer의 검토 영수증이다.
- 쓰기 경로: `/var/lib/hololive-collectors/b/{data,logs}`(1000:1000). UserNS 없이 호스트 uid·gid로 실행한다.
- 네트워크: Podman `hololive-collector-b`(10.89.16.0/24)에서 중앙 PostgreSQL `100.100.1.8:5433`에 `verify-full` TLS로 직접 접속합니다.
  HBA가 역할 `hololive_collector_b`와 tailnet source `100.100.1.5/32`를 함께 제한합니다. metrics는 `100.100.1.5:30096`에 게시합니다.
- issuer는 collector session이 끝나면 generation을 바꾸려고 스스로 종료한다. collector는 issuer를 `Wants=`로만 끌어온다.

## 배포

kapu에서 실행한다. 원장(migration 222) 증명은 중앙 주 서버에서만 할 수 있으므로 같은 release의 c를 먼저 배포한다.

```bash
PO_B_SSH_TARGET=iris-seoul APPROVE_PO_B_DEPLOY=true scripts/deploy/po-central-cutover.sh b deploy
PO_B_SSH_TARGET=iris-seoul scripts/deploy/po-central-cutover.sh b check /opt/hololive-bot/collector-b/backups/po-b-<TIMESTAMP>
PO_B_SSH_TARGET=iris-seoul APPROVE_PO_B_ROLLBACK=true scripts/deploy/po-central-cutover.sh b rollback /opt/hololive-bot/collector-b/backups/po-b-<TIMESTAMP>
```

unit 교체 전에 `b quiesce`로 실행 중 이전 pair를 보존·정지하고, 설치된 후보와 중앙 재개가 끝난 뒤 `b resume`으로 재개합니다. 두 단계 모두 `<clean-candidate-tree> <trusted-receipt-dir> <b-staging-timestamp> <owner-window-id> <trusted-origin-pair-dir>` 다섯 인자를 받고, `b quiesce`는 여섯째 인자로 준비한 재개 grant 파일을 받아 정지 grant에서 단계만 `resume`으로 바꾼 줄을 담은 일반 파일인지 확인하고, 그 줄을 영수증 디렉터리의 `b.<owner-window-id>.resume-grant`에 남깁니다. backup 하나만 주는 `b resume <backup>`은 `staged-po-b-*` backup을 받지 않습니다. `b preflight`는 `b quiesce`와 같은 여섯 인자로 `b quiesce`가 grant를 소비하기 전까지의 검사만 하고 SSH를 열지 않으므로 정지 창을 열기 전에 실행합니다. `b quiesce`는 stage 완료 영수증의 seal만 요구하고, c 후보 수용 영수증(`c.accepted`)은 resume·check·rollback에서 확인합니다. stage archive의 `source-files.sha256`은 c와 같이 `po-central-files.txt`와 `manifest.txt`의 002 이후 migration·계약 SQL을 모두 담아야 하므로, c `quiesce-verified`에 쓴 pair와 같은 seal이 됩니다. 이 release의 `b quiesce`는 staging 후보 unit이 설치된 unit과 바이트 단위로 같을 때만 진행하고, 그 unit으로 정지 전 실제 경로(`--check-database-endpoint`)를 확인합니다. 정지된 collector에 일반 `deploy`를 적용하지 않습니다. 실패 시 hold와 backup을 보존합니다. endpoint가 다른 이전 pair로의 `rollback`은 거부되므로 호환 후보로 fix-forward합니다.

kapu의 영수증 디렉터리는 실행 사용자 소유 0700이며 심볼릭 링크 없는 절대 경로로 지정합니다. 이 디렉터리에는 b staging이 남긴 `arm64/` archive와 `b.stage.<b-staging-timestamp>.finished`(`<revision> <seal> <b-staging-timestamp>` 한 줄), 승인 주체가 쓴 정지 grant `b.current-grant`가 있어야 하며, 영수증 파일은 모두 실행 사용자 소유 0600 일반 파일입니다. 승인 주체는 grant를 게시하거나 회수할 때 영수증 디렉터리의 `receipts.lock`을 `flock -n`으로 잡고, `b quiesce`·`b resume`도 같은 잠금 안에서 grant를 다시 검증하고 소비합니다. grant는 하드 링크가 1개인 한 줄 `PO_B_PAIR_V1 iris-seoul <owner-window-id> quiesce <b-staging-timestamp> <tool-revision> <revision> <seal> <origin-revision> <origin-collector-id> <origin-issuer-id> <origin-digest> <expires>`입니다. 재개 grant는 `quiesce`만 `resume`으로 바꾼 같은 줄이므로 `expires`(Unix 초)가 정지부터 b 재개까지를 덮어야 합니다. `b quiesce`는 `expires`가 1시간 이상, `b preflight`는 1시간 30분 이상 남았을 때만 통과합니다.

`b/current`는 source checkout이 아니라 release pair 저장소이므로 `.source-revision`을 만들지 않습니다.
`quiesce`는 동일 full revision으로 빌드한 image/manifest와 bounded source checksum을 전송하고 보존합니다.
helper·migration·후보 collector unit도 checksum 대상입니다. `resume`은 helper를 실행하기 전에 이를 대조하고,
설치된 기본 collector unit이 staged 후보와 바이트 단위로 같은지 확인합니다. 다른 unit을 설치하거나
staging을 수정해 맞추지 말고 검토된 후보를 사용합니다.

`quiesce` 성공 뒤 출력된 staging 경로에서 **collector 기본 unit만** 아래처럼 수동 설치합니다.
network·volume·issuer unit과 worker-profile drop-in 소유권은 기존 절차대로 유지합니다.

```bash
# iris-seoul, 두 writer가 해당 backup의 persistent hold 아래에 있는 동안 실행
sudo install -m 0644 /opt/hololive-bot/collector-b/po-staging-<b-staging-timestamp>/deploy/podman/iris-seoul/hololive-collector-b.container \
  /etc/containers/systemd/hololive-bot/hololive-collector-b.container
sudo systemctl daemon-reload
# kapu, 같은 release의 중앙 재개와 c 수용 뒤 실행
PO_B_SSH_TARGET=iris-seoul APPROVE_PO_B_DEPLOY=true scripts/deploy/po-central-cutover.sh b resume \
  <clean-candidate-tree> <trusted-receipt-dir> <b-staging-timestamp> <owner-window-id> <trusted-origin-pair-dir>
```

이전 unit은 quiesce가 보존한 `current/artifacts/worker-profile/releases/staged-po-b-<b-staging-timestamp>/collector-b/previous/unit.container`에
남습니다. 재개 검증이 실패하면 mask를 해제하지 않으며, 재개 후 실패할 때도 두 writer를 다시 hold합니다.
롤백은 먼저 중앙 HBA와 설치 unit의 effective endpoint를 이전 경로로 정합화한 뒤 수행해야 합니다.
