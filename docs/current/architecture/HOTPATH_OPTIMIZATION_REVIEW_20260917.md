# Hot-path 시간복잡도 및 SQL 쓰기 재리뷰 — 2026-09-17

## 기준과 적용 범위

기준은 `main`의 `d5f54aab6ff88d3dc30122187c3e9296bbffd630`이다. 이 문서는 PR #505의 런타임 변경과 남은 검증을 설명한다. 이전 27파일 제안 패키지를 그대로 게시한 것이 아니다. 코드를 재검토해 19개 코드·테스트 파일과 본 문서로 범위를 한정했다.

운영 데이터, 인덱스, 마이그레이션, 배포 설정, CI 게이트, Go 의존성은 변경하지 않는다. 신규 production helper는 기존 소유 파일에 넣어 AP rsync 빌드 입력을 늘리지 않았다. 신규 파일은 회귀 테스트와 본 문서뿐이다.

## 1. 멤버 스냅샷 검증에서 반복 전체 탐색 제거

대상은 `hololive/hololive-shared/pkg/service/member/`의 `cache_point_ownership.go`, `channel_representative.go`, `cache_all_members.go`, `cache.go`이다.

기존 채널·이름 L1 적중은 이미 맵 조회였다. 남아 있던 낭비는 분산 캐시 응답의 소유권 검증에서 전체 멤버를 순회하는 것과, `channelMemberForPointLocked`에서 단일 멤버를 저장하면서 채널 대표 맵을 다시 구성하는 것이었다.

스냅샷에 `memberPointIndex`를 함께 게시한다. 영속 ID가 있으면 ID, 없으면 채널, 둘 다 없으면 이름이라는 기존 식별자 우선순위를 키로 표현한다. 중복 식별자를 가진 호환 데이터는 버킷 내부의 원래 순서를 유지한다. 채널 대표 선정도 기존 `preferChannelMember`를 재사용한다.

런타임은 publication 이전에 인덱스를 준비한다. 직접 생성된 테스트 스냅샷에는 `sync.Once`로 안전한 초기화를 제공한다. 실패 후 stale 스냅샷을 재사용할 때는 멤버 목록과 인덱스를 함께 재사용한다. 기존 generation/epoch 검사, 요청 이름·별칭 검증, 공유 채널 대표 규칙을 없애지 않는다.

멤버 수 M과 검증 횟수 Q에 대해 반복 전체 순회의 O(Q*M)을 구축 O(M)+평균 조회 O(Q)로 줄인다. 중복 식별자 버킷과 한 멤버의 별칭 수, 문자열 비교 비용은 별도로 남는다. 이는 스냅샷의 식별자와 목록을 불변으로 사용하는 기존 계약을 전제로 한다. 외부에 반환한 멤버 포인터의 임의 변경을 허용하는 새 계약은 도입하지 않는다.

별칭 조회의 Valkey 읽기는 `snapshotMu` 밖으로 이동한다. 응답 뒤 잠금을 다시 획득해 generation을 확인하므로 무효화 중 도착한 이전 응답은 채택하지 않는다. 공식 이름의 `EqualFold`와 명시적 별칭의 대소문자 구분을 유지한다. `GetAllAliases`의 임시 합본 슬라이스는 만들지 않고 두 원본 목록을 직접 검사한다. 전역 별칭 L1이나 negative cache를 추가한 것은 아니다.

## 2. 방송 부재 coverage의 반복 검사 제한

대상은 `internal/service/youtube/reconcile/live/apply_absence.go`와 `reduce.go`이다. 경로는 모두 `hololive/hololive-shared/` 아래에 있다.

기존 구조는 로드된 부재 구간 H, 세션 S, 구간당 채널 C에 대해 채널 포함 검사가 최악 O(H*S*C)이었다. 모든 구간에 맵을 즉시 만들면 작은 요청에서 할당 비용이 커지고 보조 메모리도 전체 이력에 비례한다.

`liveCoverageMatcher`는 처음 32회는 선형 탐색하고 반복 조회가 계속되면 맵으로 전환한다. 채널·상태 집합이 모두 8개 이하이면 선형 검사를 유지한다. 현재 순회 중인 slot의 matcher 하나만 보관한다. 따라서 채널 검사 부분을 평균 O(H*(C+S))로 제한하고, 추가 인덱스의 동시 보유량은 현재 구간 크기에 비례하도록 한다. 전체 reducer의 복제, 정렬, 상태 적용 비용까지 없어지는 것은 아니다.

