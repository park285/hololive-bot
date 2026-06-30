# Host Migration: root → kapu (Full Migration + NOPASSWD sudo)

**Goal:** 베어 메탈 호스트의 주 작업 계정을 `root`에서 `kapu`로 전환하고, `kapu`에 NOPASSWD `sudo`를 부여한다. prod 서비스(Docker Compose 5개, systemd 유닛 12+개)는 새로운 `/home/kapu/` 경로로 이전한다.

**Architecture:**
- LVM root 단일 볼륨 (1.4TB)이라 `/root` → `/home/kapu` 이동은 같은 파일시스템 내 `mv` (인스턴트, 추가 디스크 불필요).
- compose 서비스의 실행 사용자는 `root` 유지 (Docker daemon이 root이므로 분리 이득 적음, 변경 최소화). systemd 유닛은 **경로만** 갱신.
- 다운타임 허용 → 단일 정비창에서 stop → 이동 → systemd 갱신 → restart 일괄 수행.
- 런타임 캐시(rustup/cargo/go/npm/nvm/gradle/m2)는 chown 방식으로 이전 (재설치 불필요). 거대 캐시(`.codex`, `.cache`)는 chown만, 백업/임시 디렉토리는 사용자 확인 후 삭제.

**Tech stack:** systemd, Docker Compose v2, LVM (단일 VG `kapu-vg`), zsh, OpenBao Agent.

**Execution:** `TaskCreate`/`TaskUpdate`로 단계별 진행. 각 Phase 종료 시점에 헬스체크 + 사용자 컨펌. Phase 2~5는 단일 정비창 내 연속 수행.

---

## Pre-flight: 결정 사항 (Phase 0 이전 사용자 확인 필요)

| 항목 | 크기 | 권장 | 결정 |
|---|---|---|---|
| `/root/restore-staging` | 101G | **삭제** (LVM 마이그레이션 잔여) | 미정 |
| `/root/restore` | 5G | 삭제 (재시동 후 1주 이상 안정 시) | 미정 |
| `/root/restore-safety-20260521-003903`, `/root/restore-metadata` | 1.4M | `/home/kapu/migration-archive/`로 보존 | 미정 |
| `/root/efi-pve-kernel-backup-20260508`, `/root/efi-loader-entries-backup-20260508` | 902M+52K | 보존 (부팅 복구용) | 미정 |
| `/root/debian-migrate-backup` | 9M | `/home/kapu/migration-archive/`로 보존 | 미정 |
| `/root/.codex` | 26G | chown 이전 (Codex 사용) | 미정 |
| `/root/.cache` | 21G | 이전 안 함 (재생성) | 미정 |
| `/root/go` | 4.1G | chown 이전 (Go module cache) | 미정 |
| `/root/.rustup`, `/root/.cargo` | 5G+1.4G | chown 이전 | 미정 |
| `/root/.gradle`, `/root/.m2`, `/root/.npm`, `/root/.nvm` | 4.0G | chown 이전 | 미정 |
| compose 서비스 실행 사용자 | - | `root` 유지 (변경 최소화) | 미정 |
| `code-server-root.service` | - | `code-server-kapu.service`로 신규 작성 (이름까지 변경) | 미정 |
| `/root/github-runner` | - | 손대지 않음 (별도 시스템 사용자) | 확정 |
| `kapu` docker 그룹 추가 | - | 필요 (`docker ps` 등 호환) | 확정 |

---

## Success criteria

- [ ] `sudo -u kapu sudo -n true` → exit 0 (NOPASSWD 적용)
- [ ] `id kapu` 그룹에 `docker` 포함
- [ ] `ls /root/work` → "No such file or directory" 또는 symlink → `/home/kapu/work`
- [ ] `docker ps` → 정비창 이전과 동일한 컨테이너 15개, 모두 `healthy`/`Up`
- [ ] `systemctl is-active hololive-compose chatbotgo-compose chatbot-infra-compose cliproxy-compose openbao-compose ima2-go rclone-google-drive` → 전부 `active`
- [ ] `systemctl list-timers hololive-*` → `hololive-main-log-mirror@osaka.timer`, `hololive-daily-log-rollup.timer` 다음 트리거 예약됨
- [ ] kapu로 SSH 접속 가능, zsh + oh-my-zsh + p10k 정상 로드
- [ ] `sudo -u kapu claude` 실행 시 `/home/kapu/.claude` 인식, 메모리/세션 유지
- [ ] hololive prod 헬스체크 통과: `./scripts/smoke/smoke-runtime-health.sh`, `curl -fsS http://127.0.0.1:30190/health` (admin-dashboard)
- [ ] 8시간 후 재확인: `docker ps` 동일 상태, `journalctl -u hololive-* --since=8h` 신규 에러 없음

---

## File map

### 신규 생성

