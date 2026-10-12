# PGO 빌드 설정

`hololive-api`, `hololive-alarm-worker`, `youtube-collector`의 운영 빌드는 `-pgo=off`를 사용합니다.
API와 alarm-worker는 각 Dockerfile, collector는 Docker와 host-native가 공통으로 호출하는
`scripts/build/build-youtube-collector-go.sh`에서 이 옵션을 지정합니다.

품질 검증은 `scripts/ci/local-ci.sh`의 제품 빌드·테스트가 담당합니다.
PGO 비활성 상태를 확인하기 위한 별도 빌드나 정책 목록은 두지 않습니다.

PGO 도입은 대표 workload의 성능·자원 측정, profile 출처와 배포·복구 계획을 포함한
별도 설계와 명시적 승인이 필요합니다. 라이브 트래픽을 이용한 조사도 별도 승인을 받습니다.