slot의 시각만 캐시 키로 사용하지 않는다. 같은 scheduled_for라도 coverage가 다른 slot을 구분하기 위해 해당 reducer의 slot 객체를 사용한다. 서로 다른 reduction은 캐시를 공유하지 않는다. 빈 채널은 불허하고 빈 statuses는 모든 상태를 허용하는 기존 의미를 그대로 유지한다. positive/absence 시각, 중복 구간 집계, 종료 횟수와 grace 판정은 변경하지 않는다.

32회 기준은 작은 구성 비용을 피하기 위한 보수적 시작값이다. 운영 ARM64의 최적 임계값이나 서비스 전체의 속도 향상률을 보증하는 값은 아니다. 구성 비용을 포함하는 벤치마크를 함께 제공한다.

## 3. 전송과 인자 구성을 모두 제한한 배치 저장

대상은 `pkg/dbx/tx.go`와 `pkg/service/youtube/sourceobservation/live_persist.go`, `live_evidence.go`이다.

재리뷰에서 이전 제안은 전송을 128문장으로 나누면서도 전체 세션의 Statement/Args를 먼저 만들고 있음을 확인했다. 이번 코드는 세션을 최대 64개, pending end를 최대 128개씩 처리하고 각 묶음의 인자만 만든다. 세션당 session/head 두 문장이므로 한 세션 묶음은 최대 128문장이다. 다음 묶음의 구성 전에 context 취소도 확인한다.

`dbx.ExecStatements`는 기존 Tx 인터페이스를 확장하지 않는다. 실제 pgx 트랜잭션의 SendBatch 기능을 사용할 수 있을 때만 배치 전송하고, 그 외 구현은 기존 순차 Exec로 처리한다. 입력 순서를 보존하며 새 연결, 커밋, 롤백을 임의로 만들지 않는다. 기존 테이블에 대한 DML 전용이며 같은 배치 안에서 선행 DDL에 의존하는 사용은 계약에서 제외한다.

기존 `session A -> head A -> session B -> head B` 순서와 영상 ID 정렬을 유지한다. pending 저장 후의 scoped DELETE, absence slot 저장, 지연 외래키를 검사하는 트랜잭션 경계도 그대로다. head의 updated_at 의미는 변경하지 않는다.

BatchResults는 정상 완료, 실행 오류, panic 경로에서 닫는다. 실행 오류와 Close 오류는 함께 보존하고, 오류 이후 새 배치는 보내지 않는다. 단, 이미 서버로 전송한 현재 배치의 문장까지 애플리케이션에서 취소했다고 보장하지 않는다. 오류 시 호출자의 트랜잭션 롤백이 정합성을 책임진다. 테스트도 실제 PostgreSQL의 unique violation 뒤 전체 트랜잭션이 롤백되는지 검사한다.

세션 D, pending P의 SQL 문장 수와 행 처리량 자체는 O(D+P)로 남는다. 단위 전송마다 기다리는 왕복을 묶는 변경이지 O(1) 저장이 아니다. pending/session ID 목록과 decision 상태는 여전히 O(D+P) 메모리를 사용한다. 제한하는 것은 임시 Statement/Args 묶음의 동시 생성 크기이며 전체 누적 할당량까지 상수라고 주장하지 않는다.

## 4. 최종 저장값과 UPSERT 변경 감지의 일치

대상 쿼리는 `repository_live_session_upsert_0047_47.sql`과 `repository_live_pending_end_upsert.sql`이다.

기존 session SQL은 예약 시각을 COALESCE로 보존하고 관측 시각을 GREATEST로 보존하면서 WHERE에서는 입력값 자체를 비교했다. 이 때문에 NULL 예약 시각이나 더 오래된 관측 시각이 들어오면 최종 값이 같아도 UPDATE가 실행될 수 있었다.

WHERE도 SET과 동일한 표현식으로 계산한 값을 비교하도록 수정한다. 새로운 관측 시각, 최초 Premiere 분류, 새 종료 정보는 그대로 저장한다. pending end는 비키 컬럼 전체의 tuple IS DISTINCT FROM으로 동일 evidence의 재기록만 차단한다.

이는 불필요한 행 버전 생성 감소를 목적으로 한다. 충돌한 행의 잠금까지 없애는 변경은 아니다. 실제 발생 빈도와 WAL 감소량은 운영에서 별도로 측정해야 한다.

## 회귀 테스트와 검증

추가 테스트 함수는 28개이다.

