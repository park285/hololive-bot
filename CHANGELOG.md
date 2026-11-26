# 변경 이력

현재 릴리즈 버전은 루트 `VERSION`에서 관리하고 Git tag는 `r<version>` 형식을 사용합니다. 번호는 r1.0.0부터 다시 시작했으며, `v<version>` tag와 절은 이전 번호 체계의 기록입니다.
버전은 배포하거나 게시할 때만 올리며, 절차는 [릴리즈 버전 관리](docs/current/runbooks/release.md)를 따릅니다.
이 문서는 `미출시`와 최근 세 버전 구간(그 사이의 tag 없는 준비 구간은 세지 않음)만 유지하며, 이전 기록은 이 파일의 Git 이력과 tag에서 확인합니다.

## 미출시

## r1.2.13 - 2026-11-26

- **YouTube 원본 보관 정리의 보호 판정 지연:** 보존 대상 판정(`source_observation_retention_protected_ids`)의 "세션의 최신 적용 원본" 조건을 PostgreSQL이 해시 집합으로 바꿔, 요청 1,000건마다 LIVE·UPCOMING 세션 전체의 적용 기록(운영 약 42만 행)을 훑었습니다. 정리 한 번이 최대 9.9초 걸려 10초 예산을 넘기면 `SQLSTATE 57014`로 취소됐고, DB slot 하나를 그동안 붙잡아 같은 slot을 기다리던 원본 소비가 `acquire DB slot: context deadline exceeded`로 실패했습니다. migration 008이 이 조건을 요청 id에서 출발하도록 바꿉니다. 운영 데이터에서 판정 결과는 같고, 오래된 원본 1,000건 판정이 4.8초에서 12ms로 줄었습니다. 대기 migration이므로 `docs/current/runbooks/release.md`의 "대기 migration 적용 창" 절차로 배포합니다.

## r1.2.12 - 2026-11-26

- **종료 live head 일회성 복구 퇴역:** 2026-09-28에 승인 범위 24건을 적용해 끝난 과거 LIVE head 대조 복구(`scripts/maintenance/preview-terminal-live-heads.sql`·`reconcile-terminal-live-heads.sql`, `scripts/runtime/test-terminal-live-head-repair.sh`, runbook)를 지웠습니다. 대상 조건이 2026-08-17 이전 head로 고정돼 다시 쓸 일이 없습니다.
- **PostgreSQL 19의 기존 행 반환 활용:** replay epoch 활성화와 YouTube 발송 요청 동결에 `ON CONFLICT DO SELECT ... RETURNING`을 적용했습니다. replay epoch는 중복 활성화 시, 발송 요청은 신규·중복 요청 모두에서 별도 재조회 한 번을 줄입니다. 최초 활성화 여부는 `OLD.singleton IS NULL`로 구분하고 기존 cutoff·작업자·사유를 보존합니다. 발송 요청은 저장된 본문 해시·경로·수신방·멤버·중복 제거 키의 검증과 재발급 세대를 유지합니다. 스키마·권한·행 잠금 강도는 바꾸지 않으며 PostgreSQL 19가 필요합니다. 적용 범위와 후속 관측 데이터 최적화 조건은 [설계 문서](docs/current/architecture/postgres19-get-or-create-20261126.md)에 정리했습니다.
- **AP 전환 owner의 신호 처리:** owner가 worker pid를 기록하는 사이에 신호가 도착하면, 이어서 시작한 차단형 `wait`이 멈춘 worker를 끝없이 기다릴 수 있었습니다. 이제 신호 상태와 worker 생존을 0.1초 간격으로 함께 확인하며 기다립니다. 신호가 온 뒤 worker 종료를 기다리는 시간도 5초에서 30초로 늘렸습니다. 끝나 가는 `systemctl restart`나 부하로 늦어진 종료가 `outcome_unknown`으로 잘못 남지 않게 하기 위해서입니다. 30초 뒤에도 남은 worker는 이전과 같이 guard를 보존한 채 `outcome_unknown`으로 남습니다.

## r1.2.11 - 2026-11-25

- **osaka1 배포의 swap 경로 누락:** #881이 최초 설치용 swap 생성 코드를 지우면서 `swapfile=/swapfile` 정의도 함께 지웠습니다. 그 결과 기존 호스트 검사(`native_existing_host_check`)가 `swapfile: parameter null or not set`으로 실패해, main의 `ap-host-native-deploy.sh osaka --apply`가 원점 확인 단계에서 항상 멈췄습니다. 검사 함수가 경로를 직접 가집니다. collector 런북의 swap 서술도 "없으면 만든다"에서 "기존 swap을 확인하고 다르면 거부한다"로 고쳤습니다.
