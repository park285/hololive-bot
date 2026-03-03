# M2 shared 경계 정리 실행 초안 (2026-03-03)

## 1) 목표

`shared-go`를 기술 공통 라이브러리로 고정하고, 도메인/서비스 맥락 코드는 `hololive-shared`(contracts/application/adapter)로 단계 이동한다.

## 2) As-Is 인벤토리 (2026-03-03)

`go list` 기반 importer 집계 기준:

### 2-1. 사용량 상위 shared-go 패키지

1. `json` (importers: 29)
2. `stringutil` (17)
3. `jsonutil` (6)
4. `workerpool` (5)
5. `retry`, `runtime/automaxprocs` (각 4)

→ 상위 사용 패키지는 기술 공통 성격이 뚜렷하므로 M2 범위에서 유지.

### 2-2. 경계 정리 우선 후보

1. `irisx`
   - webhook 경로/헤더/dedup key(`iris:*`)를 포함
   - 기술 primitive보다 서비스 계약 성격이 강함
2. `processinglock`
   - `DomainService` wrapper가 포함되어 도메인 맥락 노출
   - 앱/도메인 계층으로 이동 또는 thin helper로 축소 필요

### 2-3. 사용 0 패키지(후보)

`cache`, `errors`, `flagx`, `ginserver`, `grpcx`, `httpx`, `ptrutil`, `shutdown`, `sliceutil`, `textutil`, `timeutil`, `valkeyx`, `valkeyx/mqutil`

→ import graph + 코드 검색 기준 dead 확인 후 13개 패키지 제거 완료(allowlist 동기화).

## 3) 실행 순서 (업데이트)

1. `irisx` 완전 제거
   - 계약 상수/DTO/유틸을 `hololive-shared/pkg/contracts/iris`로 이동
   - `shared-go/pkg/irisx` 삭제 (호환 어댑터/페일백 없이 직접 전환)
2. `processinglock` 완전 제거
   - `shared-go/pkg/processinglock` 삭제
3. 사용 0 패키지 검증
   - 코드 검색 + 실제 빌드 경로 확인 후 삭제 후보 확정
4. allowlist 정리
   - 패키지 이동/삭제 시 `docs/architecture/shared-go-package-allowlist.txt` 즉시 동기화

## 4) 회귀 방지

1. CI: `scripts/architecture/check-shared-go-packages.sh`로 신규 패키지 유입 통제
2. CI: `scripts/architecture/check-shared-go-boundary.sh`로 hololive 모듈 import 역유입 차단
3. PR 규칙: 경계 변경 PR에 사유/영향/롤백/검증 로그 포함

## 5) 진행 체크포인트

- [x] `shared-go/pkg` 신규 패키지 allowlist 게이트 도입
- [x] Iris 계약 상수/DTO를 `hololive-shared/pkg/contracts/iris`로 완전 이관 (`shared-go/pkg/irisx` 삭제)
- [x] `processinglock` 제거 (`shared-go/pkg/processinglock` 삭제)
- [x] admin/kakao `internal/server/*_compat.go` 제거 + sharedserver 직접 참조로 전환
- [x] 사용 0 패키지 dead 여부 확인 및 단계 제거(`cache`, `errors`, `flagx`, `ginserver`, `grpcx`, `httpx`, `ptrutil`, `shutdown`, `sliceutil`, `textutil`, `timeutil`, `valkeyx`, `valkeyx/mqutil`)