- `/etc/sudoers.d/kapu-nopasswd` — `kapu ALL=(ALL) NOPASSWD:ALL`
- `/home/kapu/work/` ← `/root/work/` 이동 결과
- `/home/kapu/.{zshrc,zshenv,gitconfig,oh-my-zsh,p10k.zsh,tmux.conf}` ← `/root/`에서 복사 후 kapu 소유
- `/home/kapu/.{claude,claude.json,claude-code-router,codex,config,local,ssh}` ← `/root/`에서 이동 후 kapu 소유
- `/home/kapu/.{cargo,rustup,gradle,m2,npm,nvm,go,java-env.sh,android,dotnet,hermes}` ← chown 이전
- `/home/kapu/migration-archive/` — 보존 백업 (restore-metadata, restore-safety, debian-migrate-backup)
- `/usr/local/sbin/hololive-compose-up` — `cd /root/work/...` → `cd /home/kapu/work/...` 갱신 (기존 파일 in-place edit)
- `/usr/local/sbin/hololive-compose-down` — 신규 (root-owned 0755). `hololive-compose.service` ExecStop이 mutable home 스크립트를 직접 실행하던 것을 immutable wrapper로 옮긴 대응물. 기존 ExecStop과 동치 명령을 수행한다: `COMPOSE_ENV_FILE=/run/hololive-bot/compose.env <repo>/scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml down` (live-compat overlay 분리 후 표준 prod down).
- `/etc/systemd/system/code-server-kapu.service` — 신규 (User=kapu, HOME=/home/kapu)

### 수정 (in-place edit, `/root/` → `/home/kapu/`)

- `/etc/systemd/system/hololive-compose.service:9` — WorkingDirectory
- `/etc/systemd/system/chatbot-infra-compose.service:9` — WorkingDirectory
- `/etc/systemd/system/chatbotgo-compose.service:8` — WorkingDirectory
- `/etc/systemd/system/cliproxy-compose.service:9` — WorkingDirectory
- `/etc/systemd/system/hololive-main-log-mirror@.service:8,10,11,12` — WorkingDirectory + Environment + ExecStart
- `/etc/systemd/system/hololive-daily-log-rollup.service:6,7,8` — WorkingDirectory + Environment + ExecStart
- `/etc/systemd/system/ima2-go.service:8,9,10` — WorkingDirectory + EnvironmentFile + ExecStart
- `/etc/systemd/system/rclone-google-drive.service:13` — `--config /root/.config/rclone/...` → `/home/kapu/.config/rclone/...`

### 제거 (사용자 확인 후)

- `/root/restore-staging/` (101G)
- `/root/restore/` (5G, 1주 안정화 후)
- `/root/.cache/` (21G)
- `/etc/systemd/system/code-server-root.service` (`code-server-kapu.service`로 대체)
- `/etc/systemd/system/rclone-google-drive.service.bak-*` (백업 파일)

### 보존 (변경 없음)

- `/root/.bashrc`, `/root/.zshrc` 등 root 최소 shell 환경 (root 로그인 시 사용)
- `/root/.ssh/authorized_keys` (root SSH 유지)
- `/opt/actions-runner/` (별도 사용자)
- `/opt/secrets-stack/openbao/` (이미 `/opt` 하위, 변경 없음)
- `/etc/openbao-agent/*.hcl`, `openbao-agent-*.service`, `openbao-unseal.service`, `openbao-compose.service` (root 동작, /root 미참조)

---

## Tasks

### Task 0: Pre-flight 결정 컨펌 + 백업

**Files:** 없음 (read-only + 백업)

- [ ] **Step 0.1: 사용자에게 Pre-flight 결정 사항 확정 받기**

상단 표의 미정 항목 모두 결정 후 Phase 진행. 특히 `restore-staging` 삭제 여부와 `code-server` 이름 변경 여부 확정.

- [ ] **Step 0.2: 시스템 상태 스냅샷 저장**

```bash
mkdir -p /home/kapu/migration-archive/snapshot-pre
docker ps --format '{{.Names}}\t{{.Status}}\t{{.Image}}' > /home/kapu/migration-archive/snapshot-pre/docker-ps.txt
docker compose ls > /home/kapu/migration-archive/snapshot-pre/compose-ls.txt
systemctl list-units --type=service --state=running > /home/kapu/migration-archive/snapshot-pre/systemd-running.txt
systemctl list-timers --all > /home/kapu/migration-archive/snapshot-pre/systemd-timers.txt
ls -la /root > /home/kapu/migration-archive/snapshot-pre/root-listing.txt
du -sh /root/* /root/.* 2>/dev/null > /home/kapu/migration-archive/snapshot-pre/root-sizes.txt
cp -a /etc/systemd/system /home/kapu/migration-archive/snapshot-pre/systemd-system-backup
```

- [ ] **Step 0.3: 헬스체크 베이스라인 기록**

