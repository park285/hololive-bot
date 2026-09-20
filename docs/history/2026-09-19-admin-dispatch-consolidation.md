# 발송 원장 PR 재통합과 조회 경계 검토

## 기준과 범위

2026-09-19에 확인한 최신 main `f74ed443ddfc` 위에서 #508의 `065f1decf359`를 재통합합니다. #508 이후 main의 12개 커밋에 포함된 워커 발송·시간 판정·런타임 종료·배포·검증 변경을 보존합니다. 변경이 없는 admin 내부 하위 트리와 #508의 라우트 변경만 가져오며, main에서 변경된 app/build_runtime.go, build_runtime_auth.go, runtime_admin_api.go와 새 종료 검사는 덮어쓰지 않습니다.

짝 PR은 https://github.com/park285/iris-admin/pull/10 입니다. 서로 다른 저장소를 하나로 이동시키지 않고 기존 PR 계보를 유지한 대체 PR 한 쌍으로 검토합니다. 원본 PR을 자동 종료하거나 main에 병합하지 않습니다.

## 보존한 안전 경계

기존 #508의 인증·속도 제한·엄격한 입력 판정, 문자열 bigint, 마이크로초 리비전, 묶음 전체 검사, PostgreSQL serializable 잠금과 감사 기록을 보존합니다. 부분 묶음, 상태 변경, 전송·취소 흔적이 있는 항목은 재처리하지 않습니다. 본문과 외부 요청 식별자를 바꾸지 않으며, 실제 발송은 기존 worker만 수행합니다. commit 결과가 불명확한 실패를 자동 재시도로 전환하지 않습니다.

원본의 HTTP·라우트·PostgreSQL 통합 테스트 파일도 그대로 포함합니다. 파일을 보존했다는 사실은 이번 실행에서 해당 통합 테스트를 통과했다는 뜻이 아닙니다.

## 새로 개선한 목록 조회

기존 list.sql은 모든 요청에 선택적 필터 OR 조건 네 개를 남겼습니다. 새 buildListQuery는 실제로 입력한 상태·방·채널·ID 커서에 대해서만 고정된 predicate를 추가합니다. 필터 존재 여부에 따른 SQL 형태는 최대 16가지입니다.

SQL 문법과 열 이름은 코드에서만 정하고 사용자 값은 전부 바인딩 인자로 전달합니다. SQL projection·오류 코드 마스킹·열 순서는 변경하지 않습니다. 상태 미지정 시 DLQ와 quarantined만 조회하는 기본 동작, ID 내림차순 keyset 커서, PageSize 50 및 다음 페이지 판정용 51행 제한도 유지합니다. bigint 커서는 int64로 바인딩하여 JavaScript 정밀도 경계보다 큰 값도 손실 없이 다룹니다.

이 변경은 실제 DB 실행계획이나 지연시간 개선을 측정한 결과가 아닙니다. PostgreSQL에서 필터별 EXPLAIN (ANALYZE, BUFFERS)와 동일 결과·커서 검증을 수행해야 합니다. 인덱스 추가나 운영 DDL은 포함하지 않습니다.

## 이번에 실행한 검사

Go 1.23.2의 독립된 디렉터리에서 원본 model.go와 model_test.go를 수정 없이 사용했습니다. GitHub blob SHA를 대조한 뒤 새 list_query.go, list_query_test.go와 실제 SQL projection을 추가해 검사했습니다. 모듈의 Go 버전이나 저장소 의존성은 낮추지 않았습니다.

| 검사 | 실제 결과 |
| --- | --- |
| 모델·쿼리 생성기 go test -race -count=1 | 통과. 최상위 테스트/퍼징 시드 실행기 14개, 하위 검사와 시드를 포함한 pass 이벤트 89개입니다. |
| 같은 파일 집합 go vet | 진단 없이 통과했습니다. |
| 위 두 구현 파일의 statement coverage | 98.7%입니다. 저장소 전체 또는 HTTP/DB 계층 커버리지가 아닙니다. |
| FuzzListQueryBindings, 2 workers, fuzztime=3s | 92,687회 실행 후 통과했습니다. |

신규 검사는 필터 존재 여부 16개 조합, SQL 입력 보간 방지, bigint 최대값·정밀도 경계, 잘못된 입력의 SQL 생성 차단, 요청 간 인자 격리, placeholder와 인자 수·순서 및 LIMIT 보존을 다룹니다. 기존 모델 테스트의 재처리 금지 상태·부분 묶음·리비전 손실·과대 묶음 판정도 같은 실행에 포함했습니다.

## 아직 실행하지 않은 병합 조건

지정된 Go 1.27 환경과 shared-go/iris-client-go를 포함한 실제 workspace에서 전체 빌드·테스트·race, HTTP/라우트 계약, PostgreSQL 동시성·롤백·감사 원자성 통합 검사, NilAway·govulncheck 및 저장소의 local/pre-push gate를 실행해야 합니다. 이 분리 검사는 해당 gate를 대신하지 않습니다. CI 소유권이나 gate를 약화시키지 않으며 PR은 Draft로 발행합니다.

## 어드민 연동의 정확한 상태

발송 원장 UI와 Iris Admin gateway OpenAPI 정본·생성 계약의 완성된 연동은 여전히 미구현입니다. 어드민 #10은 기존 UI 공통 구조·세션·SSR 수명을 통합한 PR이지 발송 원장 화면을 완성한 PR이 아닙니다.

추가 연동에서는 gateway가 operatorId를 인증된 사용자에 결합해야 합니다. 브라우저가 전달한 운영자 이름을 신뢰하거나 allowlist를 우회하면 안 됩니다. csrf/비밀번호 증명, 문자열 bigint와 원본 updatedAt의 소수초, 묶음 전체 확인, 중복 위험 동의, 결과 불명 시 조회 우선과 무자동재시도 정책을 양쪽 E2E로 확인해야 합니다. 두 PR을 병합하는 것만으로 이 기능을 운영에 노출하지 않습니다.

이 작업은 운영 배포, 스키마 변경, worker 강제 실행, 원본 PR 자동 종료 또는 자동 병합을 수행하지 않습니다.
