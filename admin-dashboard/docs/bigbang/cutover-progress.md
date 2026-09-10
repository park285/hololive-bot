# T09 첫 구형 전환·복구 리허설 진행

`DEC-20260909-hololive-admin-bigbang-replacement`. 로컬 fixture만 실행했으며 실제 운영 전환이나 출시 gate 완료를 주장하지 않습니다. BFF는 로컬 시험 image, 구형 BFF는 중앙 runtime 조사와 일치하는 실제 arm64 image를 private QEMU로 실행합니다. Nginx·socket-proxy·Valkey도 고정 image를 사용합니다.

## C06: Nginx 정비 응답과 실제 admission 경계

첫 리허설에서 새 연결의 `/health`는 개방 후 200이었지만 기존 HTTP 연결의 로그인은 정비 worker에서 HTML 503을 받았습니다. 반대로 fence 직후에도 기존 client를 처리하던 이전 worker가 새 POST를 구형 BFF로 전달하는 경우가 재현되었습니다. 최초 failure는 `/tmp/hololive-admin-t09-cutover.log`에 관찰했고, 변경 전 결과는 세션 도구 출력에도 남아 있습니다.

[Nginx의 HUP 계약](https://nginx.org/en/docs/control.html#reconfiguration)에 따라 이전 worker는 기존 client의 처리를 마친 뒤 종료합니다. 새 연결의 503 하나로 기존 HTTP·WS·직접 BFF 경로가 모두 차단되었다고 판단할 수 없습니다.

첫 전환은 ingress를 정비로 바꾼 뒤 `docker stop --time 25`로 구형 BFF 종료를 시작하고, 구형 listener가 새 연결을 받지 않는 것을 확인하여 admission 경계를 확정합니다. 진행 중인 요청은 이때 별도로 유지·분류하고 종료 완료를 기다립니다. 새로운 작업은 upstream에 0회 도착해야 합니다. 구형 BFF에 새 admission API가 있다고 가정하거나 구형 image를 미리 패치하지 않습니다.

다시 열 때는 HUP 이전 Nginx worker PID가 종료된 것을 먼저 확인하고 새 연결의 200 및 short-link health를 확인합니다. 공유 ingress container를 정지·재생성하거나 기존 client 요청을 재전송하지 않습니다.

## 도구

- `admin-dashboard-maintenance.sh fence|open <ingress-id> <config>`: 정확한 container/config mount와 소유권을 확인합니다. `.maintenance` marker를 owning renderer가 계속 반영하고, admin 30191만 503으로 닫습니다. 30192 short-link listener는 유지합니다.
- `purge-admin-dashboard-sessions.sh <valkey-id> <stopped-bff-id> <fenced-ingress-id>`: BFF 종료·정비 marker·503을 확인한 뒤 `session:admin:*`만 bounded SCAN/UNLINK합니다. family의 mutation claim도 함께 폐기합니다. Valkey의 기존 설정을 container 안에서 인증에만 사용하며 키 값이나 password를 출력하지 않습니다. FLUSHDB/FLUSHALL과 공유 snapshot 복원은 없습니다.
- `test-admin-bigbang-cutover.sh <immutable-local-fixture-id>`: 실제 구형 요청/WS, 원자 변경 효과 ledger, 정비·종료·세션 폐기·새 secret·신형 smoke·전체 rollback을 실행합니다. fixture별 internal network, 고유 label, transient cgroup, EXIT cleanup을 사용합니다.

## 확인된 정상 경로

최초 구형 전환 → 전체 rollback의 5개 phase가 통과했습니다. 구형 변경의 client 응답은 500으로 불명 상태를 유지했고 fake owner의 효과는 정확히 1회였습니다. 종료 뒤 direct 요청·구형 WS가 차단되었고, 관리자 key 3개(첫 전환)/2개(rollback)를 지우면서 `session:twentyq:*` 및 업무 sentinel 값을 보존했습니다. 매 전환에 새 signing secret을 사용하고 이전 양쪽 cookie가 모두 거부되는 것을 확인했습니다. shared ingress의 StartedAt은 유지되었습니다.

최초 측정은 ingress fence 298ms, 구형 drain·직접 거부 확인 12,229ms, session purge·신형 준비 1,056ms, 신형 open/smoke 383ms, 전체 rollback 2,078ms입니다. 이 수치는 로컬 정상 순서의 관찰이며 운영 시간 예산을 변경하지 않습니다.

추가로 실제 arm64 후보에서 9개 phase를 통과했습니다. 관찰 상한 300초 안에서 5초 간격 60회(실측 약 295초)와 유효 WS frame 147개·invalid 0개·업무 추가 효과 0개를 확인했습니다. 실행 중 BFF에 대한 purge는 거부됐고, 잘못된 proxy target의 open은 정비로 복원됐으며, 구형 in-flight timeout은 약 20초 뒤 exit 1과 outcome_unknown/NO-GO로 보존됐습니다. 첫 전환·rollback에서 고정 구형 proxy argv와 신규 생성 argv도 실제 container로 교체하고 대조했습니다. 실제 O02 firewall, 커밋된 production revision, 독립 검토·수용은 이 기록으로 대신하지 않습니다.

Fallback delta: open의 preflight/reload/smoke 실패 시 관리자 origin을 한 번 다시 닫는 보상 동작을 추가했습니다. marker 생성·렌더·nginx -t·HUP는 각 1회이며 503 확인은 최대 5초의 읽기 전용 poll입니다. 확인 실패는 `maintenance_compensation=unknown`으로 남기고 cutover를 중지합니다. 이는 기존 BFF의 모든 작업이 취소되었다는 증거가 아니며, 별도 BFF 종료·효과 분류가 필요합니다. owner는 maintenance 도구/T09이며 현재의 HUP 전환 계약을 사용하는 동안 유지합니다. 업무 요청이나 결과를 replay하지 않습니다.