```bash
for port in 30001 30003 30006 30007 30190; do
  echo "=== :$port ==="
  curl -fsS -m 3 "http://127.0.0.1:$port/healthz" 2>&1 || curl -fsS -m 3 "http://127.0.0.1:$port/" 2>&1 | head -c 200
  echo
done | tee /home/kapu/migration-archive/snapshot-pre/healthchecks.txt
```

**Validation:** `ls /home/kapu/migration-archive/snapshot-pre/` → 6개 파일 존재.

---

### Task 1: NOPASSWD sudo + docker 그룹 (Phase 0, 다운타임 없음)

**Files:**
- Create: `/etc/sudoers.d/kapu-nopasswd`
- Modify: `/etc/group` (docker 그룹에 kapu 추가, `usermod`로)

- [ ] **Step 1.1: sudoers 파일 작성**

```bash
echo 'kapu ALL=(ALL) NOPASSWD:ALL' | sudo tee /etc/sudoers.d/kapu-nopasswd
sudo chmod 0440 /etc/sudoers.d/kapu-nopasswd
sudo visudo -cf /etc/sudoers.d/kapu-nopasswd   # 문법 검증
```

**Expected:** `parsed OK`

- [ ] **Step 1.2: kapu를 docker 그룹에 추가**

```bash
sudo usermod -aG docker kapu
getent group docker   # kapu가 멤버에 포함되었는지 확인
```

**Expected:** `docker:x:107:github-runner,kapu`

- [ ] **Step 1.3: 검증**

```bash
sudo -u kapu sudo -n true && echo "NOPASSWD OK"
sudo -u kapu groups | tr ' ' '\n' | grep -E '^(sudo|docker)$'
```

**Expected:** `NOPASSWD OK`, `sudo` + `docker` 둘 다 출력.

---

### Task 2: kapu 셸 환경 준비 (Phase 1, 다운타임 없음)

**Files:** `/home/kapu/.{zshrc,zshenv,oh-my-zsh,p10k.zsh,tmux.conf,gitconfig,profile,zshrc.d}`

- [ ] **Step 2.1: zsh 환경 복사**

```bash
# 정적 복사 (root용 백업 파일/임시 파일은 제외)
sudo cp -a /root/.oh-my-zsh /home/kapu/.oh-my-zsh
sudo cp /root/.zshrc /home/kapu/.zshrc
sudo cp /root/.zshenv /home/kapu/.zshenv
sudo cp /root/.p10k.zsh /home/kapu/.p10k.zsh
sudo cp /root/.tmux.conf /home/kapu/.tmux.conf
sudo cp /root/.gitconfig /home/kapu/.gitconfig
sudo cp -a /root/.zshrc.d /home/kapu/.zshrc.d
sudo cp /root/.java-env.sh /home/kapu/.java-env.sh 2>/dev/null || true
sudo chown -R kapu:kapu /home/kapu/.oh-my-zsh /home/kapu/.zshrc /home/kapu/.zshenv \
  /home/kapu/.p10k.zsh /home/kapu/.tmux.conf /home/kapu/.gitconfig /home/kapu/.zshrc.d /home/kapu/.java-env.sh
```

- [ ] **Step 2.2: kapu 기본 셸 zsh로 변경**

```bash
sudo chsh -s /usr/bin/zsh kapu
getent passwd kapu | awk -F: '{print $7}'
```

**Expected:** `/usr/bin/zsh`

- [ ] **Step 2.3: kapu 로그인 검증 (별도 SSH 세션 또는 `sudo -iu kapu`)**

```bash
sudo -iu kapu zsh -ic 'echo $ZSH; echo "PS1 OK"; which claude || true'
```

**Expected:** `/home/kapu/.oh-my-zsh` 출력, 에러 없음. `claude` 명령은 Task 3에서 설치/이전 완료 후 동작.

- [ ] **Step 2.4: SSH 키 이전 (root authorized_keys → kapu)**

```bash
sudo mkdir -p /home/kapu/.ssh
sudo cp /root/.ssh/authorized_keys /home/kapu/.ssh/authorized_keys 2>/dev/null || true
# 키 페어도 함께 이전 (개인 SSH 사용)
sudo cp -a /root/.ssh/id_* /home/kapu/.ssh/ 2>/dev/null || true
sudo cp -a /root/.ssh/known_hosts /home/kapu/.ssh/ 2>/dev/null || true
sudo cp -a /root/.ssh/config /home/kapu/.ssh/ 2>/dev/null || true
sudo chown -R kapu:kapu /home/kapu/.ssh
sudo chmod 700 /home/kapu/.ssh
sudo find /home/kapu/.ssh -type f -name 'authorized_keys' -exec chmod 600 {} \;
sudo find /home/kapu/.ssh -type f -name 'id_*' ! -name '*.pub' -exec chmod 600 {} \;
sudo find /home/kapu/.ssh -type f -name '*.pub' -o -name 'known_hosts' -o -name 'config' | xargs -r sudo chmod 644
```

