# runtime-config

이 디렉터리는 운영 중 컨테이너에 read-only로 마운트되는 비밀이 아닌 파일 기반 런타임 설정 위치다.

현재 기본 compose 설정은 다음 파일을 참조할 수 있다.

- `iris_base_url`: `IRIS_BASE_URL_FILE=/app/runtime-config/iris_base_url`을 사용할 때의 Iris base URL 파일

Iris H3 CA bundle은 OpenBao Agent가 `/run/hololive-bot/certs/iris-ca.pem`에 렌더링한다.
실제 값 파일은 커밋하지 않는다. 예시는 `iris_base_url.example`만 커밋한다.
