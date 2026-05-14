# Patch: docker-compose.osaka.yml log rotation

현재 Osaka overlay는 app file log와 Docker json-file log가 작게 잡혀 있습니다.
원격 로그를 메인 서버 `/logs`로 mirror할 때 장애 상황의 로그 유실 가능성을 줄이려면 아래처럼 조정합니다.

```yaml
services:
  stream-ingester:
    environment:
      LOG_MAX_SIZE_MB: "20"
      LOG_MAX_BACKUPS: "10"
      LOG_MAX_AGE_DAYS: "14"
      LOG_COMPRESS: "true"
    logging:
      driver: "json-file"
      options:
        max-size: "5m"
        max-file: "3"

  youtube-scraper:
    environment:
      LOG_MAX_SIZE_MB: "20"
      LOG_MAX_BACKUPS: "10"
      LOG_MAX_AGE_DAYS: "14"
      LOG_COMPRESS: "true"
    logging:
      driver: "json-file"
      options:
        max-size: "5m"
        max-file: "3"
```
