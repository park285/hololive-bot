# 코드 감사 후속 수정

목표는 2026-10-02 코드 검토에서 재현한 결함을 수정하고, 현재 규칙과 충돌하는 검사 및 의미가 같은 중복 구현을 정리하는 것입니다. 사용자 `ㄱㄱ` 승인에 따라 구현합니다. 기존 수집기 리팩토링 변경은 보존하며, 배포·운영 DB 변경·Git 게시·의존성 업그레이드는 범위에 포함하지 않습니다.

## 구현 범위

- [x] Bot: 저장 결과 불명 추가 응답 억제, 달력 요청 취소 분리, 멤버 조회 오류 보존, accepted 정산 일치, request ID 검증 재사용과 불가능한 오류 분기 정리.
- [x] Canonical content: UTF-8 제목 절단 수정과 필드 변경 batch 실행.
- [x] Worker/delivery: 실행 전 lease 만료 방지, profile timeout 연결, grouped 포맷 오류 보존, claim cache 타입·테스트 전용 구현 정리.
- [x] Shared: limiter 표식 충돌 방지, member cache 작업 종료 소유권, 중복 SQL·typed-nil 처리 재사용.
- [x] Admin/LLM: 후보 URL·멤버 귀속 검증, 인증 오류 로그 비식별화.
- [x] Admin password reset: 별도 승인에 따라 두 공개 경로를 유지하고 부작용 없이 503 미지원 응답으로 변경.
- [x] Collector: 실제 고정 라이브러리 profile/photo/attachment/continuation 매핑 수정. 기존 변경과 분리한 함수 패치 및 실제 라이브러리 클래스 회귀 14개.
- [x] Gates: 금지된 checker self-tests·문자 marker·퇴역 이름 grep·contract hash 고정을 제거하고 제품 검사 유지.
- [x] 통합 diff 검토와 필요한 계약 문서 갱신.

각 구현 담당은 서로 다른 subtree만 수정합니다. shared member cache 종료 연결은 공용 infra cleanup과 LLM cleanup에 함께 반영합니다. package 전체 재배치는 지금 필요한 동작 수정과 구분하며, 별도 승인된 password reset 503 전환 외의 외부 route·DB 상태·payload·config 계약은 보존합니다.

## 검증

재현 입력을 정상 기대의 제품 회귀 테스트로 바꿉니다. 담당 범위 테스트·race 후 root에서 전체 대상 Go 테스트·lint·NilAway, YouTube.js test/typecheck 및 수정한 직접 게이트를 실행합니다. stack의 retry/projection/reissue/worker 계약 등 실제 변경 표면에 해당하는 검사도 실행합니다. 통과한 검사는 입력이 바뀌거나 실패·잔여 위험이 있을 때만 반복합니다.

## 경계 및 결정

- 수집기의 활성 편집은 보존했습니다. 검토 내내 변하지 않은 네 매핑 함수와 필요한 import만 scoped patch로 수정하고 별도 라이브러리 fixture 테스트를 추가했습니다. 기존 오류 처리·테스트 fixture 파일은 수정하지 않았습니다.
- 직접 호출용 password-reset API의 전달 수단 부재는 추가 `ㄱㄱ` 승인에 따라 두 경로 유지·503 미지원으로 결정했습니다. 토큰 생성·소비와 비밀번호 변경 전에 종료하고 기존 인증 오류 형식·IP 허용 목록을 유지합니다.
- 기존 canonical 멀티이미지 누락의 운영 데이터 복구는 이번 코드 수정과 별개입니다.

## 결과

