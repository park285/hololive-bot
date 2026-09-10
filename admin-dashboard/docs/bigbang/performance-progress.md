# T10 성능 검증 진행 증거

`DEC-20260909-hololive-admin-bigbang-replacement`, `PLN-20260909-hololive-admin-bigbang-replacement` T10. 이 문서는 초기 JS 최적화와 이전 후보의 실패를 보존한 이력입니다. 최신 구현·artifact·검증 결과는 [성능·자원 회귀 수정](performance-resolution.md)에서 확인합니다. 최종 유지한 `b9e9210a…`의 ABBA 단축 비교는 7개 route와 RSS 모두 **PASS_DIAGNOSTIC**입니다. 추가 큐 축소 실험은 Docker p95 예산 초과로 제외했고, 승인한 초기화 후 검사에서도 최종 후보의 RSS 회복은 **FAIL**입니다. 정규 12회 비교와 60분 관찰은 미실행이며 전체 출시는 **NO-GO**입니다. 운영 반영·commit·push·출시 인수 완료를 뜻하지 않습니다.

## 고정 조건과 관찰

[측정 조건](performance.md)의 native CPU 2개/128 MiB, 기존→후보→후보→기존 3회, 브라우저·압축·로그인/통계 경로와 +10% 예산을 유지했습니다. 구형은 [실제 구형 자산](old-assets.json)을 포함한 [native baseline](native-baseline.json)입니다. 동일 fixture topology를 먼저 만든 뒤 각 표본마다 새 Chromium browser/context로 `/login`부터 `/dashboard/stats`까지 실제 JS body 전송량을 CDP `Network.dataReceived.encodedDataLength`로 합산합니다. cache/service worker는 사용하지 않습니다.

| 후보 | 기존 median | 후보 median | 비율 | 결과/증거 |
|---|---:|---:|---:|---|
| 단일 validator module | 508318 | 959449 | 1.8874975901 | FAIL, [원본](js-budget-before-split.json) |
| 기능별 module | 508318 | 662326 | 1.3029756963 | FAIL, [분리 후](js-budget-after-group-split.json) |
| 공유 boolean 검증기·계약 선언 | 508318 | 577926 | 1.1369379011 | FAIL, [공유 후](js-budget-after-assertions.json) |
| 공통 envelope·최초 경고 UI 지연 로딩 | 508318 | 557805 | 1.0973544120 | PASS, [전체 12회](js-budget-local-fixture.json) |

각 비교는 variant별 6회이며 모든 표본에서 같은 전송량을 관찰했습니다. UI·CSP 오류 및 업무 효과는 0이었습니다. 실패한 결과를 삭제하거나 기준을 완화하지 않습니다. 새로운 후보의 측정 전에는 위 결과를 새 identity에 재사용하지 않습니다.

## C07 · 전송 전에 준비하는 기능별 검증기

초기 실측 초과를 해결하기 위해 정본의 request/response 검증기를 기능별로 생성하고 인증 operation은 개별 module로 만듭니다. 같은 스키마의 assertion과 모든 operation에 공통인 response 선언은 공유합니다. `operation-contracts.json`은 검사 inventory이고 runtime의 생성 TypeScript는 같은 값이며 deep equality로 대조합니다.

transport는 해당 operation의 입력과 모든 응답 validator를 HTTP 전에 함께 준비합니다. 준비·HTTP가 기존 timeout 예산을 공유하고, 준비 실패·취소·세션 변경이면 HTTP를 보내지 않습니다. CSRF는 준비 후 현재 값을 사용합니다. 업무 잠금은 준비부터 결과 분류까지 유지하며 전송 후 검증 코드 다운로드·자동 재시도는 없습니다. WS 검증기는 해당 관측 component가 동기 import합니다.

boolean 전용 생성에서는 외부/순환/sibling ref를 허용하지 않는 현재 계약에 따라 local ref를 전개합니다. JSON Schema의 `not(not(schema))`는 판정을 유지하면서 UI가 소비하지 않는 오류 상세의 생성을 줄입니다. int32/int64는 pinned ajv-formats 3.0.1의 판정과 같은 `integer` 및 int32 범위로 표현합니다. coercion/default/removal은 모두 false이며 컴파일러·eval·Function·미해결 require는 production에 없습니다. 영어 오류 상세를 숨기는 UI 변경은 없으며 기존 소유 오류 문구를 사용합니다.

원본 schema-bundle을 직접 컴파일한 Ajv와 생성 assertion을 Go DTO·정상 envelope·큰 ID·한글·int32 경계·비정상 값/누락/추가 필드에서 대조했습니다. **110 schema, 814440 comparisons, 7537 valid fixtures, 판정 차이·입력 변경 0**입니다. compiler는 이 비교용 자식 프로세스에만 있으며 일반 standalone 시험은 동적 코드 생성 금지 상태로 실행합니다. 생성 2회 byte 재현, 계약·lint·production build가 통과했습니다.