- [ ] **Step 2.5: kapu SSH 접속 검증 (별도 터미널에서 시도)**

```bash
# 호스트 외부 또는 별도 터미널에서:
# ssh kapu@<host>
# 성공 시 다음 단계 진행. 실패 시 키 권한/SELinux/PAM 확인.
```

**Stop rule:** kapu SSH 접속 실패 시 진행 중단. root SSH는 유지되므로 안전.

---

### Task 3: Claude Code / Codex / 개발 도구 설정 이전 (Phase 1, 다운타임 없음)

**Files:**
- `/home/kapu/.claude/`, `/home/kapu/.claude.json`, `/home/kapu/.claude-code-router/`
- `/home/kapu/.codex/`, `/home/kapu/.config/`, `/home/kapu/.local/`

- [ ] **Step 3.1: Claude Code 설정 이전 (chown 방식)**

`/root/.claude`는 34M이라 복사도 무방하지만, 메모리/세션 일관성을 위해 이동 후 root용 사본을 빈 디렉토리로 신규 작성.

```bash
sudo mv /root/.claude /home/kapu/.claude
sudo mv /root/.claude.json /home/kapu/.claude.json
sudo mv /root/.claude-code-router /home/kapu/.claude-code-router
sudo chown -R kapu:kapu /home/kapu/.claude /home/kapu/.claude.json /home/kapu/.claude-code-router
# root 폴백 (root로 claude 실행 시를 위해 빈 디렉토리)
sudo mkdir -p /root/.claude
```

- [ ] **Step 3.2: Codex 설정 이전 (26G, chown)**

```bash
sudo mv /root/.codex /home/kapu/.codex
sudo chown -R kapu:kapu /home/kapu/.codex
```

**Note:** 26G 이동도 같은 LVM이라 `mv` 인스턴트.

- [ ] **Step 3.3: 일반 config / local 이전**

```bash
sudo mv /root/.config /home/kapu/.config
sudo mv /root/.local /home/kapu/.local
sudo chown -R kapu:kapu /home/kapu/.config /home/kapu/.local
# root 폴백 (rclone, code-server-root가 아직 /root/.config/* 참조 → Phase 5에서 systemd unit 갱신)
# 임시 호환: symlink 생성, Phase 5 완료 후 제거
sudo ln -s /home/kapu/.config /root/.config
sudo ln -s /home/kapu/.local /root/.local
```

**경고:** rclone-google-drive.service 와 ima2-go.service 가 아직 `/root/.config/rclone/...`, `/root/.config/ima2-go.env`, `/root/.local/bin/ima2-server-go` 참조 중. **symlink로 다리 놓은 상태에서 Phase 5의 systemd unit 갱신 + restart 까지 한 번에 진행**해야 안전.

- [ ] **Step 3.4: 개발 런타임 chown 이전**

```bash
for d in .cargo .rustup .gradle .m2 .npm .nvm go .android .dotnet .hermes .docker; do
  if [ -e "/root/$d" ]; then
    sudo mv "/root/$d" "/home/kapu/$d"
    sudo chown -R kapu:kapu "/home/kapu/$d"
  fi
done
# .vscode-server는 code-server 사용자 변경(Task 6) 시 결정. 일단 보존.
```

**Validation:**
```bash
sudo -iu kapu zsh -ic 'cargo --version; rustc --version; go version; node --version 2>/dev/null || true'
ls /home/kapu/.claude /home/kapu/.codex >/dev/null && echo "configs OK"
```

---

### Task 4: prod 서비스 정지 (Phase 2, 다운타임 시작)

**다운타임 시작점.** 시각 기록.

- [ ] **Step 4.1: 정비창 시작 시각 기록**

```bash
date +"maintenance-start: %F %T %Z" | sudo tee -a /home/kapu/migration-archive/migration-log.txt
```

- [ ] **Step 4.2: timer 정지**

```bash
sudo systemctl stop hololive-main-log-mirror@osaka.timer
sudo systemctl stop hololive-daily-log-rollup.timer
```

- [ ] **Step 4.3: 의존성 역순으로 compose 서비스 정지**

```bash
sudo systemctl stop hololive-compose.service
sudo systemctl stop chatbotgo-compose.service
sudo systemctl stop chatbot-infra-compose.service
sudo systemctl stop cliproxy-compose.service
sudo systemctl stop ima2-go.service
sudo systemctl stop code-server-root.service
sudo systemctl stop rclone-google-drive.service
# openbao-agent는 oneshot이라 별도 stop 불필요
# openbao-compose는 /opt 기반이라 유지 (경로 영향 없음)
```

- [ ] **Step 4.4: 컨테이너 잔존 확인**

```bash
docker ps
# hololive-*, chat-bot-go-kakao-*, cliproxy-* 모두 stop 상태여야 함.
# openbao-01, buildx_buildkit_*는 유지.
```

