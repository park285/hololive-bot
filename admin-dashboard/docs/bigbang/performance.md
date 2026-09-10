# 관리자 기준·후보 측정 조건

2026-09-09, T01. 측정 결과를 보기 전에 고정한 비교 조건입니다. 아래 수치는 시험 입력·허용 예산이며, 실제 실행 결과는 [진행 증거](performance-progress.md)에 기록합니다. `DEC-20260909-hololive-admin-bigbang-replacement`와 계획 V06/V10에 연결합니다.

## 동일 조건

| 항목 | 고정 조건 |
|---|---|
| 실행 위치 | kapu의 격리 환경. 같은 host·kernel·CPU 할당에서 old/candidate를 번갈아 실행. 운영 host에서 build/test 금지 |
| 아키텍처 | 성능 비교는 동일 native host architecture; 실제 arm64 image 전환 검증은 별도. QEMU 결과를 native 성능으로 해석하지 않음 |
| BFF 예산 | CPU 2개, memory limit 128 MiB, GOMAXPROCS=2, GOGC=100, GOMEMLIMIT=96MiB, 로그 설정 동일 |
| 주변 서비스 | 루프백의 격리 Valkey와 fake Holo/Docker; 운영 credential·DB·Docker Engine 제어 없음 |
| fixture | members 1,000명, rooms 100개, alarms 2,000개, live 50개, upcoming 100개, Docker 요약 9개, settings alarmAdvanceMinutes=15; ID는 문자열 `9007199254740993`을 포함하고 한글 이름 유지 |
| transport | 동일 HTTP/TLS 경로와 keep-alive 설정; fake 응답 지연 10ms; cold cache와 warm cache 결과를 분리 |
| 부하 | 보호된 members/rooms/alarms/settings/streams live/upcoming/Docker containers 읽기 7종 균등 분배, 동시 client 8개, 변경 요청 0개 |
| 워밍업 | 각 실행 60초, 이 구간의 지연은 집계에서 제외 |
| 표본 | route별 2,000개 이상이면서 전체 120초 이상; 두 조건을 모두 충족할 때 종료 |
| 반복 | old→candidate→candidate→old 순서를 3회 반복, 각 variant 6회; 실패·timeout·비정상 body를 표본에서 누락하지 않음 |
| 지연 판정 | route별 p95를 계산하고 반복 결과의 median 비교. 모든 route에서 후보≤기준×1.15, 요청 오류 0개 |
| RSS 판정 | 워밍업 후 1초 간격 process RSS, 각 실행 median 비교의 median. 후보≤기준×1.10이며 128 MiB 제한·OOM 위반 없음 |
| 초기 JS | production build, cache·service worker 없는 Chromium context, 로그인 후 `/dashboard/stats` 안정화까지 요청한 JS의 실제 전송 body bytes 합. 동일 압축·browser·network 설정으로 6회; 후보 median≤기준×1.10 |
| 장시간 관찰 | 60분, session family 4개×WS 4개=16개, 연결/종료 100주기 추가. RSS·goroutine·FD·subscription 5초 표본; 한도 밖 WS는 거부; 정리 후 마지막 5분의 resource 수준이 시작 5분 수준으로 회복되는지 기록 |

기준값이 0이거나 서로 다른 artifact/부하/환경인 결과는 비율 PASS로 판정하지 않습니다. 측정 중 종료·timeout·로그 오류는 실패로 보존하며 더 좋은 실행만 선택하지 않습니다. candidate를 바꾸면 identity와 영향받은 gate를 다시 측정합니다.

## 실행 명령과 증거 상태

T10에서 다음 실행 도구를 구현했습니다. 불변 local fixture image ID(`sha256:…`)만 받으며 각 결과에는 도구 버전·fixture hash·artifact identity·환경·오류와 원시 표본을 기록합니다. Node 24와 Go 1.27.1을 선택한 PATH로 Hololive 루트에서 실행합니다.

```bash
scripts/deploy/test-admin-bigbang-native-performance.sh <native-baseline-image> <native-candidate-image>
scripts/deploy/test-admin-bigbang-native-performance.sh <native-baseline-image> <native-candidate-image> --quick
scripts/deploy/test-admin-bigbang-js-budget.sh <native-baseline-image> <native-candidate-image>
scripts/deploy/test-admin-bigbang-resource-soak.sh <native-candidate-image>
scripts/deploy/test-admin-bigbang-resource-soak.sh <native-candidate-image> --quick
```

