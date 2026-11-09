# iris-seoul Podman collector b

2026-11-06 `youtube-collector-b`와 PO issuer b를 iris-seoul(Ubuntu 26.04 arm64)의 Docker Compose `oracle` profile에서
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
- 이미지: `localhost/hololive-youtube-collector:prod`·`localhost/hololive-youtube-po-sandbox:prod`(arm64). 실행 image는
  `hololive-youtube-collector-b.container.d/50-worker-profile.conf`의 `Image=sha256:<id>`가 고정한다.
- 비밀: stack-secrets `hosts/iris-seoul/hololive-bot/` → `/etc/stack-secrets/hololive-bot/`. compose 보간이 없어
  `youtube-collector.env`에 `POSTGRES_PASSWORD`를 둔다.
- 배포 루트: `/opt/hololive-bot/collector-b/{current,backups,po-current-b}`. `current/artifacts/worker-profile/`에
  release pair와 활성 포인터가 있고, `po-current-b/`는 실행 중 issuer의 검토 영수증이다.
- 쓰기 경로: `/var/lib/hololive-collectors/b/{data,logs}`(1000:1000). UserNS 없이 호스트 uid·gid로 실행한다.
- 네트워크: `hololive-collector-b`(10.89.16.0/24). 중앙 PostgreSQL은 tailnet 출처(`100.100.1.5`)로 접속하므로
  컨테이너 대역은 `pg_hba`와 무관하다. metrics는 `100.100.1.5:30096`에 게시한다.
- issuer는 collector session이 끝나면 generation을 바꾸려고 스스로 종료한다. collector는 issuer를 `Wants=`로만 끌어온다.

## 배포

kapu에서 실행한다. 원장(migration 222) 증명은 중앙 주 서버에서만 할 수 있으므로 같은 release의 c를 먼저 배포한다.

```bash
PO_B_SSH_TARGET=iris-seoul APPROVE_PO_B_DEPLOY=true scripts/deploy/po-central-cutover.sh b deploy
PO_B_SSH_TARGET=iris-seoul scripts/deploy/po-central-cutover.sh b check /opt/hololive-bot/collector-b/backups/po-b-<TIMESTAMP>
PO_B_SSH_TARGET=iris-seoul APPROVE_PO_B_ROLLBACK=true scripts/deploy/po-central-cutover.sh b rollback /opt/hololive-bot/collector-b/backups/po-b-<TIMESTAMP>
```

## 2026-11-06 전환 기록

1. Docker의 `:prod` 두 image를 `docker save | podman load`로 옮겼다. image ID가 같다(collector `dc2f1776…`, issuer `785dc3e7…`).
2. issuer 영수증 `~/hololive-bot/backups/po-sandbox-current-b.{json,image-id}`를 `po-current-b/`에 설치하고
   `po_verify_image`로 대조했다.
3. Docker 컨테이너 둘을 `docker stop`하고(삭제하지 않음) unit을 기동했다. 수집 중단은 약 25초였다.
4. 실행 중 container 기준으로 `worker-profile-pair.py prepare youtube-collector-b …/releases/podman-cutover-20261106T1148Z/youtube-collector-b/previous`로
   첫 drop-in을 만들고 재시작해 `verify-live`를 확인했다. profile hash는 Docker 시절과 같다.

같은 날 사용자 승인으로 Docker 쪽 b 컨테이너·볼륨·이미지·compose 네트워크와 compose 파일, `~/hololive-bot` 배포 트리를 지웠다.
Docker 전환 이전 상태로의 롤백 경로는 더 없다. Docker에는 Redroid와 그 이미지만 남는다.
