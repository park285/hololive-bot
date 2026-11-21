# 변경 이력

현재 릴리즈 버전은 루트 `VERSION`에서 관리하고 Git tag는 `r<version>` 형식을 사용합니다. 다음 릴리즈부터 번호를 1.0.0으로 다시 시작하며, `v<version>` tag와 절은 이전 번호 체계의 기록입니다.
버전은 배포하거나 게시할 때만 올리며, 절차는 [릴리즈 버전 관리](docs/current/runbooks/release.md)를 따릅니다.
이 문서는 `미출시`와 최근 세 버전 구간(그 사이의 tag 없는 준비 구간은 세지 않음)만 유지하며, 이전 기록은 이 파일의 Git 이력과 tag에서 확인합니다.

## 미출시

- **이벤트 등급(`!이벤트등급`):** 점수와 지역을 등록하면 공개된 최종 컷 기준 결과(최고 충족 등급, 경계 동점, 순위권 밖, 24시간 내 최종 컷 없음)를 기존 알림 발송 경로로 보냅니다. 후속 DB 변경 `002_epoch4_event_grade`가 canonical 테이블, append-only 관측 원장, 지역별 소비 offset, collector c 전용 수집 lease와 발행 함수를 만듭니다. alarm-worker는 관측을 지역별 순번대로 적용하고, 아직 적용하지 않은 발행이나 진행 중 수집이 있으면 판정을 미룹니다. 게임 수집은 collector c 소관이며 worker에는 게임 helper·인증 설정이 없습니다. 이 DB 변경을 포함한 배포는 승인된 정비 창에서 진행합니다.
- 스택 공통 lint 자산(주석 금지 검사, `verify-tools.sh`, Go 린트 행렬·도구 resolver, 생성된 `.golangci.yml`)을 메타 정본과 맞췄습니다. 확장된 검사가 찾은 workflow YAML·셸 heredoc·JavaScript·Go 주석을 지웠고, 해석되지 않던 heredoc·here-string을 파이프나 변수로 바꿨습니다(Valkey 상태 점검, 풀러 SQL 확인, go.work 동기화 검사, 종료 live head 복구 시험). SQL이 아닌 스키마 카탈로그 직렬화 golden은 `hololive/hololive-dbtest/testdata/schema_snapshot.golden`으로 이름을 바꿨습니다. 동작은 바뀌지 않습니다.
- 버전 번호를 다음 릴리즈부터 1.0.0으로 다시 시작하고 tag를 `r<version>`으로 씁니다. 배포 복구 도구는 원래 릴리스 tag로 `r<version>`과 이전 `v<version>`을 모두 받고, `scripts/publish-release.sh`는 새 Release를 `r<version>` tag로만 만듭니다.
- **Gemini Decisions**의 공식 Go SDK 클라이언트를 방송 유형 분류와 일정 포스터·변경 게시글 검토가 공유합니다. 판정마다 요청은 한 번만 보내며, 응답 상한 64 KiB와 용도별 실패 처리를 유지합니다. HTTP 오류와 로그에는 상태 번호와 분류에 쓰는 코드만 남겨 Gemini가 응답에 포함한 API 키를 보호합니다.
- **CI:** 공개 CI에서 collector Node 설치를 공유하고 셸·포맷 검사를 `fast-gate`에 합쳤습니다. 게시 훅과 로컬 Go 검사는 같은 변경 경로를 쓰며, collector 산출물 손상 시험은 필요한 경우에만 정상 빌드 결과를 재사용합니다. CGO 비활성·성능·race·선택적 통합 시험은 일회용 PostgreSQL 서버를 공유합니다.
- **게시·보안 검증:** `CHANGELOG.md`의 제목 표기는 게시 차단 조건에서 제외하고 `VERSION` 형식 검사는 유지합니다. npm 감사는 수동 목록 없이 추적된 lockfile을 검사하되 운영 프로젝트의 lockfile 누락은 실패로 처리합니다. main push는 정기·수동 이미지 검사를 취소하지 않습니다.

## v20.0.4 - 2026-11-21

- **중앙 앱 복구:** 최초 소스 게시 후 API가 schema job 발행 전에 종료된 경우, 원본 영수증 및 변경되지 않은 두 writer를 증명한 뒤에만 별도 후보 소스를 게시할 수 있습니다. API와 worker의 후속 전환 순서를 검사하고, worker 수용 확인(`receipt`)이 기록된 뒤에만 정상 후속 게시를 허용합니다. 정상 게시나 롤백의 조건은 완화하지 않습니다.
- **중앙 앱 기동:** 봉인 배포와 롤백은 기존 의존성이 활성이어도 대기 중인 job이 있으면 schema 또는 writer job을 요청하지 않고 중단합니다.

## v20.0.3 - 2026-11-19

- **중앙 앱 기동:** systemd가 보고한 `-.mount`를 옵션이 아닌 루트 마운트 이름으로 조회합니다. 모든 기존 의존성이 활성일 때만 schema 또는 writer job을 요청합니다. 이미 소스만 게시되고 writer가 이전 세대인 설치에 재시도·복구 권한을 추가하지 않습니다.

## v20.0.2 - 2026-11-19

- **중앙 앱 소스 게시:** 봉인 세대의 prestart와 그 라이브러리는 후보 커밋의 Git 바이트를, 기존 호스트에 남기는 libexec는 설치된 구 커밋의 바이트를 각각 요구합니다. 두 세대의 `postgres-endpoint.sh`가 다르다는 이유만으로 게시를 거부하지 않으며, 설치된 파일이나 방화벽·DB 설정을 교체하지 않습니다.