- YouTube.js 전체 312개, typecheck, architecture boundary gate, stack worker contract 검사 통과.
- API·worker·collector `GOWORK=off go build -mod=readonly ./...` 통과.
- staticcheck·NilAway 통합 검사와 최종 전체 golangci-lint(0 issues), `git diff --check` 통과. 마지막 delivery 수정 뒤 해당 패키지 NilAway·staticcheck도 다시 통과했습니다.
- 전체 race 실행에서 generic delivery의 같은 poll 내 재claim 회귀를 발견했습니다. 기존 request snapshot·bounded reissue·준비 실패 테스트의 기대를 유지하고, 처리한 ID를 해당 batch 크기 안에서 제외하는 방식으로 수정했습니다. delivery 패키지 전체 `-race -count=1` 재검증 통과(14.615초).
- 같은 race 실행의 youtubejscollector 실패는 Docker reaper 연결 시간 초과였습니다. Docker API 가용성 확인 후 해당 패키지 `-race -p 1 -count=1` 재실행 통과(15.398초). 다른 제품 패키지의 race 실패나 데이터 경쟁 보고는 없었습니다.
- 모노레포 전체 suite를 실행하는 `go test -mod=readonly -count=1 ./internal/workspace` 최종 재실행 통과(102.744초). 형제 shared-go·iris-client-go 및 네 업무 모듈의 suite를 포함합니다. 최종 AP 전송 manifest 검사도 통과했습니다.
- 추가 승인된 password reset 503 전환 뒤 Admin 전체 `go test -mod=readonly -race -count=1 ./hololive/hololive-api/internal/planes/admin/...` 통과. 두 핸들러의 nil/non-nil 서비스·정상/빈/잘못된 본문 14개와 실제 라우터의 두 경로·허용/비허용 IP 4개 회귀를 포함합니다. Admin 전체 golangci-lint 0건, 기존 CI 옵션의 staticcheck·NilAway 및 diff 검사도 통과했습니다. 별도로 시도한 staticcheck `-checks=all`은 기존 한국어 주석·package comment 스타일 지적만 발생했으며, 프로젝트 CI와 같은 기본 옵션으로 재검증했습니다.
- `Fallback delta`: 저장/전송 결과 불명 뒤 추가 응답, matcher 장애의 미발견 변환, 이름 조회 장애의 기본 문구 대체, grouped 포맷 실패의 개별 발송 전환을 제거했습니다. 새 fallback·retry 원천·의존성은 추가하지 않았습니다. 기존 live catchup marker 실패 정책은 별도 예외 계약에 trigger·한도·telemetry·owner·재검토 조건을 기록했습니다.
- 운영 데이터의 기존 이미지 누락 복구, 실제 Iris 전송·운영 부하 시험, package 전체 소유권 재배치와 미측정 UNIT B 배치 최적화는 수행하지 않았습니다.

주요 새 회귀는 실제 PostgreSQL INSERT 뒤 receipt 유실, UTF-8 제목과 알림 저장, 129개 필드 변경의 batch 전체 롤백, 동일 호스트 limiter 표식 충돌, 캐시 자식 worker 종료, 방별 claim·짧은 backoff·재발급, 실제 YouTube.js 객체 매핑, LLM 출처·멤버 귀속, 운영 로거를 통과한 인증 키 비식별화를 검증합니다. 검사기를 검증하는 새 테스트나 source/doc 문자 고정 검사는 추가하지 않았습니다.

## 적대적 재검토 및 게시

사용자가 후속으로 적대적 리뷰·이슈 해소·커밋·푸시를 승인했습니다. 원격 main `c3e2ee6ac`에서 만든 `fix/code-audit-20261002` 브랜치에는 이번 감사 변경만 분리합니다. 기존 수집기 리팩터링과 원본 main의 별도 문서 커밋은 포함하지 않습니다.

- 채널 metadata의 vanity URL 전체를 handle로 저장하던 반례를 수정했습니다. 유효한 YouTube `/@handle` 주소에서만 handle을 추출하고 legacy `/user`, `/c` 주소는 handle로 사용하지 않습니다.
- LLM 검증이 표시 문자열의 쉼표를 멤버 경계로 오인해 실제 이름의 일부를 승인하던 반례를 수정했습니다. 후보 필터가 제공한 `MatchedMembers` 정본 이름으로 검증하며, 기존 콜라보 부분집합과 순서 변경을 유지합니다.
- 저장 receipt 유실을 bot 라우터 로그에서 일반 실패로 기록하던 분류를 공통 결과 불명 판정과 일치시켰습니다.
- 묶음 포맷 실패·확정 발송 실패가 공유 post claim을 먼저 해제하여 다른 방의 완료 정산을 깨뜨리던 반례를 실제 PostgreSQL에서 재현했습니다. 배치의 모든 방이 끝난 뒤 모든 소비자가 확정 실패한 claim만 해제하고, 성공·결과 불명·미정산 소비자의 증명을 보존합니다.
- 분리된 게시 대상의 YouTube.js 241개 테스트와 타입 검사, source observation 실제 DB 회귀, AP manifest, LLM·bot 로그 회귀가 통과했습니다. 공유 cache 종료는 실제 Valkey 클라이언트와 8개 동시 `Close`의 race 검증도 통과했습니다. 최종 커밋에 대해서는 기존 게시 hook을 그대로 실행합니다.
