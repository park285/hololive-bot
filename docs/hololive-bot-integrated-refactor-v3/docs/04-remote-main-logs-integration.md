# 04. Remote Runtime Logs Into Main `/logs`

## 문제

`youtube-scraper`와 `stream-ingester`가 Osaka 등 다른 서버에서 실행되면 메인 서버에서 로그 확인이 불편해집니다.

## 목표

메인 서버에서 아래 경로로 모든 로그를 봅니다.

```text
/logs/bot.log
/logs/alarm-worker.log
/logs/llm-scheduler.log
/logs/youtube-scraper.log
/logs/stream-ingester.log
```

이때 `youtube-scraper.log`, `stream-ingester.log`는 원격 서버 파일을 메인 서버로 mirror한 파일입니다.

## 권장 구조

```text
/logs/
  youtube-scraper.log -> remote/osaka/youtube-scraper.log
  stream-ingester.log -> remote/osaka/stream-ingester.log

  remote/
    osaka/
      youtube-scraper.log
      stream-ingester.log
      archive/
```

## 동기화 방식

메인 서버가 원격 서버에서 pull합니다.

이유:
- 원격 서버에 메인 서버 write 권한을 크게 열지 않아도 됨
- SSH key 하나로 제어 가능
- 장애 시 수동 실행이 쉬움
- `/logs` 파일 운영 습관 유지 가능

## systemd timer

30초 또는 60초 주기로 rsync합니다.

```bash
sudo systemctl enable --now hololive-main-log-mirror@osaka.timer
```

## 주의

`/logs/youtube-scraper.log`에 직접 원격 파일을 덮어쓰지 말고 symlink를 사용합니다. 이렇게 해야 로컬 runtime과 원격 runtime 파일 writer가 충돌하지 않습니다.
