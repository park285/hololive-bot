# Task 007. ingestion/youtube-scraper flow

## 목표

`stream-ingester`와 `youtube-scraper`가 같은 module을 써도 runtime role을 로그와 구조에서 구분한다.

## 이벤트

```text
ingestion.runtime.configured
youtube.poll.iteration.started
youtube.poll.iteration.succeeded
youtube.poll.iteration.failed
youtube.outbox.write.started
youtube.outbox.write.succeeded
youtube.outbox.write.failed
photo.sync.iteration.started
photo.sync.iteration.succeeded
photo.sync.iteration.failed
```

## 금지

- youtube-scraper에서 final Iris/Kakao send 기록 금지
- stream-ingester에서 YouTube outbox production ownership 금지
