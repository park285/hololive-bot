# Admin Dashboard 배포 경계

웹 구현·OpenAPI·인증·세션·변경 작업 보호는 `iris-admin` 저장소가 소유합니다.
`hololive-web`은 홀로 디자인을 유지하며 공통 Rust gateway의 `/dashboard/*` 영역과 React SSR을 사용합니다.
로그인·계정별 권한·PWA·native 배포는 통합 관리자 앱이 소유합니다.

이 저장소에는 웹 backend/frontend를 다시 만들지 않습니다. 업무 로직은 `hololive/hololive-api`,
배포 설정은 `deploy/compose`, 운영 절차는 `docs/current/runbooks/admin-dashboard.md`가 소유합니다.
공개 주소는 `admin.holoshi.com`이며 독립 `admin-dashboard` 서비스와 포트 `30190`은 폐기했습니다.
이 저장소의 shortlink ingress와 업무 API H3 publish는 유지합니다.

검증: 이 저장소는 `bash scripts/ci/public-pr-frontend-gate.sh`, 웹 저장소는
`bash scripts/verify-all.sh`를 실행합니다. 운영 적용은 별도 승인된 배포 절차를 따릅니다.
