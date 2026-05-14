# docker-compose.osaka.yml log rotation patch

원격 로그를 메인 서버 `/logs`에 mirror하려면 Osaka 쪽 로그가 너무 빨리 rotate 되면 안 됩니다.
현재 2MB/2 backups는 장애 burst 상황에서 너무 작을 수 있습니다.

권장 patch:

```diff
 services:
   stream-ingester:
     environment:
-      LOG_MAX_SIZE_MB: "2"
-      LOG_MAX_BACKUPS: "2"
-      LOG_MAX_AGE_DAYS: "7"
+      LOG_MAX_SIZE_MB: "20"
+      LOG_MAX_BACKUPS: "10"
+      LOG_MAX_AGE_DAYS: "14"
       LOG_COMPRESS: "true"
@@
     logging:
       driver: "json-file"
       options:
-        max-size: "2m"
-        max-file: "2"
+        max-size: "5m"
+        max-file: "3"

   youtube-scraper:
     environment:
-      LOG_MAX_SIZE_MB: "2"
-      LOG_MAX_BACKUPS: "2"
-      LOG_MAX_AGE_DAYS: "7"
+      LOG_MAX_SIZE_MB: "20"
+      LOG_MAX_BACKUPS: "10"
+      LOG_MAX_AGE_DAYS: "14"
       LOG_COMPRESS: "true"
@@
     logging:
       driver: "json-file"
       options:
-        max-size: "2m"
-        max-file: "2"
+        max-size: "5m"
+        max-file: "3"
```