기능별 분리 단계의 전체 프런트 시험은 **175/175, skip 0**, Chromium/Firefox/WebKit **3/3** 통과했습니다. 실제 module 요청의 지연·실패·취소·세션 변경 뒤 업무 전송 0회를 각 엔진에서 검사했습니다. 이후 assertion·envelope 최적화 및 첫 경고 UI 로딩을 포함한 전체 회귀 시험도 **175/175, skip 0, 세 브라우저 3/3** 통과했습니다. 경고 module 지연 중 표시, 로딩 실패 표시, 세션 변경 뒤 이전 dialog 제거, 최초 focus·연장·후속 경고 재사용을 검사했습니다.

## 최초 전체 부하 비교와 응답 인코딩 개선

native 기준 image `ffe386f4…`와 기존 후보 `9676dfeb…`를 동결 조건 그대로 12회 실행했습니다. 모든 표본은 120초 이상·route별 2,000개 이상이며 요청 오류·업무 변경·OOM·비정상 종료는 0입니다. [요약과 RSS 표본](native-performance-before-buffer-fix.json), [전체 지연 원시 표본](native-performance-before-buffer-fix.json.gz)을 보존합니다.

| 지표 | 기준 | 기존 후보 | 비율 | 판정 |
|---|---:|---:|---:|---|
| members p95 (ms) | 17.2910 | 17.4749 | 1.0106 | PASS |
| rooms p95 (ms) | 17.8145 | 16.3103 | 0.9156 | PASS |
| alarms p95 (ms) | 18.6964 | 19.2762 | 1.0310 | PASS |
| settings p95 (ms) | 17.3282 | 16.9672 | 0.9792 | PASS |
| live p95 (ms) | 18.6400 | 16.7561 | 0.8989 | PASS |
| upcoming p95 (ms) | 17.4153 | 16.5733 | 0.9516 | PASS |
| Docker containers p95 (ms) | 2.4318 | 4.2350 | 1.7415 | **FAIL** |
| RSS (bytes) | 63569920 | 69957632 | 1.100483 | **FAIL** |

kapu의 별도 35초 계측에서 응답 JSON의 `MarshalWrite`와 임시 `bytes.Buffer` 확장이 할당량의 약 44%를 차지했습니다. 2,000개 알람 인코딩 진단은 622205 ns/1048933 bytes/20 allocations에서 표준 `jsonv2.Marshal`의 486393 ns/237925 bytes/3 allocations로 줄었습니다. 관리자 `httpx`가 JSON 응답을 한 번에 인코딩하도록 변경하고 HTML 이스케이프·UTF-8 거부·기존 Content-Type·본문 없는 상태 코드·쓰기 실패 계약을 보존했습니다. 공용 라이브러리·upstream·스키마·인증 정책은 변경하지 않았습니다.

같은 짧은 계측의 전체 할당량은 12.37GB에서 9.32GB로 줄었습니다. 추가 수신 버퍼 변경은 개선을 입증하지 못해 최종 코드에서 제외했습니다. 이 진단은 워밍업·표본·반복 조건이 다른 원인 분석이며 V06/G10 PASS 근거가 아닙니다. 이 단계의 native 후보는 `b703d266…`, source input fingerprint는 `4148d797…`입니다. 전체 backend CI(build/test/race/lint/NilAway/govulncheck)를 통과했고 도달 가능한 취약점은 0입니다.

2026-09-10 08:14:56 KST 호스트 재부팅으로 첫 짧은 비교가 중단됐습니다. 완료되지 않은 표본은 판정에 사용하지 않습니다. 재부팅 뒤 user systemd의 기본 PATH가 Node 22를 선택한 문제도 확인했으며, 격리 검사 wrapper는 호출자가 선택한 PATH를 자식 unit에 명시적으로 전달합니다. 장시간 실행은 시간 상한·`KillMode=control-group`·소유 label 정리가 있는 임시 서비스로 관리합니다.

## 이전 후보 c7132408과 축소한 비교

Holo 성공 응답은 8 MiB 상한 안에서 `jsonv2.UnmarshalRead`로 DTO에 직접 읽습니다. 마지막 공백·추가 JSON도 상한에 포함하며 잘못된 MIME/status·JSON은 거부합니다. 모든 경로에서 body를 닫고 실패 시 drain을 64 KiB로 제한합니다. JSON 오류의 입력값은 숨기고 I/O·close 원인은 보존합니다. 0이 유효하지 않은 upstream member ID는 값 필드로 읽고 기존 양수 검사를 유지합니다. 알람 포인터를 제거한 수동 decoder 실험은 전체 진단에서 개선을 입증하지 못해 제거했습니다.

