# T10 성능·자원 회귀 수정

`DEC-20260909-hololive-admin-bigbang-replacement`, `PLN-20260909-hololive-admin-bigbang-replacement` T10/V06. 2026-09-10의 로컬 수정과 단축 검증 기록입니다. 운영 반영·전체 출시 게이트 완료를 뜻하지 않습니다. 이전 실패와 측정 조건은 [진행 기록](performance-progress.md), [동결 조건](performance.md)에 보존합니다.

이후 사용자는 RSS 회복 실패를 수용하고 적대적 리뷰·커밋·푸시·운영 반영을 승인했습니다. 아래 최적화 단계의 identity와 판정은 보존하며, 추가 I/O 오류 수정과 최신 후보·출시 상태는 [출시 실행 기록](release-progress.md)에서 관리합니다.

최종 코드는 연결 4개·읽기/쓰기 16 KiB·SDK 기본 명령 큐를 사용하는 `b9e9210a…`와 같은 소스입니다. 이 이미지의 단축 비교는 7개 route와 구형 대비 RSS 예산을 통과했고 RSS는 40.42% 작았습니다. 승인한 초기화 후 10분 검사에서도 RSS 회복은 **FAIL**입니다. 추가 큐 축소 후보는 Docker p95 예산을 넘겨 제외했습니다. [최종 소스 일치·검증 연결](retained-performance-evidence.json).

## 수정한 소유 경계

멤버·알람은 원래의 전체 JSON 전달과 달리 필드 검증과 투영이 필요합니다. 큰 목록에서 문자열 포인터를 행마다 만들고 다시 인코딩하던 비용을 줄이기 위해 strict JSON decoder로 필수 값·타입·정수 범위·UTF-8·중복 이름을 검사하고, 허용 필드만 독립적인 행 JSON으로 보관합니다. 부재·null·false·빈 문자열을 혼동하지 않습니다. 멤버 ID는 표준 int64 디코딩 후 십진 문자열로 출력하고, 별명 부재의 빈 배열 및 선택 이름의 `omitempty`를 기존 Go DTO와 대조합니다.

응답이 소유한 16 KiB 블록 안에서 행을 서로 겹치지 않게 복사합니다. 각 행의 slice capacity를 길이에 고정하고 reader·작업 버퍼를 재사용해도 이미 반환한 행은 바뀌지 않게 했습니다. 행별 할당은 고정 알람 corpus의 microbenchmark에서 약 2,021회에서 36~37회로 줄었습니다.

`JSONWriter`는 검증한 비공개 행을 HTTP 출력 버퍼에 기록하는 계약입니다. 멤버·알람 입력은 계속 strict decoder를 통과하며 느슨한 UTF-8/중복 이름 옵션을 거부합니다. 출력에서는 유효한 JSON 문자열 안의 `<`, `>`, `&`만 Unicode escape로 바꾸고 writer 오류를 전달합니다. 임의의 raw 입력을 전달하는 API가 아니며, 기본 DTO는 기존 표준 JSON 인코더를 사용합니다. 모든 출력은 완료될 때까지 버퍼에 보관하고, 실패한 일부 JSON은 클라이언트로 보내지 않습니다. 기존 인코더와의 의미·HTML escape 대조, 잘못된 입력과 미초기화 값 거부, I/O 오류 검사를 수행합니다.

공통 응답 버퍼는 재사용하되 1 MiB를 넘는 버퍼는 보관하지 않습니다. 완성한 본문의 `Content-Length`를 명시하여 구형 Holo 응답과 전송 조건을 맞췄습니다. upstream 수신 버퍼는 전체 8 MiB 제한 안에서 작은 읽기를 모으고, EOF·추가 JSON·취소·close 실패와 64 KiB drain 상한을 유지합니다.

