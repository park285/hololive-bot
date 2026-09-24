# ASOBI★MAWARI-TAI! 신규 등록과 운영 반영 — 2026-09-24

## 변경

`203_asobi_mawaritai_members.sql`을 epoch-2 manifest의 064 단계에 추가했다.
개인 4명과 공용 채널 1개의 이름·한국어/일본어 별칭·유닛·공식 링크를 등록한다.
한국어 이름은 일본어 발음의 음역이며 공식 한국어 표기로 확인한 값은 아니다.
개인은 공식 생일·데뷔일을 저장하고 공용 채널에는 개인 기념일을 부여하지 않는다.
생일의 2000년은 기존 저장 규칙을 따른 월·일 기준 연도이다.

| 멤버 | 한국어 이름 | YouTube 채널 ID | 생일 | 데뷔일 |
| --- | --- | --- | --- | --- |
| Hyakuto Kyoko | 햐쿠토 쿄코 | UCSjQDxud2HkAO2DVD3lwxmw | 5월 8일 | 2026-09-25 |
| Achichi Mela | 아치치 메라 | UC8eitCE9Z6EwUCs-VUi1blg | 4월 16일 | 2026-09-24 |
| Suzuna Tsuzuri | 스즈나 츠즈리 | UCy9mgxB8pn2C4aNK_MPthDQ | 6월 28일 | 2026-09-25 |
| Sorashina Sopia | 소라시나 소피아 | UCROQtXcp2loQEmvpe5rhJzQ | 6월 13일 | 2026-09-24 |
| ASOBI★MAWARI-TAI! | 아소비★마와리타이! | UCAHwWUotyS3l2qBetFDsjgQ | NULL | NULL |

공식 근거:

