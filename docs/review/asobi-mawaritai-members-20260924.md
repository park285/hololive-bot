# ASOBI★MAWARI-TAI! 신규 등록 준비 — 2026-09-24

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
프로젝트 소유 `db-migrate` 러너로 203을 적용한 뒤 DB 등록과 캐시 갱신·수집 대상 반영을 확인한다.
현재 단계는 적용 전이며 완료 증거를 별도로 기록한다. Git 게시와 애플리케이션 교체는 범위에 포함하지 않는다.
