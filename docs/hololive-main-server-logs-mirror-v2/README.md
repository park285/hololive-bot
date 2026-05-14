# Hololive main-server /logs mirror v2

목표:
- Osaka 서버에서 실행 중인 `youtube-scraper`, `stream-ingester` 로그를 메인 서버 `/logs`에서 그대로 본다.
- 운영자는 `/logs/youtube-scraper.log`, `/logs/stream-ingester.log`만 보면 된다.
- 실제 원본 mirror는 `/logs/remote/osaka/`에 두고, `/logs/*.log`는 symlink로 연결한다.
- 이렇게 하면 원격 로그와 로컬 로그가 한 디렉터리에서 보이면서도, 원격 원본 위치를 구분할 수 있다.

최종 구조:
```text
/logs/
  bot.log
  alarm-worker.log
  llm-scheduler.log
  youtube-scraper.log      -> remote/osaka/youtube-scraper.log
  stream-ingester.log      -> remote/osaka/stream-ingester.log
  remote/
    osaka/
      youtube-scraper.log
      stream-ingester.log
      archive/
        ...
```

적용은 repository root에서 수행합니다.

검증:
```bash
./docs/hololive-main-server-logs-mirror-v2/scripts/verify-main-log-mirror-v2.sh
```

적용:
```bash
sudo mkdir -p /logs
sudo chown -R "$USER":docker /logs
chmod +x scripts/logs/remote-sync-main-logs.sh

./scripts/logs/remote-sync-main-logs.sh once osaka
tail -f /logs/youtube-scraper.log
tail -f /logs/stream-ingester.log
```

systemd:
```bash
sudo cp scripts/systemd/hololive-main-log-mirror@.service /etc/systemd/system/
sudo cp scripts/systemd/hololive-main-log-mirror@.timer /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now hololive-main-log-mirror@osaka.timer
```

주의:
- `docker-compose.osaka.yml`에는 `youtube-scraper`, `stream-ingester`의 app file log 보존값을 `20MB / 10 backups / 14 days`로, Docker `json-file` 보존값을 `5m / 3 files`로 반영합니다.
- live service 재기동이나 timer enable은 운영 변경이므로 별도 승인 후 수행합니다.
