# Hololive Admin

웹 소스와 이미지 빌드는 [Iris Admin](https://github.com/park285/iris-admin)의
`hololive-web`, `tools/iris-admin-web`, `Dockerfile.hololive`로 이관했습니다.
기존 홀로 디자인과 업무 API를 유지하며 Rust가 인증·세션·재인증·CSRF·중복 변경 방지를 담당합니다.

이 저장소는 [Compose](../deploy/compose/docker-compose.prod.yml)와
[운영 절차](../docs/current/runbooks/admin-dashboard.md)를 소유합니다.
Docker 제어·호스트 리소스·원시 메트릭은 이 화면에 제공하지 않습니다.