| 영역 | 수 | 주요 검증 |
|---|---:|---|
| coverage helper | 4 | 빈 값·중복·상태·입력 소유권, 승격 전후 동등성, 작은 요청 무할당 |
| slot 연결 | 1 | 같은 시각의 다른 범위, 현재 slot 재사용, reduction 격리 |
| 멤버 인덱스 | 4 | 식별자 parity, 공유 대표, 동시 초기화, generation·Unicode, stale 재사용 |
| 별칭 원격 I/O | 1 | 원격 읽기가 무효화를 막지 않고 이전 응답을 버리는지 검사 |
| dbx 배치 | 10 | 크기·순서, fallback, 실행·Close 오류, panic 회수, 시작 전 및 배치/문장 사이 취소, nil 결과 |
| 실제 저장 호출부 | 8 | session/head 순서·인자, 두 SQL의 RowsAffected, 여러 배치 전체 롤백, 재시도, 지연 외래키, 묶음 경계 오류 |

후속 리뷰에서 한 배치 내부 실패와 여러 배치 사이의 실패를 구분했다. `statement_batch_boundary_test.go`는 첫 배치를 닫은 직후 취소하거나 순차 실행의 첫 문장 뒤 취소해 다음 작업이 시작되지 않는지 확인한다. 시간 지연에 의존하지 않고 기존 recording transaction으로 경계를 결정한다.

`live_batch_boundary_test.go`는 PostgreSQL에서 처음 128문장이 실행된 뒤 두 번째 배치에 unique violation을 일으켜 이전 배치까지 0행으로 롤백되는지 검사하고, 이후 정상 트랜잭션으로 257행을 기록한다. 별도 테스트는 128개 자식 행의 참조 대상을 다음 배치에 삽입하여 DEFERRABLE 외래키가 전송 경계가 아닌 트랜잭션 커밋에서 검사되는지 확인한다. DDL은 배치 전에 실행하며 운영 DB에 접속하지 않는다.

초기 작성 환경에서 직접 실행한 것은 동일한 coverage helper의 독립 테스트 4개와 race 검사이다(Go 1.23.2/amd64). 15개 Go 파일의 parser 검사와 gofmt 검사도 실행했다. 후속 경계 테스트 두 파일은 로컬 parser/gofmt 검사를 추가로 수행했다. parser 검사는 패키지 타입 검사의 대체물이 아니다.

후속 경계 테스트 이전 head `381ca9c56610c9528b01276196e915241a3cb4f5`의 CI run `35199113900`은 성공했다. 공유 모듈 로그에서 실제 checkout SHA, Go 1.27.1, GOWORK=off, vet, `go test -count=1 ./...`, `go test -race -p 2 -count=1 ./...` 실행을 확인했다. 성공한 테스트의 상세 출력은 기존 runner가 삭제하므로 이 로그만으로 개별 DB 테스트의 pass/skip을 단정하지 않는다.

최종 commit에 해당하는 CI 상태와 세부 로그는 PR 본문에서 갱신한다. 이전 commit의 성공을 후속 commit의 성공으로 간주하지 않는다. 로컬 full pre-push, NilAway, 운영 부하·ARM64 벤치마크는 별도 검증 사항이며 공개 PR CI의 성공으로 대체하지 않는다.

## 이번 PR에서 제외한 변경

현재 관측 구간 전용 SQL, migration 198의 scheduled_for 인덱스, 관련 schema golden 수정은 포함하지 않는다. 기존 채널 GIN 인덱스와 실제 데이터 분포를 기준으로 EXPLAIN을 비교하고 migration 재생으로 golden을 생성한 뒤 별도 변경으로 다룬다. 전체 golden을 추정값으로 교체하거나 검증 없이 자동 인덱스 생성을 추가하지 않는다.

registry 증분 관리와 due heap, UNIT B 구독 preload, 전역 별칭 L1/negative cache, 이름 표현식 인덱스도 별도 상태·신선도·실행계획 검증이 필요한 후속 범위다. 따라서 최초 분석의 모든 후보가 해결되었다고 표현하지 않는다.

## 적용 및 롤백

본 PR 자체는 실행 코드만 바꾸며 운영 배포나 DB 마이그레이션을 수행하지 않는다. 기존 CI와 배포 전 검증을 통과한 후 일반 코드 배포 절차를 따른다. 롤백은 코드 commit을 되돌리는 방식이며 새 스키마의 역변환은 필요 없다. lease·epoch 검사, 잠금 순서, 오류 처리, CI gate를 성능 최적화 명목으로 제거하지 않는다.
