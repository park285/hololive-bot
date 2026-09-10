# C02 · 브라우저의 POST 재전송과 BFF 변경 진입

2026-09-09, `DEC-20260909-hololive-admin-bigbang-replacement`, T06/AC04/V04. 구현 전 발견 기록이며 수정 검증 PASS가 아닙니다.

실제 Chrome `152.0.7977.82-1`, Playwright 1.63.0, 격리 HTTP/1.1 fake upstream에서 POST `/admin/api/holo/rooms`의 본문을 받고 효과를 기록한 뒤 응답 헤더 전에 연결을 닫았습니다. 앱 mutation 함수 1회와 Axios 호출 1회에도 7개 TCP 연결에서 같은 POST 효과가 기록되었습니다. 오류 후 앱 재시도는 없었습니다. 로그: `/tmp/hololive-admin-t06-replay-diagnostic.log`. 실제 production 변경은 실행하지 않았습니다.

Chromium의 [HttpNetworkTransaction::ShouldResendRequest](https://chromium.googlesource.com/chromium/src/+/master/net/http/http_network_transaction.cc)는 재사용한 연결에서 응답 헤더를 받기 전 유실된 요청의 재전송을 허용합니다. 이 링크는 현재 upstream 소스이며 설치한 빌드와 일치하는 소스라는 주장은 하지 않습니다. 판단의 직접 근거는 위 실제 브라우저 재현입니다.

정본의 16개 업무 operation에 `X-Admin-Mutation-ID` UUIDv4를 필수로 추가합니다. SDK는 논리 제출마다 ID 하나를 생성하여 브라우저 재전송에도 같은 ID가 전달되게 합니다. BFF는 auth/CSRF 다음, upstream 전 Lua 원자 선점을 수행합니다. 중복은 `409 MUTATION_ALREADY_ATTEMPTED`로 거부하고 결과는 unknown입니다. ID는 receipt나 결과 조회 API가 아니며 성공 응답을 저장·재생하지 않습니다. ASVS 2.3.1/2.3.2: 정해진 업무 진입 순서와 중복 실행 제한.

`deploy/compose/docker-compose.prod.yml`의 Valkey는 비영속 캐시이고 `allkeys-lfu`입니다. 별도 dedup 키만 소실될 수 있으므로 family lease와 사용한 ID를 하나의 hash에 저장합니다. 같은 family의 rotation/refresh는 ID를 보존합니다. hash가 사라지면 기존 token만으로 인증·refresh·rotation·mutation을 복원할 수 없습니다. 캐시 전체 재시작도 family 인증을 닫습니다. 보존 기간은 family의 실제 수명이며 절대 만료를 넘겨 인증을 연장하지 않습니다. 관리자 namespace 외 설정·자료는 바꾸지 않습니다.

검증 조건: 실제 BFF + fake upstream에서 누락/잘못된 ID 0회, 동일 ID 동시 요청/서로 다른 operation/취소 후 재요청 합계 최대 1회, store 실패 0회, rotation 뒤 중복 0회 추가, family eviction 후 인증·refresh·rotation 재생성 0회. 세 브라우저에서는 앱/Axios/HTTP 시도와 upstream 효과를 각각 기록합니다. 원래 실패를 약화하지 않고 upstream 효과 최대 1회와 사용자 unknown을 확인합니다.

Fallback delta: none. 브라우저의 자체 HTTP 재전송을 허용된 앱 재시도로 취급하지 않으며, BFF가 같은 변경 ID의 재진입을 거부합니다. 구현 및 검증 결과는 후속 기록으로 추가합니다.

## 구현 및 집중 검증

계약 세대 `e76b55e85d68c5f8f06ea44764cb31da7e4b9f952d068bcf0cc63c0a92f06ead`, 35 operations/110 validators. `session/mutation.go`와 family hash를 적용하고 family/ID fallback·legacy 정규화를 제거했습니다. `httpapi/mutation_test.go`의 실제 HTTP BFF + miniredis + fake upstream에서 POST/DELETE 동시 16건 중 upstream 효과 1회, 502 1건, 중복 409 15건을 확인했습니다. 별도 store 시험은 동시 32개 선점 중 1회, refresh/rotation 뒤 기록 보존, family 키만 제거한 뒤 인증·refresh·rotation·mutation 거부와 재생성 없음, 취소·store 실패 뒤 기존 claim 보존을 확인했습니다.

전체 Go CI는 lint/NilAway/build/test/race/govulncheck까지 통과했습니다(`/tmp/hololive-admin-t06-go-ci-final.log`). 실행 코드·import package 취약점 0건이며 기존 비도달 module 취약점 1건은 이전 공급망 기록과 같습니다. `make lint` 단독은 PATH의 staticcheck 부재로 실패했으며 canonical CI가 저장소에 고정한 도구 경로로 그 검사를 완료했습니다.

Chromium/Firefox/WebKit에서 앱 mutation/Axios 각 1회, 응답 유실·잘못된 응답 각각 upstream 효과 최대 1회와 UI unknown을 확인했습니다. BUSY·offline 후 재연결의 추가 효과는 0회입니다. 브라우저 fixture의 BFF claim 모사는 위 실제 Go BFF 시험과 분리한 증거입니다(`/tmp/hololive-admin-t06-business-browser-fixed.log`, 3/3, skip 0). `check-stack-retry-contract.sh`도 통과했습니다. 수정 전 7회 효과 기록은 그대로 보존합니다. 전체 G01~G12 및 실제 운영 전환은 아직 완료하지 않았습니다.

## C04 · 응답 유실 뒤 drain 거부의 의미

T07의 추가 Chromium 재현에서 최초 POST 효과를 기록하고 응답 연결을 닫은 직후 admission을 닫았습니다. 서로 다른 연결의 POST 2건, 업무 효과 1회였고 마지막 `503 ADMISSION_CLOSED`를 클라이언트가 rejected로 분류했습니다. 로그 `/tmp/hololive-admin-t07-drain-replay-repro.log`에 expected unknown/actual rejected가 남아 있습니다. C02는 중복 효과를 차단했지만 HTTP 거부 코드만으로 효과 부재를 판단하는 오류가 남았습니다. auth/CSRF/저장소 거부도 이전 네트워크 시도의 효과를 단독으로 부정하지 못합니다.

정본 ErrorResponse에 선택 필드 `notDispatchedMutationId`를 추가합니다. 값은 인증·CSRF 후 해당 family의 ID를 최초로 원자 선점한 요청에 한해, 공통 HTTP 오류 작성 시 upstream dispatch 표시가 없을 때만 발행합니다. 오류 payload가 스스로 넣은 값은 신뢰하지 않고 요청 context에서 덮어씁니다. 프런트는 이 값이 원래 SDK 요청의 mutation ID와 일치하는 경우에만 전송 전 거부/실패를 확정합니다. 없는 값·다른 ID·중복·drain/auth/CSRF/store 진입 거부·upstream 시도 후 실패는 unknown으로 보존합니다.

이는 유일한 최초 진입의 부정 근거이며 accepted receipt, 상태 조회 API, 저장된 결과 재생이 아닙니다. 기존 family claim 외 저장소 write를 추가하지 않습니다. ASVS 2.3.1/2.3.2와 계획의 outcome_unknown 보존을 적용합니다. 새 외부 의존성·업무 DB·운영 상태 변경은 없습니다.

필수 검증: 실제 Go BFF의 최초 claim 뒤 잘못된 body/policy 거부만 ID를 발행하며, 동일 ID의 후속 거부·선행 auth/CSRF/store/drain 거부·upstream 이후 오류에는 발행하지 않습니다. 클라이언트는 일치/누락/다른 ID를 구분합니다. 실제 브라우저의 POST 2건/효과 1회/drain 오류는 unknown이어야 합니다. 이 절은 C04 구현 전 기록이며 최종 PASS를 선언하지 않습니다.

### C04 구현·검증 결과

생성 세대 `c06bddfe3e2b69f6866e93426978bb55bfe3387510775be74e7e7394f251b118`, 35 operations/110 validators. `contract.Dispatch`에 최초 선점 ID를 연결하고, `httpx`의 공통 오류 작성이 payload의 자체 주장 대신 context의 근거만 사용하게 했습니다. 프런트는 원래 Axios 요청의 헤더 ID와 대조합니다. 전송 시도의 거부와 업무 결과를 구분하며, 안내도 “같은 변경을 자동으로 다시 실행하지 않습니다”로 맞췄습니다.

`TestOnlyFirstClaimBeforeDispatchCanProveRejection`, `TestMutationRejectionEvidenceComesOnlyFromRequestContext`와 기존 mutation/세션/race 시험이 통과했습니다. 전체 Go CI는 lint/NilAway/build/test/race/govulncheck까지 통과했습니다(`/tmp/hololive-admin-c04-go-ci.log`). 프런트 전체 173/173, Chromium/Firefox/WebKit 3/3, skip 0이며 C04의 유실 후 drain 거부도 unknown으로 보존합니다(`/tmp/hololive-admin-c04-front-test.log`). 생성 계약·route parity·독립 clean fixture 생성과 stack retry gate도 통과했습니다(`...-contract.log`, `...-stack-retry.log`). 기존 2회 POST/1회 효과/rejected 재현은 수정하지 않았습니다.