**Expected:** hololive/chatbot/cliproxy/ima2/code-server 관련 컨테이너 0개. openbao + buildkit만 잔존.

- [ ] **Step 4.5: rclone 마운트 해제 확인**

```bash
mountpoint /mnt/google-drive && sudo fusermount3 -uz /mnt/google-drive
mountpoint /mnt/google-drive || echo "unmounted OK"
```

**Stop rule:** 컨테이너가 안 죽으면 `docker stop <name>` 수동 호출. 그래도 안 되면 강제 종료 전에 사용자 확인.

---

### Task 5: 워크트리 이동 + 보존 백업 (Phase 3)

- [ ] **Step 5.1: `/root/work` → `/home/kapu/work` 이동**

```bash
sudo mv /root/work /home/kapu/work
sudo chown -R kapu:kapu /home/kapu/work
```

**Validation:**
```bash
ls /home/kapu/work | wc -l   # 39개 디렉토리
ls /root/work 2>&1            # "No such file"
```

- [ ] **Step 5.2: 호환 symlink (Phase 5의 systemd 갱신 전까지 임시)**

```bash
sudo ln -s /home/kapu/work /root/work
```

**Note:** systemd unit 갱신 후에도 일부 스크립트(`scripts/logs/*.sh` 내 하드코딩 경로)가 남아있을 수 있어, symlink는 **Phase 6 검증 + 7일 안정화 후** 제거하는 것이 안전. 제거는 별도 Task 9.

- [ ] **Step 5.3: 보존 백업 아카이브**

```bash
sudo mkdir -p /home/kapu/migration-archive/preserved
sudo mv /root/restore-metadata /home/kapu/migration-archive/preserved/
sudo mv /root/restore-safety-20260521-003903 /home/kapu/migration-archive/preserved/
sudo mv /root/debian-migrate-backup /home/kapu/migration-archive/preserved/
sudo mv /root/kapu-*.log /root/kapu-*.sh /home/kapu/migration-archive/preserved/ 2>/dev/null || true
sudo chown -R kapu:kapu /home/kapu/migration-archive
```

- [ ] **Step 5.4: 삭제 (사용자 컨펌된 항목만)**

```bash
# Pre-flight에서 삭제 결정된 항목만 실행. 미결정 시 SKIP.
# sudo rm -rf /root/restore-staging   # 101G
# sudo rm -rf /root/.cache             # 21G
# /root/restore (5G)는 Task 10에서 삭제 (7일 안정화 후)
```

**Stop rule:** 사용자 미컨펌 시 이 step 건너뛰고 다음 Task로.

---

### Task 6: systemd 유닛 경로 갱신 (Phase 4)

**Files:**
- Modify: `/etc/systemd/system/hololive-compose.service`
- Modify: `/etc/systemd/system/chatbot-infra-compose.service`
- Modify: `/etc/systemd/system/chatbotgo-compose.service`
- Modify: `/etc/systemd/system/cliproxy-compose.service`
- Modify: `/etc/systemd/system/hololive-main-log-mirror@.service`
- Modify: `/etc/systemd/system/hololive-daily-log-rollup.service`
- Modify: `/etc/systemd/system/ima2-go.service`
- Modify: `/etc/systemd/system/rclone-google-drive.service`
- Modify: `/usr/local/sbin/hololive-compose-up`
- Create: `/etc/systemd/system/code-server-kapu.service` (Pre-flight 결정 시)
- Delete: `/etc/systemd/system/code-server-root.service` (Pre-flight 결정 시)
- Delete: `/etc/systemd/system/rclone-google-drive.service.bak-20260521-093546`

- [ ] **Step 6.1: 자동 치환 (sed in-place, 백업 포함)**

```bash
UNITS=(
  /etc/systemd/system/hololive-compose.service
  /etc/systemd/system/chatbot-infra-compose.service
  /etc/systemd/system/chatbotgo-compose.service
  /etc/systemd/system/cliproxy-compose.service
  /etc/systemd/system/hololive-main-log-mirror@.service
  /etc/systemd/system/hololive-daily-log-rollup.service
  /etc/systemd/system/ima2-go.service
  /etc/systemd/system/rclone-google-drive.service
)
for u in "${UNITS[@]}"; do
  sudo cp "$u" "$u.bak-20260521-migration"
  sudo sed -i 's|/root/work/|/home/kapu/work/|g; s|/root/\.config/|/home/kapu/.config/|g; s|/root/\.local/|/home/kapu/.local/|g; s|=/root/work$|=/home/kapu/work|g' "$u"
done
```

- [ ] **Step 6.2: 치환 결과 검증**

```bash
for u in "${UNITS[@]}"; do
  echo "=== $u ==="
  grep -E '^(WorkingDirectory|Environment|EnvironmentFile|ExecStart|ExecStop)=' "$u"
done | grep -E '/(root|home/kapu)/' | sort -u
```

