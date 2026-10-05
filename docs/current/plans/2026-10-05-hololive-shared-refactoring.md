# hololive-shared 리팩토링

## 목표와 제약

두 차례 조사에서 확인한 템플릿 종료 결함, 실행 예산, 구독자 조회, telemetry 저장, 멤버 조회 계약, delivery·설정 소유권, canonical JSON 비용, cache rebuild 비용을 개선한다. 초기 구현은 로컬 변경으로 한정했으며, 후속 인수 요청에서 적대적 검토·수정·commit·push·라이브 반영까지 승인받았다. 환경변수 값, canonical byte/hash, 구독 정합성, 발송 lease·결과 불명 처리, 트랜잭션 원자성을 유지한다.

## 작업 분할

- 템플릿: 정수·float 경계, 취소·출력·계산 예산, batch resolve 및 실제 소비처.
- 구독자·cache: empty marker batch, resolver별 singleflight, namespace scan 메모리 상한.
- Telemetry·SQL: typed DB, 고정 배열 인자 조회, batch UPDATE, placeholder 변환 계층 정리.
- 멤버: context·오류 반환, snapshot 검색과 상태 소유권.
- Delivery: maintenance 수명 분리, worker 전용 실행·저장 코드 소유권.
- 설정: 런타임 조립과 shared 옵션·provider 경계.
- Canonical JSON: 중간 트리·반복 순회 축소, byte/hash 계약 보존.
- 통합 담당: 겹치는 runtime 소비처, 문서, 독립 검토 및 foreground 검증.

구현 에이전트는 맡은 파일만 변경하고 build/lint/test/formatter는 실행하지 않는다. 공통 소비처 변경은 통합 담당에게 인계한다.

## 기존 재현 근거

- Preview의 formatNumber/formatNumberKR에 MinInt64 또는 float 2^63: 128 KiB 스택 제한 자식 프로세스에서 stack overflow, 종료 코드 2.
- 취소된 context의 Preview에서 정수 range가 10,000 bytes를 생성하고 성공.
- 구독 singleflight에 다른 DB A/B를 전달하면 A 조회 1회, B 조회 0회이며 B가 A 오류 수신.
- Telemetry 분류 100개: SELECT 1회와 직렬 UPDATE 100회. UPDATE당 1ms 주입 시 약 107ms.
- 잘못된 타입의 telemetry DB 생성은 통과하고 FetchAndLockPending에서 nil panic.
- Empty marker 100채널: EXISTS 100회, 호출당 2ms 주입 시 약 222ms.
- CanonicalizeJSON 1 MiB: 기존 benchmark 약 8.38–12.78ms/op, 총 할당 약 4 MiB/op.

## 검증

수정 후 같은 공개 경로의 로컬 smoke, 관련 회귀·DB 통합·race 검사, shared/API/worker/collector build와 적용되는 architecture gate를 실행한다. 기존 정합성·실패 경로 테스트를 유지하고 새 테스트는 경계·취소·소유권·원자성 같은 소비자 관찰 가능 동작만 검증한다.

### 실행 결과

- 네 Go 모듈 전체 `go test -p 2 ... -count=1`: 185개 package 통과, 24개는 test 없음. 이후 수정된 동작은 해당 package의 race·정적 검사로 다시 확인했다.
- 네 모듈 `go build` 및 각 모듈 `GOWORK=off go vet ./...`: 통과. 고정 패치 staticcheck 통과.
- member·cache·alarm·subscriptions·notificationdelivery 및 membernews 소비처 race 검사 통과. panic이 있는 공유 적재는 대기자 모두에게 오류를 반환하고 stale 성공으로 바뀌지 않는 회귀 포함.
- 공개 `Preview` smoke: MinInt64·float 2^63 종료, 사전 취소 오류, 출력 없는 100만 회 loop가 100,000 단계에서 종료. 반복 인자·컨테이너 padding·dict DAG·UTF-8 확장·미사용 인자·잘못된 `*` 형식의 일곱 할당 probe 모두 부분 결과 없이 거부한다. 반복 인자 probe 총 할당은 약 41.8MB에서 126KB, 미사용 인자 probe는 약 21.1MB에서 136KB로 감소했다.
- 실제 miniredis smoke: 100개 empty marker를 `DoMulti` 한 번으로 조회하고 100개 key를 page iterator로 순회했다. 서로 다른 resolver의 DB A/B는 각각 한 번 조회하고 자기 오류를 받았다. typed-nil telemetry DB는 생성 오류다.
- dispatcher 공개 `Run` smoke: 유지보수 dependency panic을 오류로 반환하고 sibling loop를 join했다(약 149µs). 외부 발송은 실행하지 않았다.
- Canonical JSON 1MiB benchmark: 0.892ms/op, 1,049,084B/op, 10allocs/op. 기존 8.38–12.78ms·약 4MiB와 비교해 byte/hash 계약을 유지하며 중간 할당을 줄였다. 측정값은 로컬 단일 실행 결과다.
- architecture boundary/SQL ownership gate, AP 전송 manifest, workspace DB access policy, sensitive-log 검사 통과. PostgreSQL capacity는 60슬롯 중 앱 55·superuser 예약 3·잔여 2로 통과했다.
- YouTube publish/consume performance gate: 약 9.71ms/op, 36개 관측의 투영 cycle 350ms(예산 3.6초)로 통과했다.
- 최종 NilAway 네 모듈 검사 통과. template·YouTube dispatch 전체·canonical JSON·telemetry race 검사 통과.
- `local-ci.sh --integration-tests-only`: 격리 PostgreSQL/Valkey로 dispatchoutbox integration-tag, YouTube dispatch·joblease·LLM summarizer 그룹 통과. 생성한 컨테이너는 제거했다.
- 최종 네 모듈 `golangci-lint run`: **0 issues**.