이 단계의 native fixture image는 `sha256:c7132408cdb09c0d7daafac78a6d61448aed15669fa4a249c16f10921adf1172`, source input fingerprint는 `63887ae839596888b1cfceab809ab62b8cfb9b2bb6f2f53e0f8ef9778b08af7e`입니다. revision `unknown`인 로컬 시험 artifact이며 배포 후보 봉인이 아닙니다. 이 소스의 전체 backend CI와 route·DTO·validator·생성 2회 재현 검사가 통과했습니다(`/tmp/hololive-admin-t10-stream-go-ci.log`, `/tmp/hololive-admin-t10-stream-contract.log`).

큰 응답의 `JSON.parse`와 deep equality가 같은 event loop의 다른 응답 시각을 늦추는 계측 간섭을 확인해 모든 응답 대조를 전용 worker로 옮겼습니다. client별 검증 대기·8개 동시성·본문 전체 검증·오류 집계는 유지합니다. 큰 ID/null/누락/추가 필드/잘못된 UTF-8/worker 종료 시험 3개가 통과했습니다. 이 도구로 baseline과 현재 후보를 함께 다시 측정했으며 이전 도구의 지연 수치와 합산하지 않습니다.

사용자의 장시간 대기 축소 요청에 따라 첫 ABBA 1회, **완료 표본 4/12** 뒤 실행을 중단했습니다. 완료된 각 표본은 warmup 60초·측정 120초 이상·route별 2,000개 이상을 충족했고 오류·업무 효과·OOM은 0, 네 BFF의 정상 종료를 확인했습니다. 다섯 번째 warmup은 중단했으며 판정에서 제외했습니다. wrapper 종료 코드 0은 정리 결과이고 12회 시험 성공을 뜻하지 않습니다. [요약](native-performance-shortened.json)과 [중단 시점의 원시 기록](native-performance-shortened.json.gz)을 보존합니다.

| 지표 | 기준 | 현재 후보 | 증가율 | 진단 결과 |
|---|---:|---:|---:|---|
| members p95 (ms) | 12.5215 | 14.7660 | 17.92% | **FAIL** |
| rooms p95 (ms) | 12.0899 | 12.9388 | 7.02% | 예산 이내 |
| alarms p95 (ms) | 12.6508 | 16.0520 | 26.88% | **FAIL** |
| settings p95 (ms) | 12.1711 | 12.2986 | 1.05% | 예산 이내 |
| live p95 (ms) | 12.1457 | 12.4365 | 2.39% | 예산 이내 |
| upcoming p95 (ms) | 12.0602 | 12.6161 | 4.61% | 예산 이내 |
| Docker containers p95 (ms) | 0.8608 | 1.3872 | 61.15% | **FAIL** |
| RSS (bytes) | 62373888 | 67395584 | 8.05% | 예산 이내 |

사용자가 허용한 별도 AI의 읽기 전용 검토도 수행했습니다. 응답 합치기 뒤 worker 전달용 복사가 남아 있고 fake upstream의 10ms timer도 부하 도구의 event loop를 공유한다는 점을 확인했습니다. 검토 시점에는 현재 후보의 대응 프로파일이 없어 Docker 자체 병목으로 단정하지 않았습니다. 이 검토는 G12 출시 인수를 대신하지 않습니다.

이후 같은 소스의 임시 Go overlay로 [35초 프로파일](profile-streaming-diagnostic.json)을 수집했습니다. JSON struct 디코딩 누적 CPU는 55.87%, pointer 디코딩은 36.07%이며 서로 중첩됩니다. GC 후 heap 표본은 도구 표시 기준 24.79 MB이고 Valkey 연결의 누적 비중은 91.93%입니다. 연결 설정은 기존 native baseline과 같으므로 이번 회귀의 원인으로 단정하거나 임의로 축소하지 않았습니다. 이 계측은 CPU 비용·남은 heap의 소유자를 좁히는 진단이며, 타이밍 오버헤드와 다른 요청 수가 있어 정규 p95 비교나 정규화한 총 할당 개선율로 사용하지 않습니다.

## 10분 연결 유지 진단

같은 `c7132408…` image로 `test-admin-bigbang-resource-soak.sh <image> --quick`을 실행했습니다. **600.001초, 120개 자원 표본, 16개 WS, 재연결 100회**를 완료했습니다. 연결·프레임 오류와 업무 효과는 0이며, family의 다섯 번째 연결과 process의 열일곱 번째 연결은 모두 429로 거부했습니다. 모든 연결 종료 뒤 active stream 0개, 15초 동안 구독자 기반 upstream health 요청 0회를 확인했습니다. [실행 증거](resource-soak-quick-local-fixture.json).

