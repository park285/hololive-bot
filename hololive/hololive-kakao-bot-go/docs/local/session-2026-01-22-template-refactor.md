# OUTBOX 템플릿 리팩토링 세션 요약

**Date**: 2026-01-22
**Status**: 완료 (DB 마이그레이션 재적용 필요)

## 배경

기존 OUTBOX 알림 템플릿 형식이 방송 알림과 불일치:
- 기존 방송 알림: `🔔 채널명 방송 알림` (대괄호 없음)
- 기존 OUTBOX: `[멤버명] 새 쇼츠` (대괄호 있음, 이모지 뒤에)

## 변경 사항

### 1. 아키텍처 결정

**헤더/본문 분리 패턴 채택** (예외 없음):
- DB 템플릿: 본문만 저장 (제목/내용 + URL)
- 코드 (`applySeeMorePadding`): 헤더 추가 + 카카오톡 패딩 적용

### 2. 수정 파일

| 파일 | 변경 내용 |
|------|-----------|
| `scripts/migrations/012-seed-all-templates.sql` | 모든 OUTBOX 템플릿에서 헤더 제거 (MILESTONE 포함) |
| `internal/service/youtube/outbox/dispatcher.go` | `applySeeMorePadding`에 MILESTONE 케이스 추가, 헤더 형식 통일 |

### 3. 최종 출력 형태

| 종류 | 헤더 | DB 템플릿 본문 |
|------|------|----------------|
| SHORTS | `📱 멤버명 쇼츠 알림` | `{{.Title \| truncate 50}}\n{{.URL}}` |
| COMMUNITY | `📝 멤버명 커뮤니티 알림` | `{{.ContentText \| truncate 100}}\n{{.URL}}` |
| VIDEO | `📺 멤버명 영상 알림` | `{{.Title \| truncate 50}}\n{{.URL}}` |
| MILESTONE | `🎉 멤버명 마일스톤 알림` | `{{.MemberName}} {{.Milestone}} 돌파!` |
| 방송 알림 | `🔔 채널명 방송 알림` | (formatter_templates.go) |

### 4. Oracle 검증 결과

- SHORTS/VIDEO: 정상
- COMMUNITY: 500자 미만일 때 헤더 누락 버그 발견 → 수정 완료
- MILESTONE: 기존 예외 처리 → 헤더/본문 분리 원칙으로 통일 완료
- byte 길이 → rune 길이 변경 (한글/이모지 정확한 글자 수 계산)

## 적용 명령어

```bash
cat hololive-kakao-bot-go/scripts/migrations/012-seed-all-templates.sql | \
  docker exec -i llm-postgres psql -U twentyq_app -d hololive
```

## 후속 작업 (다음 세션)

### P1: CMD_* 템플릿 DB 단일 SSOT 전환

**현재 상태**:
- DB에 `CMD_*` 템플릿 시드됨 (`012-seed-all-templates.sql`)
- 런타임은 임베디드 템플릿 사용 (`formatter_templates.go`)
- 이중 관리 상태 (불일치 위험)

**작업 내용**:
1. `executeFormatterTemplate()` → `template.Renderer.Render()` 변경
2. 호출 위치 (13곳):
   - `formatter_help.go`: help.tmpl
   - `formatter_alarm.go`: alarm_added/removed/list/cleared/notification.tmpl, milestone_*.tmpl
   - `formatter_directory.go`: member_directory.tmpl
   - `formatter_streams.go`: live_streams/upcoming_streams/channel_schedule.tmpl
3. `formatter_templates.go` 파일 제거

**예상 작업량**: Medium (13곳 수정, 1 파일 삭제)

### P2: 회귀 테스트 추가

- COMMUNITY 499/500 rune 경계에서 헤더/패딩 적용 테스트
- MILESTONE 헤더 형식 테스트

## 관련 파일

- `internal/service/youtube/outbox/dispatcher.go` - OUTBOX 발송 로직
- `internal/adapter/formatter_alarm.go` - 방송 알림 포맷터
- `internal/adapter/formatter_templates.go` - 임베디드 템플릿 (제거 대상)
- `internal/service/template/renderer.go` - DB 템플릿 렌더러
- `internal/util/kakao.go` - `ApplyKakaoSeeMorePadding` 유틸
