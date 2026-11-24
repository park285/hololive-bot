# `!달력` 월간 포스터 고도화·생일/데뷔 기념일 분리 앨범·로테이팅 테마 인계

작성: 2026-11-24

## 1. 목표와 확정된 제약

- **목표**: 기존 세로 목록형 `!달력`(`calendar` 명령) 이미지를 `!스케쥴` 포스터 수준의 7열 월간 그리드 포스터로 교체합니다.
- **생일·데뷔 기념일 분리 앨범**: 한 달에 생일과 데뷔 기념일이 모두 있으면 `SendImages` 2장 앨범(`1/2 생일 달력`, `2/2 데뷔 기념일 달력`)으로 분리 전송하고, 한 종류만 있거나 등록된 기념일이 없는 달은 1장만 전송합니다(`len(pages) == 1`일 때 `transport.WithImageContentType(ctx, "image/png")` 지정).
- **2종 테마 교차 로테이팅**:
  - `prism_daylight` (라이트 3-A: 사쿠라 블로섬 / 홀로 스카이 2D 대기 그라데이션, 3단 명도 분리, 다층 프리즘 굴절 조형)
  - `starlight_orbit` (다크 3-B: `!스케쥴` `Orbit` 패밀리룩, 행성 지평선 곡선·대각 성운·별빛, 50% 반투명 글래스 타일, 헤더 메인 배지와 셀 반투명 골드 배지 계층 분리)
  - `variantPicker`로 연속 호출 시 같은 테마가 두 번 연속 나오지 않도록 교차하며, 한 번의 호출에서 생성되는 생일·데뷔 기념일 2장은 항상 동일한 테마로 묶습니다.
- **조판·색채·예산 불변식**:
  - 가로 폭 `1024px` 고정(`40 + 128×7 + 8×6 = 1024` 정수 격자), 페이지당 세로 상한 `1536px` (`calendarOutputHeight`) 이내 적응형 수렴(6주 달, `trailingEmpty < 3` 하단 목록 카드, 하루 2~6인 단체 카드, 200건 과밀 입력 포함).
  - 멤버 아바타 대표색 추출 시 중앙 얼굴 피부·홍조 영역(`반경 < 0.42`, `Hue <= 44°` 또는 `>= 345°`) 가중치를 감쇠하고, 보색 선형 혼합 없이 황·녹 Abney 이동(`-10°`)과 명도 보정을 거친 퍼스널 컬러만 사용합니다.
  - 억지 영문 장식어를 배제하고 어법에 맞는 한국어 표기(`N월 생일 달력`, `N월 데뷔 기념일 달력`, `N월 생일 목록`, `N월 데뷔 기념일 목록`, `한국 표준시(KST) 기준`)를 사용합니다.
- **저장소·보안·폴백 제약**:
  - 코드 주석 전면 금지(`bash scripts/ci/verify-tools.sh scan` 강제), Go 1.27 컨벤션(`use-modern-go`) 준수.
  - 기존 프로필 사진 다운로드 보안(`calendar_photos.go`, `calendar_photo_security.go`의 허용 호스트·리다이렉트·사설 IP·해상도·바이트·워커 상한 및 부분 실패 시 공유 캐시 미적재 규칙)과 `message_strings` `calendar` 네임스페이스 계약을 그대로 유지합니다.
  - `Fallback delta: none` — 기존 `CalendarCommand`의 렌더링/전송 실패 시 텍스트 폴백(`observeImageTextFallback`)과 `transport.IsReplyOutcomeUnknown` 시 텍스트 폴백 억제 계약만 유지하며 새로운 우회·재시도 경로를 추가하지 않았습니다.

## 2. 작업 위치 및 변경 인벤토리

- **격리 워크트리**: `/home/kapu/work/iris-stack/.worktrees/feat-calendar-poster-upgrade/hololive-bot`
  - 브랜치: `feat/calendar-poster-upgrade` (기준 커밋: `5d707ea2b`, `origin/main`)
  - 생성 명령: `bash tools/ops/stack-worktree.sh add feat/calendar-poster-upgrade hololive-bot` (`go.work` 형제인 `iris-client-go`, `shared-go` detached 워크트리 동시 연결됨)