**Expected:** `/root/` 매칭 0건, `/home/kapu/` 매칭만 출력.

- [ ] **Step 6.3: `/usr/local/sbin/hololive-compose-up` 갱신**

```bash
sudo cp /usr/local/sbin/hololive-compose-up /usr/local/sbin/hololive-compose-up.bak-20260521
sudo sed -i 's|/root/work/|/home/kapu/work/|g' /usr/local/sbin/hololive-compose-up
sudo grep cd /usr/local/sbin/hololive-compose-up
```

**Expected:** `cd /home/kapu/work/hololive-bot`

- [ ] **Step 6.4: code-server kapu 전환 (선택, Pre-flight 결정 시)**

```bash
sudo tee /etc/systemd/system/code-server-kapu.service > /dev/null <<'EOF'
[Unit]
Description=code-server for kapu
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=kapu
Group=kapu
Environment=HOME=/home/kapu
WorkingDirectory=/home/kapu/work
ExecStart=/usr/bin/code-server --config /home/kapu/.config/code-server/config.yaml /home/kapu/work
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl disable code-server-root.service
sudo mv /etc/systemd/system/code-server-root.service /home/kapu/migration-archive/preserved/
# .vscode-server 도 이전 (선택)
sudo mv /root/.vscode-server /home/kapu/.vscode-server
sudo chown -R kapu:kapu /home/kapu/.vscode-server
```

- [ ] **Step 6.5: rclone 백업 유닛 정리**

```bash
sudo mv /etc/systemd/system/rclone-google-drive.service.bak-20260521-093546 /home/kapu/migration-archive/preserved/
```

- [ ] **Step 6.6: daemon-reload**

```bash
sudo systemctl daemon-reload
sudo systemctl enable code-server-kapu.service   # Step 6.4 진행 시만
```

---

### Task 7: 서비스 재기동 (Phase 5)

- [ ] **Step 7.1: 의존성 순서대로 기동**

```bash
sudo systemctl start rclone-google-drive.service
sudo systemctl start cliproxy-compose.service
sudo systemctl start chatbot-infra-compose.service
sudo systemctl start chatbotgo-compose.service
sudo systemctl start hololive-compose.service
sudo systemctl start ima2-go.service
sudo systemctl start code-server-kapu.service    # Step 6.4 진행 시
```

- [ ] **Step 7.2: timer 재활성화**

```bash
sudo systemctl start hololive-main-log-mirror@osaka.timer
sudo systemctl start hololive-daily-log-rollup.timer
```

- [ ] **Step 7.3: 즉시 상태 확인**

```bash
sudo systemctl status hololive-compose chatbotgo-compose chatbot-infra-compose cliproxy-compose ima2-go rclone-google-drive --no-pager | head -100
docker ps --format 'table {{.Names}}\t{{.Status}}'
```

**Expected:** 컨테이너 15개 (또는 정비 전과 동일 개수), 5분 이내 모두 `(healthy)` 또는 `Up`.

**Stop rule:** 60초 후에도 컨테이너가 안 뜨거나 unhealthy 시 → Rollback (Task 11).

---

### Task 8: prod 헬스체크 검증 (Phase 6)

- [ ] **Step 8.1: 컨테이너별 healthcheck**

```bash
sleep 30   # healthcheck warm-up
for port in 30001 30003 30006 30007 30190; do
  echo "=== :$port ==="
  curl -fsS -m 5 "http://127.0.0.1:$port/healthz" 2>&1 | head -c 300
  echo
done | tee /home/kapu/migration-archive/snapshot-post/healthchecks.txt
```

**Expected:** 모든 포트 200 응답 또는 헬스 JSON. 베이스라인(Step 0.3)과 동일 패턴.

- [ ] **Step 8.2: log mirror 동작 확인**

```bash
sudo systemctl start hololive-main-log-mirror@osaka.service   # 수동 트리거
sudo journalctl -u hololive-main-log-mirror@osaka.service -n 30 --no-pager
ls -la /home/kapu/work/hololive-bot/logs/osaka/ | head
```

**Expected:** 신규 로그 동기화 성공.

- [ ] **Step 8.3: rclone 마운트 확인**

```bash
mountpoint /mnt/google-drive && ls /mnt/google-drive | head -5
```

**Expected:** mounted, Google Drive 컨텐츠 보임.

- [ ] **Step 8.4: kapu Claude Code 동작 확인**

```bash
sudo -iu kapu claude --version
```

**Expected:** 버전 출력, 에러 없음.

- [ ] **Step 8.5: 정비창 종료 시각 기록**

```bash
date +"maintenance-end: %F %T %Z" | sudo tee -a /home/kapu/migration-archive/migration-log.txt
```

- [ ] **Step 8.6: post-snapshot 저장**

