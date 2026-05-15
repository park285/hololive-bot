# OpenBao Env Cleanup Runbook

## 목적

OpenBao Agent가 렌더링한 `/run/hololive-bot/env`를 운영 Compose env의 단일 정본으로 고정하기 위해, 운영 호스트에 남은 local `.env` 파일과 shell profile export 잔재를 정리합니다.

이 절차는 secret 값을 출력하지 않습니다. 파일 삭제는 후보를 먼저 확인한 뒤 실행합니다.

## 1. OpenBao env 확인

운영 호스트에서 `/run/hololive-bot/env`가 읽히는지 먼저 확인합니다.

```bash
sudo find /run/hololive-bot -maxdepth 2 -printf "%M %u %g %s %p\n" | sort
test -r /run/hololive-bot/env || {
  echo "env file is not readable by $(id -un)"
  exit 1
}
```

secret 값 대신 key 이름만 확인합니다.

```bash
awk -F= '
  /^[[:space:]]*(#|$)/ { next }
  index($0, "=") > 0 {
    key=$1
    gsub(/^[[:space:]]+|[[:space:]]+$/, "", key)
    print key
  }
' /run/hololive-bot/env | sort
```

wrapper preflight를 통과해야 합니다.

```bash
./scripts/deploy/compose.sh -f docker-compose.prod.yml config --quiet
```

Osaka overlay 호스트에서는 overlay까지 함께 확인합니다.

```bash
sudo -n env COMPOSE_ENV_FILE=/run/hololive-bot/env \
  ./scripts/deploy/compose.sh \
  -f docker-compose.prod.yml \
  -f docker-compose.osaka.yml \
  config --quiet
```

## 2. 저장소 내부 local env 후보 찾기

저장소 루트에서 실행합니다.

```bash
find . \
  \( -name '.env' \
     -o -name '.env.local' \
     -o -name '.env.osaka' \
     -o -name '.env.pre-*' \
     -o -name '*.env.local' \
     -o -name '*env.backup*' \
     -o -name '*env.bak*' \
     -o -name '*env.pre-*' \) \
  -not -path './.git/*' \
  -print
```

`.env.example` 같은 template 파일은 삭제 대상이 아닙니다. 실제 secret 또는 운영 runtime 값이 들어간 local `.env`, `.env.local`, `.env.osaka`, backup env 파일만 삭제 대상입니다.

tracked secret 여부도 확인합니다.

```bash
git ls-files | grep -E '(^|/)\.env($|[.])|\.env\.|env\.bak|env\.backup|env\.pre' || true
```

결과가 나오고 실제 secret이 포함되어 있었다면 단순 삭제만으로 끝내지 말고 secret rotation과 history cleanup을 별도 보안 작업으로 처리합니다.

## 3. 삭제 전 메타데이터 기록

파일 내용은 복사하지 않습니다. 파일명, 권한, 크기만 기록합니다.

```bash
mkdir -p /tmp/hololive-env-cleanup
find . \
  \( -name '.env' \
     -o -name '.env.local' \
     -o -name '.env.osaka' \
     -o -name '.env.pre-*' \
     -o -name '*.env.local' \
     -o -name '*env.backup*' \
     -o -name '*env.bak*' \
     -o -name '*env.pre-*' \) \
  -not -path './.git/*' \
  -printf '%M %u %g %s %TY-%Tm-%Td %TH:%TM %p\n' \
  > /tmp/hololive-env-cleanup/env-files-before.txt
```

## 4. 저장소 내부 local env 삭제

먼저 삭제 후보를 다시 확인합니다.

```bash
find . \
  \( -name '.env' \
     -o -name '.env.local' \
     -o -name '.env.osaka' \
     -o -name '.env.pre-*' \
     -o -name '*.env.local' \
     -o -name '*env.backup*' \
     -o -name '*env.bak*' \
     -o -name '*env.pre-*' \) \
  -not -path './.git/*' \
  -type f \
  -print
```