### 보존 경계

- Fallback delta: 새 fallback 없음. 기존 stale member snapshot·구독 DB read-through 계약을 유지한다. 새 panic·취소·template 예산 초과는 성공으로 바꾸지 않는다.
- runtime env 값·형식 거부, profile JSON, native PostgreSQL query의 트랜잭션 경계, notification lease/attempt/outcome_unknown 처리를 유지했다.
- meta retry/projection/reissue/worker 검사기는 `Iris/native` 또는 `Iris/tools`를 읽으므로 workspace의 해당 경로 스캔 금지 지침에 따라 실행하지 않는다. 관련 Hololive DB·발송·profile 동작은 로컬 Go 검사로 확인한다.
- 기존 사용자 변경을 보존한다. commit·push·배포·운영 데이터 접근은 하지 않는다. delivery 이동 중 생긴 index staging은 해제 명령의 도구 정책 제한으로 남아 있으며, 파일 내용은 작업 트리에 보존된다.

## 상태

구현·독립 검토·통합 수정·동작 검증·문서 갱신 완료. Context wrapper 오탐 한 건은 테스트용 취소 관찰 wrapper의 해당 필드에만 이유를 명시했고, 실제 동시 대기·취소 회귀 검사를 유지했다. 나머지 lint 지적은 코드 수정으로 해결했다. 로컬 smoke 파일은 제거했다. 신규 runtime 의존성은 추가하지 않았다.

## 후속 인수 검토

- `23f6497ecdb977157f817d8622dc10a7eff88e48` 배포본에 후속 변경을 통합했다. 이미 게시·적용한 migration 264·265와 manifest·schema golden을 보존했고, worker로 옮긴 failure policy의 shared 잔여 복사본을 제거했다. 신규 migration은 없다.
- 템플릿 할당 사전 검사에서 이름 있는 정수를 놓치는 결함을 재현했다. `time.Duration`, 이름 있는 `uint64`·`uintptr`를 너비 1,000,000으로 전달하고 16개 원소를 출력하면 오류 반환 전에 약 89MB를 할당했다. `fmt`와 같은 정수 Kind를 확인하도록 수정하고 2MiB 할당 상한 회귀 및 정상 출력 비교를 추가했다.
- 변경에 포함된 catalog SQL 문자열 전용 검사는 stack 지침에 따라 제거했다. 운영 catalog 조회에 세 테이블을 추가한 변경은 보존했다.
- 통합본에서 canonical JSON·template·cache·alarm·member·officialidentity·delivery·notificationdelivery·API/worker 설정 race 검사를 통과했다. 격리 PostgreSQL/Valkey의 canonicalwrite·YouTube dispatch/store·telemetry·tracking·subscriptions·membernews 통합 race 검사도 통과했다.
- 인수 이후 publication·배포는 후속 사용자 승인 범위다. 위 초기 구현 단계의 게시·배포 제한 기록은 현재 승인 상태를 뜻하지 않는다. 필수 meta 검증에 한정한 Iris 경로 읽기도 이전 승인을 재사용한다. Fallback delta: 없음.