- **주의 — 원래 체크아웃 보호**: `/home/kapu/work/iris-stack/hololive-bot`에는 이번 작업과 무관한 다른 진행 중 변경분(Valkey/Quadlet/webhook 관련 약 38개 파일)이 있으므로 원래 체크아웃에서 리셋·스태시·덮어쓰기를 하지 마십시오.
- **DB·환경변수·스키마 영향**: 스키마 마이그레이션 및 환경변수 변경 없음. 공유 Valkey 캐시 버전만 `calendarImageCacheVersion = "v6"`(`bot:calendar:album:v6:YYYY-MM:<theme>`)으로 분리되어 배포 즉시 구 `v5` 캐시와 충돌 없이 전환됩니다.

| 구분 | 경로 (`hololive/hololive-api/internal/planes/bot/internal/...`) | 역할 |
|---|---|---|
| 신규 | `render/calendar_gfx.go` | `2x` 슈퍼샘플링 벡터 캔버스(`CatmullRom`), 도형·그라데이션·글로우·블러 그림자·사진 마스킹 |
| 신규 | `render/calendar_font_color.go` | 임베드 `Pretendard`·`NotoSansJP` SFNT 글리프 아웃라인 조판기(뮤텍스 보호), 지각 명도·색상각 보정식 |
| 신규 | `render/calendar_themes.go` | `prism_daylight`(3-A) · `starlight_orbit`(3-B) 시드 팔레트, 2D 성운·행성 지평선(Limb) 배경, 프리즘·궤도 엠블럼 |
| 신규 | `render/calendar_draw.go` | 멤버 엔트리 변환, `1536px` 높이 예산 수렴 계산(`computeCalBoardLayout`), 잉크 경계 정렬 헤더·요일 바·푸터 |
| 신규 | `render/calendar_cells.go` | 빈 날짜·1인/다인 기념일 카드, 3~5칸 병합/전폭 월간 목록 카드, 다인 데뷔 상세 로스터 카드, 빈 달 배너 |
| 수정 | `render/calendar.go`, `calendar_cache.go`, `calendar_disk_cache.go`, `calendar_coalescing.go`, `calendar_layout.go`, `shared_png_cache.go` | `RenderCalendarImagesContext` / `RenderCalendarImagesWithTheme` 로테이팅 분리 앨범 단일 경로, 테마별 메모리·Valkey 번들 캐시(`encodeSchedulePages` 재사용), 동시 요청 병합(coalescing), 구형 단일 이미지 경로·`sharedPNGCache` 제거 |
| 수정 | `command/handlers/handlercore/command.go`, `command/handlers/handler_calendar.go` | `CalendarImageRenderer` 앨범 인터페이스 전환 및 `SendImages` 전송 연결 |
| 수정 | `render/calendar_test.go`, `calendar_layout_test.go`, `calendar_context_security_test.go`, `calendar_photo_security_test.go`, `command/handlers/handler_calendar_test.go`, `command/handlers/handler_calendar_context_security_test.go` | 생일/데뷔 분리·테마 교차 회전·Valkey 번들 공유·동시성 병합·보안·6주 달 높이 예산·앨범 전송 단위 테스트 |
| 수정 | `CHANGELOG.md`, `docs/current/services/hololive-api.md` | `미출시` 변경 이력 및 Valkey `bot:calendar:album:v6:<YYYY-MM>:<theme>` 캐시 계약 갱신 |
| 삭제 | `render/cardkit/*`, `render/theme/theme.go`, `render/palette.go`, `render/calendar_fonts.go` | 구형 리스트 달력 전용이었던 미사용 렌더링 보조 패키지·팔레트 정리 |

- **시각 검수 산출물 (Git 추적 외 아티팩트 참고용)**:
  - `/home/kapu/.gemini/antigravity-cli/brain/2aedbf56-98f7-41d2-ad6d-7bfdec0608a5/v7_3a_prism_light_pair.png` (`v7_3a_birthday.png`, `v7_3a_anniversary.png`)
  - `/home/kapu/.gemini/antigravity-cli/brain/2aedbf56-98f7-41d2-ad6d-7bfdec0608a5/v7_3b_orbit_dark_pair.png` (`v7_3b_birthday.png`, `v7_3b_anniversary.png`)