```bash
sudo mkdir -p /home/kapu/migration-archive/snapshot-post
docker ps --format '{{.Names}}\t{{.Status}}\t{{.Image}}' | sudo tee /home/kapu/migration-archive/snapshot-post/docker-ps.txt
systemctl list-units --type=service --state=running | sudo tee /home/kapu/migration-archive/snapshot-post/systemd-running.txt
systemctl list-timers --all | sudo tee /home/kapu/migration-archive/snapshot-post/systemd-timers.txt
diff /home/kapu/migration-archive/snapshot-pre/docker-ps.txt /home/kapu/migration-archive/snapshot-post/docker-ps.txt | head -50
```

**Expected:** 컨테이너 이름 집합이 pre 와 동일. 상태(Up 시간)만 다름.

---

### Task 9: 안정화 모니터링 (Phase 6 연장, 작업 완료 후 8h 관찰)

- [ ] **Step 9.1: 8시간 후 재확인**

```bash
docker ps --format 'table {{.Names}}\t{{.Status}}' | grep -v healthy | grep -v "Up" || echo "all healthy"
sudo journalctl --since="$(date -d '8 hours ago' '+%Y-%m-%d %H:%M:%S')" -p err -u 'hololive-*' -u 'chatbotgo-*' -u 'chatbot-infra-*' -u 'cliproxy-*' -u 'ima2-*' -u 'rclone-*' --no-pager | tee /home/kapu/migration-archive/snapshot-post/errors-8h.txt
```

**Expected:** unhealthy 컨테이너 없음. journalctl error 0건 또는 마이그레이션 무관 사전 존재 에러만.

- [ ] **Step 9.2: 카카오 봇 실제 메시지 수신/응답 1회 확인 (수동)**

별도 인스턴트 메시지로 봇과 대화 1회 → 정상 응답 확인.

---

### Task 10: 클린업 (1주 후, 별도 정비창)

- [ ] **Step 10.1: 호환 symlink 제거 (선택)**

```bash
# Phase 5의 /root/work → /home/kapu/work symlink, /root/.config, /root/.local symlink 제거
# 단, journalctl/script 등이 더 이상 /root/work 참조 안 하는지 1주 관찰 후 진행.
sudo rm /root/work /root/.config /root/.local
ls /root | grep -E '^(work|\.config|\.local)$' && echo "STILL EXISTS" || echo "removed"
```

- [ ] **Step 10.2: `/root/restore` 삭제 (안정화 후)**

```bash
sudo rm -rf /root/restore
```

- [ ] **Step 10.3: systemd unit 백업 정리**

```bash
sudo find /etc/systemd/system -name '*.bak-20260521-migration' -mtime +7 -delete
```

- [ ] **Step 10.4: hololive-bot 레포 내 systemd 템플릿도 갱신 (별도 PR)**

```bash
# /root/work/hololive-bot/scripts/systemd/*.service 내 /root 참조 갱신
# 신규 PR로 별도 진행, 본 마이그레이션과는 분리
```

---

### Task 11: Rollback 절차 (장애 시)

**Trigger:** Task 7~8 에서 prod 헬스체크 실패, 컨테이너 기동 불가, 또는 사용자 판단으로 롤백.

- [ ] **Step 11.1: 서비스 정지**

```bash
sudo systemctl stop hololive-compose chatbotgo-compose chatbot-infra-compose cliproxy-compose ima2-go rclone-google-drive
sudo systemctl stop hololive-main-log-mirror@osaka.timer hololive-daily-log-rollup.timer
```

- [ ] **Step 11.2: systemd unit 복원**

```bash
for f in /etc/systemd/system/*.bak-20260521-migration; do
  sudo mv "$f" "${f%.bak-20260521-migration}"
done
sudo mv /usr/local/sbin/hololive-compose-up.bak-20260521 /usr/local/sbin/hololive-compose-up
sudo systemctl daemon-reload
```

- [ ] **Step 11.3: 경로 복원**

```bash
# symlink 제거 (있으면)
[ -L /root/work ] && sudo rm /root/work
[ -L /root/.config ] && sudo rm /root/.config
[ -L /root/.local ] && sudo rm /root/.local

# 디렉토리 되돌리기
sudo mv /home/kapu/work /root/work
sudo chown -R root:root /root/work
sudo mv /home/kapu/.config /root/.config
sudo mv /home/kapu/.local /root/.local
sudo chown -R root:root /root/.config /root/.local
# Claude/Codex는 kapu 사용을 유지하고 싶다면 그대로 둠
```

- [ ] **Step 11.4: 서비스 재기동 (롤백 후 상태)**

```bash
sudo systemctl start rclone-google-drive cliproxy-compose chatbot-infra-compose chatbotgo-compose hololive-compose ima2-go
sudo systemctl start hololive-main-log-mirror@osaka.timer hololive-daily-log-rollup.timer
docker ps
```

- [ ] **Step 11.5: 헬스체크 검증**