후보가 hololive-bot 운영 env 잔재임을 확인한 뒤 삭제합니다.

```bash
find . \
  \( -name '.env' \
     -o -name '.env.local' \
     -o -name '.env.osaka' \
     -o -name '.env.pre-*' \
     -o -name '*.env.local' \
     -o -name '*env.backup*' \
     -o -name '*env.bak*' \
     -o -name '*env.pre-*' \) \
  -not -path './.git/*' \
  -type f \
  -exec sh -c '
    for f do
      echo "[DELETE] $f"
      if command -v shred >/dev/null 2>&1; then
        shred -u "$f"
      else
        rm -f "$f"
      fi
    done
  ' sh {} +
```

empty backup directory만 정리합니다.

```bash
find . -type d \( -name 'backup' -o -name 'backups' \) -empty -delete
```

## 5. 저장소 밖 잔재 확인

운영 host에 여러 checkout이나 backup directory가 있을 수 있습니다. scan root는 호스트별 실제 작업 경로로 명시하고, 파일명과 메타데이터만 확인합니다.

```bash
SCAN_ROOTS="$HOME /srv/hololive-bot /opt/hololive-bot"

for root in $SCAN_ROOTS; do
  [ -d "$root" ] || continue
  echo "== scan $root =="
  find "$root" \
    \( -name '.env' \
       -o -name '.env.local' \
       -o -name '.env.osaka' \
       -o -name '.env.pre-*' \
       -o -name '*.env.local' \
       -o -name '*env.backup*' \
       -o -name '*env.bak*' \
       -o -name '*env.pre-*' \) \
    -not -path '*/.git/*' \
    -printf '%M %u %g %s %TY-%Tm-%Td %TH:%TM %p\n' 2>/dev/null
done
```

저장소 밖 파일은 다른 서비스의 env일 수 있습니다. hololive-bot 운영 env 잔재로 확인된 파일만 삭제합니다.

## 6. shell profile export 정리

파일을 삭제해도 shell profile에 runtime env가 export되어 있으면 wrapper preflight가 실패합니다. 아래 명령은 값이 아니라 line 위치만 찾습니다.

```bash
profile_files=(
  "$HOME/.bashrc"
  "$HOME/.bash_profile"
  "$HOME/.profile"
  "$HOME/.zshrc"
)

for f in "${profile_files[@]}"; do
  [ -f "$f" ] || continue
  awk '
    /(^|[[:space:]])(export[[:space:]]+)?(DB_PASSWORD|CACHE_PASSWORD|IRIS_BOT_TOKEN|IRIS_WEBHOOK_TOKEN|ADMIN_PASS_BCRYPT|SESSION_SECRET|ALARM_DISPATCH_|POSTGRES_|CACHE_|YOUTUBE_SCRAPER_RUNTIME_ALLOWED|YOUTUBE_OUTBOX_DISPATCHER_ENABLED|DELIVERY_DISPATCHER_ENABLED|NOTIFICATION_EGRESS_ROLE|NOTIFICATION_SCHEDULER_ROLE)=/ {
      print FILENAME ":" FNR
    }
  ' "$f"
done

if [ -d "$HOME/.config" ]; then
  find "$HOME/.config" -maxdepth 3 -type f -print0 2>/dev/null \
    | xargs -0 -r awk '
      /(^|[[:space:]])(export[[:space:]]+)?(DB_PASSWORD|CACHE_PASSWORD|IRIS_BOT_TOKEN|IRIS_WEBHOOK_TOKEN|ADMIN_PASS_BCRYPT|SESSION_SECRET|ALARM_DISPATCH_|POSTGRES_|CACHE_|YOUTUBE_SCRAPER_RUNTIME_ALLOWED|YOUTUBE_OUTBOX_DISPATCHER_ENABLED|DELIVERY_DISPATCHER_ENABLED|NOTIFICATION_EGRESS_ROLE|NOTIFICATION_SCHEDULER_ROLE)=/ {
        print FILENAME ":" FNR
      }
    ' 2>/dev/null
fi
```