## 3. 수행 완료한 검증과 결과

마지막 코드 수정 직후 격리 워크트리에서 아래 검사를 모두 실행해 통과를 확인했습니다.

1. **주석 금지 및 도구 정적 스캔**
   - 명령: `bash scripts/ci/verify-tools.sh scan`
   - 결과: 종료 코드 `0` (위반 0건).
2. **린트 (`golangci-lint`)**
   - 명령: `golangci-lint run ./hololive/hololive-api/internal/planes/bot/internal/render/... ./hololive/hololive-api/internal/planes/bot/internal/command/handlers/... ./hololive/hololive-api/internal/planes/bot/internal/bot/orchestration/...`
   - 결과: `0 issues.` (`dupl`, `funlen`, `gocyclo`, `maintidx`, `nestif`, `revive`, `wsl_v5` 포함 무결점).
3. **모듈 전체 빌드 및 `go vet`**
   - 명령: `cd hololive/hololive-api && go build ./... && go vet ./...`
   - 결과: 전 패키지 통과.
4. **동시성(`-race`)·보안·캐시·레이아웃 단위 테스트**
   - 명령: `cd hololive/hololive-api && go test -race ./internal/planes/bot/internal/render ./internal/planes/bot/internal/command/handlers/... ./internal/planes/bot/internal/bot/orchestration/...`
   - 결과: 전수 통과 (`render` 100.3s, `handlers` 11.9s, `orchestration` 5.9s).
5. **실데이터 렌더링 및 디자인 검수**
   - 2026년 11월 현역 기념일 15명(생일 5명, 데뷔 기념일 10명 — `FLOW GLOW` 5인 동시 2주년 포함)과 실제 유튜브 채널 아바타 이미지로 프로덕션 `NewCalendarCardRenderer` 실출력을 생성해 색채·조판 디자이너 리뷰어 검수를 통과했습니다.

## 4. 남은 작업과 필요한 승인

구현·구형 단일 이미지 경로 정리(`RenderCalendarImageContext`·`sharedPNGCache`·`cardkit`·`theme` 제거)·문서 갱신(`CHANGELOG.md`, `docs/current/services/hololive-api.md`)과 로컬 단위·린트 검증을 완료해 `feat/calendar-poster-upgrade` 브랜치에 커밋했습니다.

- [ ] **로컬 `main` 병합 (선행 정리 필요)**:
  - 현재 `main`이 체크아웃된 `/home/kapu/work/iris-stack/hololive-bot`에 타 작업(Valkey/Quadlet/webhook nonce 등 54개 파일, `CHANGELOG.md`·`docs/current/services/hololive-api.md` 포함)의 미커밋 변경분이 남아 있어, 타 작업 훼손 방지를 위해 `main` 브랜치 이동은 중단했습니다.
  - 타 작업이 정리되거나 커밋된 뒤 `git -C /home/kapu/work/iris-stack/hololive-bot merge feat/calendar-poster-upgrade`로 병합하고, 원격 반영이 끝나면 `bash tools/ops/stack-worktree.sh remove feat/calendar-poster-upgrade`로 워크트리를 정리합니다.
- [ ] **원격 게시 / PR / 메타 포인터 갱신 (명시적 승인 필요)**:
  - `git push`, PR 생성(`park285/.github` PR 템플릿 준수), `iris-stack` 상위 저장소의 `hololive-bot` 서브모듈 포인터 갱신은 각각 사용자 승인 후 수행합니다.
- [ ] **운영 배포 및 실단말 확인 (별도 배포 승인 필요)**:
  - 승인 후 kapu에서 `./build-all.sh --build-only --no-bump`로 이미지를 빌드하고 `hololive-bot-ops` 런북 절차(`scripts/deploy/quadlet-redeploy-service.sh`)에 따라 중앙 `hololive-api.service`에 반영합니다.
  - 반영 후 카카오톡 테스트 방에서 `!달력`을 2회 연속 호출해 2장 앨범 분리 수신과 라이트(`3-A`) ↔ 다크(`3-B`) 교차 회전, 모바일 인앱 뷰어 가독성을 최종 확인합니다.
