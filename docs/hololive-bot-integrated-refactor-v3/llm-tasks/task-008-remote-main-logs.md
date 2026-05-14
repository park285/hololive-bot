# Task 008. remote logs into main /logs

## 목표

Osaka 등 원격 서버의 `youtube-scraper`, `stream-ingester` 로그를 메인 서버 `/logs`에서 본다.

## 수정 파일

- `scripts/logs/remote-sync-main-logs.sh`
- `scripts/systemd/hololive-main-log-mirror@.service`
- `scripts/systemd/hololive-main-log-mirror@.timer`

## 적용

```bash
sudo mkdir -p /logs
sudo chown -R kapu:docker /logs
sudo chmod 2770 /logs

chmod +x scripts/logs/remote-sync-main-logs.sh
LOG_ROOT=/logs ./scripts/logs/remote-sync-main-logs.sh once osaka
```

## 결과

```text
/logs/youtube-scraper.log -> /logs/remote/osaka/youtube-scraper.log
/logs/stream-ingester.log -> /logs/remote/osaka/stream-ingester.log
```