| 자원 | 첫 1분 p95 | 마지막 1분 median | 판정 |
|---|---:|---:|---|
| RSS (bytes) | 40583168 | 47005696 | **FAIL** |
| 파일 디스크립터 | 49 | 49 | PASS |
| goroutine | 104 | 104 | PASS |

메모리 회복 기준을 충족하지 못해 진단 전체는 **FAIL**이며 wrapper exit는 1입니다. 종료 뒤 task 소유 fixture를 정리했습니다. 후반 RSS는 약 47.0 MB로 평탄했지만, 이를 누수 부재나 60분 회복의 근거로 사용하지 않습니다. **60분 장기 검증은 NOT RUN**입니다.

응답 worker의 중복 복사는 전용 `Uint8Array` 하나의 소유권을 이전하도록 수정했습니다. 수신 종료 시각은 본문 합치기 전에 기록하며 모든 응답 검증과 client별 대기를 유지합니다. Buffer pool·부분 view의 잘못된 이전 거부와 원본 detach를 포함해 관련 시험 **4/4**가 통과했습니다. 위 ABBA 표본은 이 추가 수정 이전의 worker 도구이며 이후 결과와 합산하지 않습니다. 수동 systemd unit 중단이 0으로 반환된 관찰을 반영해 native wrapper도 실제 12회 PASS 기록을 확인해야 성공하도록 보완했습니다.

같은 `c7132408…` image에서 기존/개선 측정 도구를 ABBA 순서로 4회 비교했습니다(각 warmup 5초·측정 30초 이상·route별 2,000개 이상). 모든 route의 p95 변화는 약 ±0.3% 이내였고 Docker는 1.3829→1.3837ms였습니다. 오류는 0, 네 BFF의 정상 종료와 fixture 정리를 확인했습니다. [도구 비교 요약](driver-timing-diagnostic.json)과 [원시 표본](driver-timing-diagnostic.json.gz). 이는 **같은 앱의 도구 비교**이며 기존 앱 대비 성능 실패를 해결했다는 결과가 아닙니다. 중단된 실제 측정 기록을 새 wrapper의 완료 검사에 넣었을 때 정상적으로 거부하는 것도 확인했습니다.

## 남은 실행

- 최종 native 후보의 단축 비교에서 3개 route의 p95 초과를 해결했습니다. [최신 결과](performance-resolution.md)는 위 이전 도구·후보의 수치와 합산하지 않습니다. 정규 12회 비교는 사용자의 시간 축소 요청으로 미실행이며 단축 결과를 정규 PASS로 기록하지 않습니다.
- `scripts/deploy/test-admin-bigbang-resource-soak.sh <candidate-image>`의 60분 검증은 미실행으로 남깁니다. 이전 `c7132408` 후보의 10분 RSS 회복 FAIL은 보존하고 최종 후보의 단축 검증을 [최신 결과](performance-resolution.md)에 연결합니다.
- 장시간 관찰의 측정 전 판정 세부: 처음 5분의 16개 연결 안정 구간과 100주기 후 마지막 5분의 16개 연결 구간을 비교합니다. 각 자원의 마지막 구간 median이 시작 구간 p95 범위 안으로 돌아오는지 검사합니다. 종료 후 연결 0개와 구독자 기반 health polling 0회를 별도로 검사합니다. 실제 WS의 관측 수와 내부 subscription 개수의 직접 계측은 구분하며, 내부 소유권 검사는 해당 Go 시험으로 보완합니다.
- 최종 native/arm64 image 재봉인, 초기 JS 재측정, 영향받는 계약·브라우저·bundle/image·전환 시험을 같은 최종 후보에 연결해야 합니다.
- 실제 old/new bundle 양방향 production 브라우저는 이전 후보에서 [24개 case를 통과](browser-progress.md)했습니다. 새 후보의 영향받는 검사를 다시 연결해야 합니다. G07 O02는 [운영 재확인](operational-boundary-refresh.json)에서도 source 제한 unit이 inactive이고 nft table이 없었습니다. G12 독립 검증 기록 작성자/출시 인수는 아직 충족되지 않았습니다.

PLN의 T10은 `in_progress`, V06은 `failed`, AC08·AC10은 `not_met`로 기록했습니다. T11은 `pending`이며 DEC의 출시 완료를 주장하지 않습니다. 60분 실행과 나머지 ABBA 반복은 사용자의 시간 축소 요청으로 유보했고, 운영 변경·commit·push는 수행하지 않았습니다.

Fallback delta: none. 검증 실패는 전송 전 거부이며 실패한 검증을 생략하거나 다른 경로로 실행하지 않습니다.