나온 줄에서 운영 secret 또는 runtime policy export를 제거합니다. 편집 후 현재 shell에서도 제거합니다.

```bash
unset DB_PASSWORD CACHE_PASSWORD IRIS_BOT_TOKEN IRIS_WEBHOOK_TOKEN ADMIN_PASS_BCRYPT SESSION_SECRET
unset ALARM_DISPATCH_PUBLISH_MODE ALARM_DISPATCH_CONSUMER_MODE ALARM_DISPATCH_WAKEUP_ENABLED
unset POSTGRES_HOST POSTGRES_PORT POSTGRES_USER POSTGRES_DB POSTGRES_PASSWORD
unset CACHE_HOST CACHE_PORT CACHE_PASSWORD CACHE_SOCKET_PATH
unset YOUTUBE_SCRAPER_RUNTIME_ALLOWED YOUTUBE_OUTBOX_DISPATCHER_ENABLED DELIVERY_DISPATCHER_ENABLED
unset NOTIFICATION_EGRESS_ROLE NOTIFICATION_SCHEDULER_ROLE
```

남은 위험 후보는 key 이름만 확인합니다.

```bash
env | awk -F= '
  $1 ~ /^(DB_PASSWORD|CACHE_PASSWORD|IRIS_BOT_TOKEN|IRIS_WEBHOOK_TOKEN|ADMIN_PASS_BCRYPT|SESSION_SECRET)$/ { print $1 }
  $1 ~ /^ALARM_DISPATCH_/ { print $1 }
  $1 ~ /^POSTGRES_/ { print $1 }
  $1 ~ /^CACHE_/ { print $1 }
  $1 ~ /^YOUTUBE_/ { print $1 }
  $1 ~ /^NOTIFICATION_/ { print $1 }
' | sort
```

`COMPOSE_ENV_FILE`, `OPENBAO_HOLOLIVE_ENV_FILE`, `COMPOSE_PROFILES`, `SHARED_GO_WORKSPACE_PATH`, `IRIS_CLIENT_GO_WORKSPACE_PATH` 같은 wrapper control key는 허용됩니다.

## 7. 운영 명령 고정

정리 이후 raw `docker compose` 대신 wrapper를 사용합니다.

```bash
./scripts/deploy/compose.sh -f docker-compose.prod.yml ps
./scripts/deploy/compose.sh -f docker-compose.prod.yml logs -f hololive-alarm-worker
./scripts/deploy/compose-redeploy-service.sh hololive-alarm-worker
```

비운영 또는 테스트는 local fallback이 아니라 `COMPOSE_ENV_FILE`을 명시합니다.

```bash
COMPOSE_ENV_FILE=./.env.local ./scripts/deploy/compose.sh -f docker-compose.prod.yml config --quiet
COMPOSE_ENV_FILE=./.env.local ./build-all.sh --no-bump --build-only --skip-local-ci
```

## 8. cleanup 완료 검증

```bash
./scripts/deploy/compose.sh -f docker-compose.prod.yml config --quiet
./build-all.sh --no-bump --build-only --skip-local-ci
./scripts/deploy/test-compose-env.sh
```

Osaka host에서는 다음도 확인합니다.

```bash
sudo -n env COMPOSE_ENV_FILE=/run/hololive-bot/env \
  ./scripts/deploy/compose.sh \
  -f docker-compose.prod.yml \
  -f docker-compose.osaka.yml \
  config --quiet
```

마지막으로 local env 잔재가 없는지 다시 확인합니다.

```bash
find . \
  \( -name '.env' \
     -o -name '.env.local' \
     -o -name '.env.osaka' \
     -o -name '.env.pre-*' \
     -o -name '*.env.local' \
     -o -name '*env.backup*' \
     -o -name '*env.bak*' \
     -o -name '*env.pre-*' \) \
  -not -path './.git/*' \
  -print
```

결과가 없어야 합니다. `.env.example`은 template 파일이므로 남겨도 됩니다.