실행 결과의 기본 위치는 `admin-dashboard/frontend/node_modules/.cache/admin-native-performance.json`, `admin-js-budget.json`, `admin-resource-soak.json`입니다. versionable 결과와 판정은 [진행 증거](performance-progress.md)에 연결합니다. 기준·예산은 위 표를 유지하며 짧은 진단, 단위 시험 시간, 빌드 출력의 gzip 추정값을 전체 실측으로 대신하지 않습니다. 각 검사 wrapper는 호출자의 PATH를 격리 systemd unit에도 전달하여 재부팅 뒤 manager의 기본 toolchain을 선택하지 않게 합니다.

`--quick`은 사용자의 장시간 대기 축소 요청에 따른 별도 진단입니다. native 비교는 ABBA 1회, 실행마다 warmup 5초·측정 30초 이상·route별 2,000개 이상입니다. 자원 관찰은 **10분** 동안 16개 WS와 100회 재연결을 유지하되 첫 1분 뒤 4초마다 재연결하고 마지막 1분의 회복을 검사합니다. 결과는 `admin-native-performance-quick.json`, `admin-resource-soak-quick.json`에 따로 쓰며 성공해도 `PASS_DIAGNOSTIC`, `full_gate: NOT RUN`입니다. 원래 반복·시간 조건의 변경이나 출시 게이트 통과를 뜻하지 않습니다.

2026-09-10 사용자 승인에 따라 자원 `--quick`에는 측정 전 초기화 구간을 추가했습니다. 16 WS로 60초 동안 30개 상태 이력을 채우고, family별 heartbeat와 각 WS의 첫 재연결·이력 재전송을 확인한 뒤 위 10분 측정을 시작합니다. 준비 표본·16회 재연결은 측정 표본·100회에 합산하지 않습니다. 시작 1분 p95 대비 마지막 1분 median의 RSS·FD·goroutine 회복 기준과 자원 예산은 유지하며, 총 소요 시간에 초기화 시간이 더해집니다. 정규 60분 도구에는 이 변경을 적용하지 않았습니다. [변경 근거·절차](resource-recovery-review.md).

부하 도구는 8개 client 각각이 HTTP·수신 시각·JSON·UTF-8·기준 본문 대조를 소유합니다. 각 client는 검증 완료 뒤 다음 요청을 보내며 자신의 keep-alive agent를 워밍업부터 측정까지 유지합니다. 전역 순환 순서와 표본 수는 atomic counter로 맞추고 fake server는 별도 event loop에서 응답합니다. 이전의 단일 수신 event loop/검증 worker 수치와 합산하지 않으며 기준과 후보를 같은 도구로 재측정합니다. 오류·worker 종료·측정 중단과 다른 fixture의 이전 결과는 성공으로 처리하지 않습니다.

## 첫 전환·복구 리허설 상한

다음은 후보에 맞춰 완화하지 않을 격리 리허설 입력입니다. 실제 운영 허용 시간과 담당자 승인을 받은 값은 아니므로 T09에서 운영 입력과 충돌 여부를 확인해야 합니다.

| 구간 | 격리 상한 | 초과·실패 시 |
|---|---:|---|
| 관리자 fence 적용·검증 | 30초 | NO-GO; short-link 영향이면 즉시 복구 |
| 구형 in-flight 분류·drain | 120초 | 미확정 효과 unknown 유지; 성공으로 진행 금지 |
| 구형 WS·프로세스 종료 | 25초 | 기존 HTTP shutdown 20초와 별도 WS 종료 증거 비교; 강제 종료를 업무 취소 성공으로 간주하지 않음 |
| 정비 중 후보 smoke | 180초 | 관리자 정비 유지, rollback 진입 |
| 개방 후 관찰 | 300초 | generation/auth/WS/업무 오류 시 관리자 정비로 복귀 |
| 전체 rollback 준비·복구 smoke | 300초 | 정비 유지·실패 기록; 업무 요청 자동 재실행 금지 |
| 관리자 정비 총 시간 | 900초 | 출시 NO-GO; elapsed time을 성공 근거로 사용하지 않음 |

복구 artifact 누락, 세대 불일치, auth/CSRF/Origin 거부 실패, short-link 영향, 관리자 밖 세션/데이터 변경, 허용되지 않은 Docker 요청 수용은 시간 상한과 무관하게 NO-GO입니다. fake ledger 결과는 운영 receipt가 아니며 실제 운영 효과 확인을 대신하지 않습니다.
