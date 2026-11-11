# runtime-config

이 디렉터리는 운영 중 컨테이너에 read-only로 마운트되는 비밀이 아닌 파일 기반 런타임 설정 위치다.

중앙 Quadlet unit(`deploy/quadlet/hololive-bot/hololive-api.container`, `hololive-alarm-worker.container`)은
host의 runtime-config 디렉터리를 `/app/runtime-config`에 read-only로 마운트하고 다음 파일을 참조한다.

- `iris_base_url`: `IRIS_BASE_URL_FILE=/app/runtime-config/iris_base_url`을 사용할 때의 Iris base URL 파일

Iris H3 CA bundle은 승인된 static-secret distribution으로 host에 배치된 뒤 같은 Quadlet unit이
container의 `/run/hololive-bot/certs/iris-ca.pem`에 read-only로 마운트한다. Source와
host 경로는 `hololive-bot-ops`가 resolve하고, 별도 배포가 필요하면
`stack-platform-ops`로 라우팅한다. 실제 값 파일은 커밋하지 않는다. 예시는
`iris_base_url.example`만 커밋한다.
