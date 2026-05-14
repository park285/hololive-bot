# Main server /logs layout

사용자가 원하는 최종 상태는 아래입니다.

```text
/logs/
  bot.log
  alarm-worker.log
  llm-scheduler.log
  youtube-scraper.log
  stream-ingester.log
```

`youtube-scraper.log`, `stream-ingester.log`는 실제로는 원격 Osaka host에서 온 로그입니다.

구분을 위해 내부 원본은 아래에 둡니다.

```text
/logs/remote/osaka/youtube-scraper.log
/logs/remote/osaka/stream-ingester.log
```

그리고 상위 `/logs`에는 symlink를 둡니다.

```bash
/logs/youtube-scraper.log -> remote/osaka/youtube-scraper.log
/logs/stream-ingester.log -> remote/osaka/stream-ingester.log
```

이 방식의 장점:
- 운영자는 그냥 `tail -f /logs/youtube-scraper.log`만 쓰면 된다.
- 원격 로그인지 확인하고 싶으면 `readlink /logs/youtube-scraper.log`를 보면 된다.
- 로컬 service가 실수로 같은 파일을 쓰는 경우 충돌을 피할 수 있다.
- 원격 host가 늘어나면 `/logs/remote/<target>/`만 추가하면 된다.

## 명령

```bash
./scripts/logs/remote-sync-main-logs.sh once osaka
./scripts/logs/remote-sync-main-logs.sh status osaka

tail -F /logs/youtube-scraper.log
tail -F /logs/stream-ingester.log
```

## 기존 regular file이 있을 때

만약 `/logs/youtube-scraper.log`가 이미 일반 파일이면 script는 기본적으로 덮어쓰지 않습니다.

강제로 symlink로 바꾸려면:

```bash
FORCE_MAIN_LOG_LINKS=1 ./scripts/logs/remote-sync-main-logs.sh once osaka
```

기존 파일은 다음처럼 backup 됩니다.

```text
/logs/youtube-scraper.log.local.YYYYMMDD-HHMMSS
```