- [유닛 발표와 데뷔 일정](https://hololive.hololivepro.com/special/22482/)
- 개인 DATA: [Kyoko](https://hololive.hololivepro.com/talents/hyakuto-kyoko/), [Mela](https://hololive.hololivepro.com/talents/achichi-mela/), [Tsuzuri](https://hololive.hololivepro.com/talents/suzuna-tsuzuri/), [Sopia](https://hololive.hololivepro.com/talents/sorashina-sopia/)
- 공식 사이트에서 연결한 YouTube 채널 페이지의 `channelMetadataRenderer.title`과 `externalId`를 대조했다: `@HyakutoKyoko`, `@AchichiMela`, `@SuzunaTsuzuri`, `@SorashinaSopia`, `@hololive_ASOBIMAWARITAI`.

## X 계정과 공식 채널

개인 4명뿐 아니라 유닛의 공식 YouTube 채널도 등록한다. X 계정은 참고 정보만 기록한다.
X의 `@handle`은 위 공식 발표 링크에서 확인했다. 숫자 ID는 2026-09-24
`https://api.fxtwitter.com/{handle}`의 공개 응답에서 `code=200`, `user.screen_name`,
`user.name`, `user.id`를 대조한 값이다. X 직접 페이지는 웹 도구에서 403을 반환하여
숫자 ID의 직접 확인은 못 했으며, 이 값의 조회 출처는 제3자 FxTwitter이다.
FxTwitter는 이번 준비를 위한 일회성 조회에만 사용했으며 런타임 의존성으로 추가하지 않았다.

| 계정 | X handle | X 숫자 ID |
| --- | --- | --- |
| 햐쿠토 쿄코 | [@hyakuto_kyoko](https://x.com/hyakuto_kyoko) | 2054830382089166848 |
| 아치치 메라 | [@achichi_mela](https://x.com/achichi_mela) | 2054829476979376128 |
| 스즈나 츠즈리 | [@suzuna_tsuzuri](https://x.com/suzuna_tsuzuri) | 2054826700715048960 |
| 소라시나 소피아 | [@sorashina_sopia](https://x.com/sorashina_sopia) | 2054825907916054528 |
| 유닛 공식 | [@ASOBIMAWARITAI](https://x.com/ASOBIMAWARITAI) | 2084846250344710144 |

`members`에는 X 계정 저장 컬럼이 없다. 스페이스 대상은 별도 `X_SPACES_CONFIG_FILE`이 소유한다.
사용자가 "스페이스 감지 추가는 X"로 범위를 명시했으므로 X 스페이스 대상 추가는 제외한다.
기존 운영 설정은 변경하지 않았으며 준비했던 매핑 추가분도 제거했다.

## 동작과 보존

기존 멤버 목록과 정보 조회는 DB의 `members.units`를 사용하며, 수집 대상 조회는 활성 `channel_id`를 사용한다.
따라서 새 유닛에 대한 런타임 코드나 고정 목록 추가는 필요하지 않다.
마이그레이션은 기존 slug의 행을 수정하지 않으므로 관리자가 등록한 이름·별칭·졸업 상태와 구독을 보존한다.
동일 slug의 소속/채널이 다르거나 해당 채널이 다른 slug로 등록되어 있으면 예외를 내고
한 DO 문 안의 신규 INSERT 전체를 취소한다. 충돌은 운영 자료를 대조한 뒤 해결해야 한다.
동일 identity로 이미 등록된 행의 누락 메타데이터를 보충하는 작업은 이 시드에 포함하지 않는다.
Fallback delta: none.

## 실제 검증

빌드 호스트 `kapu`의 격리 PostgreSQL 18.6에서 다음 검사를 통과했다.

- `bash scripts/architecture/check-migration-manifest.sh`
- `go test -race -count=1 ./hololive/hololive-dbtest -run 'TestAsobi|TestSchemaSnapshotGolden|TestMemberInfoMigration|TestHoloAN' -v`
  — 신규 등록, 공식 날짜, 재적용 시 행 버전·데이터·기존 구독 보존, identity 충돌 3종의 원자적 취소, 스키마 골든.
- `go test ./hololive/hololive-api/internal/planes/bot/internal/assets/fonts ./hololive/hololive-api/internal/planes/bot/internal/command/handlers/info ./hololive/hololive-shared/pkg/service/member`
  — 새 이름의 글리프 포함 여부, 멤버 정보/목록과 repository 회귀 검사.
- 저장소의 고정 NilAway 바이너리로 `go vet -p 1 -vettool=... -pretty-print -include-errors-in-files=... ./hololive/hololive-dbtest`.
- 저장소의 고정 `golangci-lint v2.13.2`로 `run ./hololive/hololive-dbtest/...` — 0 issues.
- iris-stack 루트의 `bash tools/checks/check-stack-db-access-policy.sh`.

## 적용 상태

사용자가 운영 적용을 승인했다. 이후 지시로 X 스페이스 대상 추가는 제외했다.
`hololive-osaka`의 읽기 전용 세션(`transaction_read_only=on`)에서 대상 slug/채널 5개가
모두 미등록이며 기존 멤버는 130행, 최신 migration은 202임을 확인했다.

**2026-09-24 22:35 KST에 운영 반영을 완료했다.**

- `db-migrate`: `applied=1 skipped=63 total=64`. 203 ledger 시각은 `2026-09-24 13:35:24.461814+00`이다.
- 203의 운영 checksum과 검토한 SQL의 SHA-256은 모두 `15005b2d6f614bddf446b61405c90f52c6545b9563031cbd97bcef1bd9c325ae`이다.
- 멤버는 130 → 135행이며 새 ID는 10972~10976이다. 이름·별칭·기수·공식 링크·공개 날짜가 등록되었다.
- 기존 130행(`id <= 10971`)의 식별자·이름·별칭·소속·상태·날짜·유닛·공식 링크 지문은
  적용 전후 `e4150f57074561d93cfacfd46cfc2d33`으로 같았다. 수집기가 갱신하는 photo 필드는 지문에서 제외했다.
- H3 관리자 `GET /api/holo/channels?channelIds=...`에서 5개 채널과 이름을 조회했다.
  전체 `/members` 응답은 점검 도구의 64 KiB 제한을 넘어, 기존의 채널 ID 지정 조회로 범위를 좁혀 검증했다.
- 현재 수집 projection에서 5개 채널 모두 `channel_photo`, `channel_profile`, `channel_stats`,
  `live_snapshot`이 활성 상태임을 확인했다.
- API의 bot/admin health와 llm readiness가 모두 통과했다. 적용 이후 API의 오류 표식은 0건이었다.
- 중앙 알림 워커와 수집기 c의 이미지·시작 시각이 유지됐고 health는 정상이다. 원격 AP는 변경하지 않았다.
- X 스페이스 설정은 기존 5개 계정 그대로다. 마스터·운영 파일을 변경하지 않았으며 운영 파일 SHA-256은
  적용 전후 `c87039ef1541c280feb9750b7f9989422867f188d3f3824c18c7b07eada16526`으로 같았다.
- 사후 DB 조회도 매 세션 read-only를 증명했다. 관측한 TCP 연결은 모두 TLSv1.3이었다.

## 배포 산출물과 검증

운영 버전 `57c2ab718acf86fd7f893c46f14f5f5459c1c8bf`와 당시 로컬 main 사이에 의존성 차이가 있어,
운영 버전에 이번 4개 파일만 더한 격리 작업 트리에서 빌드했다.
이 작업의 Git 원격 게시나 main 통합은 수행하지 않았다.

- 작업 트리: `/home/kapu/work/holo-asobi-20260924/hololive-bot`
- 배포 소스: `a8bd3f780377a73ab1dd6510045330d74b38e107`
- API 이미지: `sha256:872d4968dbe5d3336a2d591670dd28d6218ac2a2f5a756b2628b7865f48f0eac`, `linux/arm64`
- 실행 API의 시작 시각: `2026-09-24T13:35:37.841678875Z`; 실행 이미지와 검증 이미지가 일치한다.
- API 실행 파일 SHA-256은 기존/후보 모두
  `8244661c08fed900999ffe1ce427359098fb7544cb2ad77ff5ce3c9503a6f116`이다.
  신규 migration을 내장한 러너를 이후 Compose 기동에서도 사용하도록 API 이미지를 교체했다.
  API 동작 코드·의존성·버전 번호는 유지했다.
- 러너의 기존 순수 migration 경로(`POSTGRES_ADMIN_PASSWORD=`)로 실행해 DB 역할·비밀번호 bootstrap은 수행하지 않았다.
- `kapu`에서 `LOCAL_CI_GO_SCOPE=changed`, 운영 revision 기준으로 `local-ci.sh` 전체를 통과했다.
  아키텍처·lint·NilAway·PGO·수집기·성능·일반 테스트·race 검사를 포함하며 선택형 integration 테스트는 실행하지 않았다.
  격리 환경에서 누락됐던 고정 버전 uv와 YouTube.js 의존성을 준비한 뒤 재실행했으며 검사 우회는 없었다.
- 로컬 로그: `/tmp/hololive-asobi-deploy-20260924/local-ci-final.log`, `image-build.log`,
  `apply-continuation.log`, `db-after.txt`, `runtime-after.txt`.

처음 원격 실행은 config 확인용 Compose 프로세스가 전달된 표준입력을 소비해 migration 전에 끝났다.
ledger와 대상 행이 모두 없음을 다시 읽기 전용으로 확인한 뒤, Compose 표준입력을 `/dev/null`로 분리해
나머지 단계를 한 번 실행했다. migration 성공 후 재실행하거나 데이터 롤백하지 않았다.

복구 자료는 `/opt/hololive-bot/releases/asobi-20260924-a8bd3f780/`에 보존했다.
여기에 이전 source revision/manifest, 적용 로그와 변경 전 런타임 정보가 있다.
이전 이미지는 `hololive-api:rollback-asobi-20260924-57c2ab718`
(`sha256:ea73fa5cbdc5124073ed1efad1ee554bb3dfce39bcdf3903ee50baa835278f51`)로 보존했다.
203 ledger가 추가됐으므로 이전 migration 러너로 재적용하지 않는다. 기존 멤버·구독을 삭제하는 롤백은 수행하지 않았다.

사후 확인 시점에 신규 채널의 통계·프로필 첫 관측은 아직 저장되지 않았다.
등록·조회·수집 대상 편입까지 검증했으며 실제 알림 메시지 발송은 시험하지 않았다.
