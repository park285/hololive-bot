# kapu Podman collector

2026-11-04 `youtube-collector-b`를 iris-seoul compose에서 kapu(RHEL 10, SELinux Enforcing)의 rootful Podman quadlet으로 옮겼다.
같은 날 osaka2를 공개 진입점으로 바꾸면서 kapu는 slot d를 맡고 b는 iris-seoul compose로 되돌렸다(slot 글자만 바꿔 Seoul
AP 경로의 b 고정을 그대로 쓴다). osaka 호스트를 다시 설치하는 동안에도 a·d를 kapu에 임시로 띄우지 않는다.

## 설치

```bash
sudo install -m 0644 deploy/podman/kapu/*.network deploy/podman/kapu/*.volume deploy/podman/kapu/*.container /etc/containers/systemd/
sudo systemctl daemon-reload
sudo systemctl start hololive-youtube-po-d.service hololive-youtube-collector-d.service
```

## 호스트 전제

- 이미지 tag 규칙: `<VERSION>`은 루트 `VERSION` 값이라, 미출시 변경을 배포할 때도 같은 tag를 다시 씁니다. 그때는 실행 중이던
  이미지에 먼저 `localhost/hololive-youtube-collector-d:runtime-before-<날짜>`(PO는 `localhost/hololive-youtube-po-d:…`) tag를
  붙여 rollback 지점으로 남긴 뒤 tag를 옮깁니다. 실제 코드 판별은 image revision label로 합니다.
- 이미지: `hololive-youtube-collector:kapu-<VERSION>`(Dockerfile, linux/amd64)과 `hololive-youtube-po-sandbox:prod`
  (`scripts/build/build-po-sandbox-artifact.sh amd64 ...`)를 rootful Podman에 load한다. 원격 build는 하지 않는다.
- 비밀: stack-secrets `hosts/kapu/hololive-bot/collector-d/` → `/etc/stack-secrets/hololive-bot/collector-d/`.
  compose 보간이 없어 env 파일에 `POSTGRES_PASSWORD`를 둔다.
- 비밀이 아닌 release profile: `/etc/hololive-bot/collector-d/worker-profile-release/`(`SHA256SUMS` 검증).
- 쓰기 경로: `/var/lib/hololive-collectors/<slot>/{data,logs}`, SELinux `container_file_t`(semanage fcontext).
- 네트워크: Podman 빅뱅 전환으로 kapu에서 Docker를 걷어 내 netavark는 기본 nftables driver를 쓴다(Docker FORWARD DROP과
  공존하려던 `50-kapu-netavark-iptables.conf`는 제거). metrics는 게시하지 않고 Prometheus(`obs-prometheus`)가 고정 IP
  (`10.89.15.<slot>`)로 수집한다.
- DB: 중앙 `pg_hba.conf`의 `hololive_scraper 100.100.1.3/32`.
- HealthCmd는 distroless 이미지라 JSON 배열로 쓴다(문자열은 `/bin/sh -c`로 감싸져 실패한다).