세션 저장소와 별도 로그인 제한기의 연결당 읽기/쓰기 버퍼를 각각 16 KiB로 줄였습니다. 기존 기본값은 각각 512 KiB이며, 해당 설정은 메시지 크기 제한이 아닙니다. [고정된 valkey-go v1.0.77 정의](https://github.com/valkey-io/valkey-go/blob/v1.0.77/valkey.go#L200). 버퍼보다 큰 바이너리 포함 RESP 메시지의 왕복도 검사했습니다.

두 client의 `PipelineMultiplex=4`는 각각 16개 연결을 뜻했습니다. 연결마다 남는 버퍼·ring의 소유자를 확인한 뒤 이 값을 단일 서버의 SDK 기본값인 `2`, 각각 4개 연결로 줄였습니다. 인증·로그인 실패 예산·원자적 family 검사는 유지합니다. 32개 동시 작업의 세션/family 응답 구분과 로그인 실패 32회 증가의 누락·중복 검사를 추가했습니다. [SDK의 연결 수 정의](https://github.com/valkey-io/valkey-go/blob/v1.0.77/valkey.go#L220).

## 측정 도구 정정

공유 수신 event loop와 검증 worker를 사용하는 조건에서는 다른 client의 완료 처리와 큰 응답 처리가 짧은 요청의 수신 시각에 영향을 주었습니다. 같은 구형/후보 artifact의 진단에서 client를 분리하자 Docker p95 증가는 약 54%에서 약 4%로 달라졌습니다. 이 비교는 도구의 영향을 확인하는 근거이며 서로 다른 도구의 수치를 하나의 성능 결과로 합산하지 않습니다.

현재 도구는 8개 client가 각각 HTTP와 응답 검증을 소유합니다. 각 client는 자신의 수신 완료 시각을 먼저 기록하고 JSON·UTF-8·전체 본문 일치 검증 뒤 다음 요청을 보냅니다. agent와 연결은 워밍업부터 측정까지 유지합니다. 전역 round-robin·route별 최소 표본·오류 누락 금지·BFF CPU/메모리 예산은 유지합니다. 연결 재사용, 8개 client의 표본 분배, 잘못된 응답 보존, worker 실패와 중단 정리 검사를 통과했습니다.

native `--quick`은 ABBA 4회, 각 warmup 5초·측정 30초 이상·route별 2,000개 이상입니다. 자원 `--quick`은 10분·16 WS·100 재연결입니다. 두 도구 모두 이전 fixture의 결과나 중단된 실행을 성공으로 처리하지 않으며 성공해도 정규 12회/60분 검증을 대신하지 않습니다.

## 행 출력 개선 단계의 검증

이 단계의 소스를 정본 Dockerfile로 빌드한 native 이미지 `sha256:328471ea4fa5eec037f510927224cdb3bd2bb703599f1b21df80d27560cbce69`를 검사했습니다. 소스 SHA-256은 `38371462dd412c6ffca450d014add47dda7183ee13fc17dddddeaafc39b1f998`이며 빌드 전후 일치했습니다. [입력 목록·빌드 identity](owned-json-native-artifact.json). 바이너리 덮어쓰기 없이 실제 이미지 전체를 사용한 로컬 fixture이고 운영 배포용 arm64 봉인은 아닙니다.

전체 backend CI의 build·test·lint·NilAway·race·govulncheck와 API 계약·DTO/schema 대조·생성 재현 검사가 **PASS**입니다. govulncheck의 호출 가능한 취약점은 0입니다. 부하 client의 연결 재사용·실패·중단 회귀 시험은 **4/4 PASS**입니다.

실제 이미지의 ABBA 4회 단축 비교는 **PASS_DIAGNOSTIC**, wrapper exit 0입니다. 각 실행은 warmup 5초·측정 30초 이상·route별 2,000개 이상을 충족했습니다. 4회 합계 94,207개 응답을 검증했고 요청 오류·업무 효과·OOM·비정상 BFF 종료는 0입니다. [요약·RSS 표본](native-performance-owned-json.json), [모든 지연 원시 표본](native-performance-owned-json.json.gz).

| 지표 | 구형 | 행 출력 개선 후보 | 변화 | 판정 |
|---|---:|---:|---:|---|
| members p95 (ms) | 12.063177 | 13.134920 | +8.88% | PASS |
| rooms p95 (ms) | 11.808497 | 11.778996 | −0.25% | PASS |
| alarms p95 (ms) | 13.047168 | 14.284516 | +9.48% | PASS |
| settings p95 (ms) | 11.962355 | 11.678560 | −2.37% | PASS |
| live p95 (ms) | 11.672980 | 11.543183 | −1.11% | PASS |
| upcoming p95 (ms) | 11.585453 | 11.585919 | +0.004% | PASS |
| Docker containers p95 (ms) | 0.500682 | 0.573385 | +14.52% | PASS |
| RSS (bytes) | 62478336 | 49260544 | −21.16% | PASS |

Docker 지연은 +15% 경계에 가깝습니다. 이 짧은 결과로 정규 반복의 통과나 통계적 유의성을 주장하지 않습니다. 같은 8개 독립 client 도구에서 최종 writer 적용 전 이미지 `0459685a…`는 알람 +15.89%로 실패했으며 [실패 요약](native-performance-before-owned-writer.json)과 [원시 표본](native-performance-before-owned-writer.json.gz)을 보존했습니다.

이 `328471ea…` 이미지의 10분 회복 검사는 RSS 39,759,872→46,133,248 bytes, goroutine 102→104로 실패했고 FD와 종료 후 연결·health polling 정리는 통과했습니다. [실패 증거](resource-soak-owned-json.json). 뒤이은 로그인 제한기 버퍼 축소 단계 `28bffde7…`도 RSS 회복은 실패했습니다. [별도 증거](resource-soak-all-valkey-buffers.json). 두 결과는 이후 후보의 결과로 덮어쓰지 않습니다.

별도의 210초 진단은 실제 이미지에 같은 소스의 계측 바이너리를 겹쳐 사용했습니다. 강제 GC 후의 생존 힙 표본에서 Valkey reader/writer 약 8.4 MiB와 ring 약 7.5 MiB가 확인됐고, 별도 로그인 제한기 호출이 큰 연결 버퍼를 소유했습니다. [계측 조건·표본](owned-json-soak-profile.json), [프로파일 요약](owned-json-soak-profile.txt). 강제 GC와 OS 반환은 이 임시 계측에만 있으며 제품이나 합격 검사에는 추가하지 않았습니다. RSS와 생존 힙은 같은 값이 아니므로 이 진단을 RSS 회복 합격으로 사용하지 않습니다. [Go GC의 메모리 비용 정의](https://go.dev/doc/gc-guide#Understanding_costs).

## 4개 연결로 줄인 후보의 검증

이 단계의 native 이미지는 `sha256:b9e9210af5c840bcb784a31c91c8db6e88fb8473d817b3a436d5daf80ac0b941`이며, 소스 SHA-256 `0f92b2aeac3295dae3be863a47392d96fd1a3de520530c7bde1c4e1e28d9346a`는 빌드 전후 일치했습니다. 정본 Dockerfile로 만든 실제 이미지이며 계측 바이너리를 덮어쓰지 않았습니다. [입력 목록·빌드 identity](valkey-pool-native-artifact.json). 32개 동시성 회귀 시험을 포함한 전체 backend CI가 PASS입니다.

이 이미지의 ABBA 4회 비교는 **PASS_DIAGNOSTIC**, wrapper exit 0입니다. 총 93,945개 응답을 검증했으며 요청 오류·업무 효과·OOM·비정상 종료는 0입니다. [요약·RSS 표본](native-performance-valkey-pools.json), [지연 원시 표본](native-performance-valkey-pools.json.gz).

| 지표 | 구형 | 4개 연결 후보 | 변화 | 판정 |
|---|---:|---:|---:|---|
| members p95 (ms) | 12.138260 | 13.198655 | +8.74% | PASS |
| rooms p95 (ms) | 11.855227 | 11.765350 | −0.76% | PASS |
| alarms p95 (ms) | 13.073608 | 14.340828 | +9.69% | PASS |
| settings p95 (ms) | 11.954366 | 11.784883 | −1.42% | PASS |
| live p95 (ms) | 11.764404 | 11.580324 | −1.56% | PASS |
| upcoming p95 (ms) | 11.606920 | 11.629317 | +0.19% | PASS |
| Docker containers p95 (ms) | 0.521451 | 0.577563 | +10.76% | PASS |
| RSS (bytes) | 60901376 | 36283392 | −40.42% | PASS |

같은 이미지의 10분 회복 관찰은 **FAIL**, wrapper exit 1입니다. 600.000초·120개 표본·16 WS·100회 재연결, 오류·업무 효과 0회를 확인했습니다. RSS는 시작 1분 p95 33,390,592에서 마지막 1분 median 35,909,632 bytes로 증가했습니다. FD 34→33, goroutine 73→72, 종료 후 active stream 0개와 15초 health polling 0회는 PASS입니다. [회복 실패 원본](resource-soak-valkey-pools.json). 앞선 `c7132408…`의 최종 RSS 47.0 MB보다 작지만, 절대 사용량 감소를 회복 성공으로 해석하지 않습니다.

같은 이미지에서 재연결 없이 16 WS를 유지한 6분 대조 관찰도 완료했습니다. 첫 1분 RSS p95는 33,058,816, 마지막 1분 median은 33,820,672 bytes로, 동일한 회복 조건에는 실패합니다. FD 33→32, goroutine 71→70, 종료 후 연결·polling 정리, 오류·업무 효과 0회는 확인했습니다. [대조 조건·전체 표본](resource-soak-no-churn-control.json). `CONTROL_CAPTURED`와 wrapper exit 0은 관찰 완료를 뜻하며 회복 PASS가 아닙니다.

이는 재연결이 없어도 시작 구간 이후 RSS가 늘어날 수 있다는 근거입니다. 재연결에 의한 증가 전체가 초기화 비용이라는 뜻은 아니며, 단일 대조로 누수 부재를 입증하지 않습니다. 사용자가 승인한 [측정 시작점 변경](resource-recovery-review.md)을 짧은 진단에 적용했습니다. 최초 이력 채움·heartbeat·이력 재전송 경로를 먼저 실행하고 별도 10분 동안 같은 수치 기준을 적용합니다. 기존 실패·정규 검증의 동결 조건·PLN 상태를 그대로 유지합니다.

## 초기화 후 측정 결과

동일한 `b9e9210a…` 이미지에서 준비 62.454초, 측정 600.000초를 완료했습니다. 준비의 heartbeat 4회·재연결 16회와 측정의 재연결 100회를 별도로 확인했습니다. 준비 후의 시작 1분 p95와 마지막 1분 median을 비교한 결과는 다음과 같습니다. [전체 표본·도구 identity·실패 기록](resource-soak-warmed.json).

| 자원 | 준비 후 시작 p95 | 마지막 median | 판정 |
|---|---:|---:|---|
| RSS (bytes) | 34729984 | 36020224 | **FAIL** |
| 파일 디스크립터 | 34 | 33 | PASS |
| goroutine | 73 | 72 | PASS |

120개 측정 표본·16 WS·정상 프레임을 확인했으며 오류·업무 효과는 0입니다. 종료 후 연결 0개와 15초 동안 구독자 기반 health 요청 0회를 확인했고 wrapper exit는 1입니다. 초기화 후 측정으로 바꾸는 것만으로 RSS 회복 실패를 해소하지 못했습니다. 후보 바이너리, 수치 기준, GC·메모리 설정은 변경하지 않았습니다.

별도 계측은 같은 소스의 임시 바이너리로 준비 후 6분·30회 재연결을 관찰했습니다. 30초 간격 MemStats에서 GC 뒤 힙은 약 5 MiB였고, 프로세스 390초의 강제 GC 뒤 HeapAlloc은 5,061,536 bytes였습니다. 이어지는 진단용 OS 반환에서는 HeapReleased가 3,670,016→8,511,488 bytes로 늘었습니다. [계측 조건·표본](warmed-resource-profile.json), [생존 힙 프로파일](warmed-resource-profile.txt). GC·프로파일 자체의 비용과 마지막 강제 반환이 포함된 진단이며 실제 이미지의 회복 PASS 근거가 아닙니다.

## 연결당 명령 큐 축소 실험과 제외

진단에서 Valkey 명령 ring과 condition variable이 생존 힙 표본의 약 3.01 MiB, 68.45%를 차지했습니다. 연결당 1,024칸인 고정 큐를 SDK가 권장하는 최소 수준인 256칸(`RingScaleEachConn=8`)으로 줄인 별도 후보를 검사했습니다. 연결 4개·읽기/쓰기 16 KiB·기존 timeout·원자적 세션/family 동작은 유지했습니다. [고정 SDK의 크기·처리량 절충 정의](https://github.com/valkey-io/valkey-go/blob/v1.0.77/valkey.go#L194).

실험 native 이미지는 `sha256:8ad05f1ea714fab16c0f5200d686af60ebb85d0787c4aedc90de9464da944378`이며 소스 SHA-256은 `1c132d63e5edcad612a2d5d323d7213c876e47fd6cedc9325aa55e07b449c16c`입니다. 빌드 전후 소스가 같고, 정본 Dockerfile로 만든 실제 이미지 전체를 검사했습니다. [입력 목록·빌드 identity](valkey-ring-native-artifact.json). 320개 동시 로그인 실패 요청의 증가 누락·중복 검사를 포함한 전체 backend CI가 PASS입니다.

승인한 초기화 후 측정 방식으로 준비 62.523초와 측정 600.000초를 완료했습니다. 준비 재연결 16회와 측정 재연결 100회는 별도로 집계했습니다. 120개 측정 표본·16 WS에서 오류·업무 효과는 0이지만 RSS 회복은 **FAIL**, wrapper exit 1입니다. [전체 표본·도구 identity](resource-soak-valkey-ring.json).

| 자원 | 준비 후 시작 p95 | 마지막 median | 판정 |
|---|---:|---:|---|
| RSS (bytes) | 30982144 | 32178176 | **FAIL** |
| 파일 디스크립터 | 34 | 33 | PASS |
| goroutine | 71 | 70 | PASS |

종료 후 active stream 0개와 15초 동안 구독자 기반 health 요청 0회를 확인했습니다. 큐 축소로 절대 사용량은 낮아졌지만 시작 범위로 복귀하는 기준은 충족하지 못했습니다. 고정 큐 크기는 SDK 권장 최소 수준이며, 측정 기준·GC·OS 반환 호출을 추가로 바꾸지 않았습니다.

이 이미지의 ABBA 4회 비교도 완료했으며 94,032개 응답에서 오류·업무 효과·OOM·비정상 종료는 0입니다. 6개 Holo route와 RSS 예산은 통과했지만 Docker p95는 0.507340→0.584230ms, **+15.16%**로 +15% 예산을 넘었습니다. RSS는 62,970,880→32,118,784 bytes, −48.99%입니다. 전체 판정은 **FAIL**, wrapper exit 1입니다. [요약·RSS 표본](native-performance-valkey-ring.json), [전체 지연 원시 표본](native-performance-valkey-ring.json.gz). 경계에 가까운 단일 비교만으로 큐 축소가 지연 증가의 원인이라고 단정하지 않습니다.

추가 큐 축소는 회복 기준을 해결하지 못했고, 이 후보의 지연 비교도 통과하지 못해 최종 코드에서 제외했습니다. 두 client의 ring 설정과 해당 실험용 동시성 확대를 제거한 뒤 전체 빌드 입력의 SHA-256이 앞서 검증한 `b9e9210a…`의 `0f92b2ae…`와 정확히 같은 것을 확인했습니다. [최종 소스·검증 연결](retained-performance-evidence.json). 이 동일 소스의 전체 backend CI, 7개 route와 구형 대비 RSS 비교를 최종 근거로 유지하며, 실험 후보의 실패는 삭제하거나 합산하지 않습니다. 동일 소스에서 승인된 워밍업을 적용한 [회복 검사 실패](resource-soak-warmed.json)도 남습니다.

60분 관찰과 정규 12회 비교는 사용자의 대기 시간 축소 요청에 따라 미실행으로 유지합니다. T10/V06·AC08·AC10의 전체 출시 검증을 완료한 것으로 변경하지 않습니다.

Fallback delta: none. 입력/응답 검증 생략, 업무 재시도, 새 호환 경로, upstream·업무 DB 변경은 없습니다.
