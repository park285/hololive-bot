# DB·런타임 실측 기반 최적화

## 목표와 경계

2026-10-06 읽기 전용 운영 점검과 로컬 재현으로 확인한 반복 계산·쓰기 비용 및 취소/계측 결함을 수정한다. 초기 범위는 로컬 구현과 검증이었으며, 적대적 리뷰 뒤 사용자가 커밋·원격 푸시·라이브 반영을 승인했다. 보존기간, 관측/replay 의미, 발송 멱등성, 잠금 순서와 기존 영수증 원본을 보존한다. 이번 최적화 자체는 새 런타임 의존성을 추가하지 않는다.

## 구현과 검증

- [x] 검토 영수증 비교: migration 270에서 기존 영수증의 진단 배열을 비교 전에 한 번 제외하고 고정한다. 원본·의미 사실 함수·검토 조건·권한은 유지한다. 구형/신형 영수증, 사실 변경, 시간대·head NULL, 최신 일치 영수증 선택, 재적용·원본 보존 테스트를 통과했다.
- [x] Live head 저장: 내용이 같은 ignored absence 배열의 기존 TOAST 값을 유지한다. 새 테스트는 기존 코드에서 청크 ID 변경으로 실패했고 수정 후 통과했다. 스칼라 변경, 실제 배열 변경·초기화, 동일 replay의 무변경도 확인했다.
- [x] 관리 통계: 수집 주도 요청이 취소되면 결과를 공유 캐시에 게시하지 않는다. 재현 테스트는 기존 코드에서 취소를 성공으로 반환하고 장애를 캐시해 실패했으며, 수정 후 정상 대기자가 새 수집을 수행한다. 기존 대기자 취소·캐시 복사 동작도 통과했다.
- [x] 보존 삭제: 앞 단계의 확정 삭제량과 실패 테이블을 구분한다. 큰 두 테이블(source observations·applications)의 인덱스 제한 조회로 적체를 측정하며 0도 갱신한다. 원본은 종류별 최대 1,000행을 검사한다. 보호된 행이 한도를 채우면 미확정으로 처리하고 지난 지표를 제거한다. 다른 테이블의 적체는 측정하지 않는다. 보존정책과 삭제 배치는 유지한다.
- [x] 영향 네 패키지의 전체 일반/race PostgreSQL 테스트, migration 재적용·schema snapshot·manifest, lint·Staticcheck·NilAway와 API 모듈 빌드를 통과했다. 스택 DB 정책 검사는 통과했으며, 금지 경로를 읽는 두 교차 스택 검사는 아래 한계에 명시했다.

## 별도 판단이 필요한 후보

DB idle transaction timeout은 migration 183의 5분과 달리 운영 기본값이 0이다. 원인과 의도적 변경 여부 확인 후 운영 승인 범위에서 복구한다. Collector 배분·왕복 재설계, worker/pool 증설, 해시 저장 타입 변경, 시청자 표본 폐기는 이번 국소 수정과 분리한다. 완료된 backfill용 인덱스 제거도 이번 변경에는 포함하지 않는다.

## 측정 결과

- 최종 270 함수 본문과 같은 읽기 전용 조회를 운영 데이터에서 비교했다. 같은 statement의 기존/신규 결과는 각각 56행이며 양방향 EXCEPT 차이는 0행이다. 원래 함수 정의는 변경하지 않았다.
- 기존/신규를 3회 번갈아 실행한 SELECT 계획의 실행 시간은 각각 270.310/28.387ms, 248.650/30.519ms, 255.051/30.197ms였다. 평균 약 88.5% 감소다. 배포 후 실제 애플리케이션 성능은 별도 관측 대상이다.
- 새 적체 조회는 기본 보존기간 입력으로 읽기 전용 실행 시 원본 4.337ms, application 2.218ms였다. 값은 당시 데이터와 캐시 상태의 측정치다.
- 이전 17,000원소 합성 실험은 동일 배열을 다시 배정할 때 약 102KB/update, 기존 datum을 유지할 때 179B/update WAL이었다. 최종 제품 SQL의 회귀 테스트는 실제 TOAST 청크 재사용으로 불필요한 재저장이 제거됐음을 확인한다.

## 검증 범위와 한계

영향 패키지는 admin system, sourceobservation, YouTube runtime, hololive-dbtest다. 네 패키지 전체 일반 테스트와 `go test -race -p 2 -count=1`, `go build ./hololive/hololive-api/...`, go vet, Staticcheck, NilAway, golangci-lint, migration manifest, SQL ownership, stack DB access policy를 통과했다. Schema snapshot은 생성 명령으로 갱신했고 전체 테스트와 race 실행에서 다시 검증했다. Markdown 경로 검사와 `git diff --check`도 통과했다.

스택 `check-stack-retry-contract.sh`와 `check-stack-projection-tables.sh`는 Iris native/tools를 직접 읽는 검사다. 상위 AGENTS.md의 `Never scan Iris/ root, Iris/native/** or Iris/tools/**` 지침 때문에 실행하지 않았다. 이번 변경의 보존·replay·영수증 동작은 해당 Go/PostgreSQL 테스트로 검증했으며 교차 스택 검사 통과를 주장하지 않는다.

Fallback delta: 새 대체 처리나 재시도 경로를 추가하지 않는다.

## 게시·운영 반영

적대적 리뷰어가 관련 호출 경로·SQL 보호 조건·runtime 권한과 핵심 회귀/race 테스트를 독립 확인했으며 확정 finding은 없었다. 게시 준비 중 원격 main에 먼저 병합된 의존성 갱신 커밋 `6f6de99d1`을 fast-forward로 통합했다. 최종 게시 게이트는 통합된 revision에서 다시 실행한다.

- [ ] 검토한 변경을 커밋하고 필수 pre-push gate를 거쳐 origin/main에 게시한다.
- [ ] kapu에서 검증한 clean revision으로 API arm64 이미지를 빌드하고 revision·architecture를 확인한다.
- [ ] 중앙의 기존 API image와 배포 트리를 보존한 뒤 검증한 image·runtime bundle을 전송한다.
- [ ] migration 270을 기존 migration job으로 적용하고 `hololive-api`만 `--no-build --no-deps`로 재생성한다.
- [ ] 실행 이미지·health/readiness·오류 로그·보존 지표·실제 조회를 확인한다.

대상은 중앙 hololive-osaka의 API와 DB 함수 교체다. API 재생성 동안 bot/admin/llm 요청이 잠시 중단될 수 있다. Worker·collector·DB 컨테이너는 부분 cutover 대상에 넣지 않는다. 270은 원본 데이터 변경이 없는 함수 최적화이므로 직전 API와 호환된다. 문제 발생 시 보존한 API image로 재생성할 수 있으며, 이전 migration runner로 새 ledger를 다시 실행하지 않는다. 운영 설정 변경과 데이터 정리, rollback artifact 삭제는 이번 반영에 포함하지 않는다.