Task 8 의 Step 8.1 재실행. 베이스라인(Step 0.3) 과 일치 확인.

**Note:** NOPASSWD sudo(Task 1)와 kapu 셸 환경(Task 2~3)은 prod 와 무관하므로 롤백 시에도 유지 가능.

---

## Validation summary (전체 완료 기준)

```bash
# 1. NOPASSWD sudo
sudo -u kapu sudo -n true && echo "OK: NOPASSWD"

# 2. docker 그룹
sudo -iu kapu docker ps >/dev/null && echo "OK: kapu docker access"

# 3. 경로 이전
[ -d /home/kapu/work/hololive-bot ] && echo "OK: work moved"
[ -d /home/kapu/.claude ] && [ -d /home/kapu/.codex ] && echo "OK: configs moved"

# 4. systemd 유닛 /root 참조 없음 (백업 제외)
sudo grep -rE '(/root/work|/root/\.config|/root/\.local)' /etc/systemd/system/ --include='*.service' --include='*.timer' | grep -v '\.bak-' | head -5

# 5. 모든 prod 서비스 active
for s in hololive-compose chatbotgo-compose chatbot-infra-compose cliproxy-compose ima2-go rclone-google-drive openbao-compose; do
  systemctl is-active "$s" || echo "FAIL: $s"
done

# 6. 헬스체크
for port in 30001 30003 30006 30007 30190; do
  curl -fsS -m 5 "http://127.0.0.1:$port/healthz" >/dev/null && echo "OK: :$port" || echo "FAIL: :$port"
done

# 7. 컨테이너 카운트 (정비 전후 동일)
diff <(sort /home/kapu/migration-archive/snapshot-pre/docker-ps.txt | awk '{print $1}') \
     <(sort /home/kapu/migration-archive/snapshot-post/docker-ps.txt | awk '{print $1}')
```

---

## Stop rules

- **Task 1.1 visudo 검증 실패** → 절대 진행 금지. sudoers 손상 시 root 락아웃 위험. 단일 root 세션 유지하면서 즉시 수정.
- **Task 2.5 kapu SSH 접속 실패** → 키 권한/sshd_config 점검 후 재시도. root SSH가 살아있으므로 안전하게 진행.
- **Task 4.4 컨테이너 강제 정지 필요** → `docker kill` 전 사용자 컨펌. 데이터 손실 위험 평가.
- **Task 7.3 컨테이너 기동 60초 이내 unhealthy** → 1차 시도: `docker logs <container>` 로그 확인 + 1회 재시도. 2차 실패 시 Task 11 Rollback.
- **Task 8.1 헬스체크 실패** → 60초 추가 대기 후 재확인. 실패 지속 시 Task 11 Rollback.
- **`/root/restore-staging` 등 삭제 시 사용자 컨펌 누락** → Pre-flight 표 미작성 항목은 모두 SKIP (보존).

---

## 정비창 타임라인 예측

| Phase | 작업 | 다운타임 | 누적 |
|---|---|---|---|
| Task 0 | Pre-flight 스냅샷 | 0 | 0 |
| Task 1 | NOPASSWD + docker 그룹 | 0 | 0 |
| Task 2 | kapu 셸 환경 | 0 | 0 |
| Task 3 | Claude/Codex/런타임 이전 | 0 (caches, 26G+ 이동도 mv 인스턴트) | 0 |
| Task 4 | 서비스 정지 | 1~2분 | 2분 |
| Task 5 | work 이동 + 백업 | <1분 (mv 인스턴트) | 3분 |
| Task 6 | systemd unit 갱신 | 1~2분 (sed + daemon-reload) | 5분 |
| Task 7 | 서비스 재기동 | 3~5분 (healthcheck warm-up) | 10분 |
| Task 8 | 헬스체크 검증 | 5분 | **15분** |
| Task 9 | 안정화 (백그라운드) | - | - |
| Task 10 | 클린업 (1주 후) | 0 | - |

**예상 다운타임: 약 15분.** 봇 응답 일시 중단 정도. Rollback 시나리오는 +10분.

---

## Open questions

1. **카카오 봇 무응답 허용 시간:** 15분 다운타임이 사용자에게 허용 가능한지 확인.
2. **정비 시각 선택:** 트래픽 최저 시간대 (보통 새벽 3~5시 KST)에 진행하는지, 즉시 진행인지.
3. **`/root/restore-staging` (101G):** LVM 마이그레이션 잔여 데이터인지, 활성 백업인지 사용자 최종 확인.
4. **github-runner 사용자 처리:** `/home/github-runner` 가 별도 시스템 유저로 분리되어 있음. 그대로 둘지, 또는 통합할지. 현재 plan은 그대로 둠.
5. **`.vscode-server` 이전 여부:** code-server-root → code-server-kapu 전환 시 함께 이동. 전환 안 하면 root에 유지.
