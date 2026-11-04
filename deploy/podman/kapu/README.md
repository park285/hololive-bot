# kapu Podman collector

2026-11-04 `youtube-collector-b`를 iris-seoul compose에서 kapu(RHEL 10, SELinux Enforcing)의 rootful Podman quadlet으로 옮겼다.
osaka1·osaka2를 다시 설치하는 동안에는 같은 방식으로 a·d를 kapu에 임시로 띄운다.

## 설치

```bash
sudo install -m 0644 deploy/podman/kapu/*.network deploy/podman/kapu/*.volume deploy/podman/kapu/*.container /etc/containers/systemd/
sudo systemctl daemon-reload
sudo systemctl start hololive-youtube-po-b.service hololive-youtube-collector-b.service
```

## 호스트 전제

- 이미지: `hololive-youtube-collector:kapu-<VERSION>`(Dockerfile, linux/amd64)과 `hololive-youtube-po-sandbox:prod`
  (`scripts/build/build-po-sandbox-artifact.sh amd64 ...`)를 rootful Podman에 load한다. 원격 build는 하지 않는다.
- 비밀: stack-secrets `hosts/kapu/hololive-bot/collector-b/` → `/etc/stack-secrets/hololive-bot/collector-b/`.
  compose 보간이 없어 env 파일에 `POSTGRES_PASSWORD`를 둔다.
- 비밀이 아닌 release profile: `/etc/hololive-bot/collector-b/worker-profile-release/`(`SHA256SUMS` 검증).
- 쓰기 경로: `/var/lib/hololive-collectors/<slot>/{data,logs}`, SELinux `container_file_t`(semanage fcontext).
- 네트워크: kapu Docker는 iptables `FORWARD` 기본 정책을 DROP으로 둔다. netavark firewalld 드라이버로는 외부 연결이 막혀
  `/etc/containers/containers.conf.d/50-kapu-netavark-iptables.conf`에서 `firewall_driver = "iptables"`를 쓴다.
  게시 포트의 신규 연결은 여전히 Docker DROP에 막히므로 metrics는 게시하지 않고 Prometheus가 고정 IP(`10.89.15.<slot>`)로 수집한다.
- DB: 중앙 `pg_hba.conf`의 `hololive_scraper 100.100.1.3/32`.
- HealthCmd는 distroless 이미지라 JSON 배열로 쓴다(문자열은 `/bin/sh -c`로 감싸져 실패한다).

## 남은 한계

- 컨테이너에서 `100.100.1.3:4317`(kapu Jaeger OTLP)로 trace를 보내지 못한다. observability OTLP 출처 허용 범위 밖이다.
