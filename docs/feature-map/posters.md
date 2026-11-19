# 주간 일정 포스터 읽기와 교정

사용자는 멤버별로 이번 주 일정을 받고, 관리자는 원본·번역·추가 및 취소된 일정을 검토한다.

## 하위 기능

예약·수동 수집, 포스터 이미지·주차 판정, 행 추출 및 번역, 한국어 교정 메모, 변경 슬롯 복구와 취소, 주차 수정·승인·반려, 멤버별 카드 디자인이 있다.

## 사용자 경로

- 방에서 `!스케쥴 미코`. `!스케줄`이나 `!일정`이 아니라 **쥴**이다. 템플릿은 채팅 인자가 아니라 Console의 멤버 관리에서 선택한다.
- `/api/holo/members/:id/schedule-card-template`에서 `GET`, `PATCH`. 포스터 기본 경로 `P=/api/holo/schedule-posters`에서 `GET P`, `GET P/summary`, `POST P/collect`, `GET`·`POST P/memo`.
- 한 건 `D=P/:id`에서 `GET D`, `GET D/image`, `GET D/changes`, `POST D/changes/revoke`, `POST D/changes/restore`, `GET`·`POST D/rows`, `POST D/rows/polish`, `POST D/changes/polish`, `POST D/confirm`, `POST D/week`, `POST D/reject`. 제공 경로의 원본은 `hololive/hololive-api/internal/planes/admin/internal/httpapi/routes.go`다.

## hololive-runtime.sh로 구동

```bash
p=$(scripts/verify/hololive-runtime.sh run)
cat "$p"/{memo-before.json,memo-save.request,memo-save.json,memo-conflict.status,memo-after.json,memo-persisted}
```

새 DB의 빈 메모는 revision `1`. `expectedRevision:"1"`로 `검증 메모`를 저장하면 `2`. 예전 revision으로 다른 본문을 보내면 HTTP 409이며 GET과 별도 DB 읽기에 최초 저장 본문만 남아야 한다.

## 함정

메모 저장은 LLM 번역의 품질 검사가 아니다. 대역 없는 X 수집·Gemini 호출이나 실제 방 PNG 발송은 하지 않는다. 행/변경 대응을 검토할 기록 입력은 `internal/planes/admin/internal/scheduleposter/testdata/{rows,changes}`에 있다. 궤도 카드가 이미지·시간 예산이나 참가자/태그 생략 조건을 만족하지 않으면 전체 일정이 `schedule.txt`로 전달된다. 검토 상태 전반을 다루는 TLA+ 모델은 없다.
