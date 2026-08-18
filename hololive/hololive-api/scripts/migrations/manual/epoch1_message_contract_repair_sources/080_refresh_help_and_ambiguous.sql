BEGIN;

UPDATE notification_templates SET body =
'🌸 홀로라이브 카카오톡 봇

📺 방송 확인
  {{.Prefix}}라이브 - 현재 라이브 중인 방송
  {{.Prefix}}라이브 [멤버명] - 특정 멤버 라이브 확인
  {{.Prefix}}예정 - 예정 방송 목록
  {{.Prefix}}예정 [멤버명] - 특정 멤버 예정 방송
  {{.Prefix}}멤버 [이름] - 일주일 이내의 방송일정을 조회

👤 멤버 정보
  {{.Prefix}}멤버 - 전체 멤버 목록
  {{.Prefix}}정보 [멤버명] - 멤버 프로필 조회

🔔 알람 설정
  {{.Prefix}}알람 추가 [멤버명]
  {{.Prefix}}알람 제거 [멤버명]
  {{.Prefix}}알람 목록
  {{.Prefix}}알람 초기화

📰 뉴스
  {{.Prefix}}뉴스 - 등록 멤버 주간 뉴스 요약
  {{.Prefix}}뉴스알림 켜기 / 끄기 / 상태

🎉 행사 알림
  {{.Prefix}}행사 - 행사 알림 상태
  {{.Prefix}}행사 켜기 / 끄기

📅 기념일
  {{.Prefix}}기념일 - 이번달 생일·주년
  {{.Prefix}}기념일 다음달 / 저번달

📊 통계
  {{.Prefix}}구독자 [멤버명] - 특정 멤버의 현재 구독자 수
  {{.Prefix}}구독자순위 - 최근 10일간 구독자 증가 순위 TOP 10
  {{.Prefix}}구독자순위 [기간]',
updated_at = now()
WHERE template_key = 'CMD_HELP' AND channel_id IS NULL;

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_AMBIGUOUS_MEMBER', NULL, '동일한 이름의 멤버가 여러 명 있습니다:

{{range .Candidates}}{{.Index}}. {{.Name}}
{{end}}
정확한 멤버를 지정하려면 다음과 같이 입력해주세요:
{{.Prefix}}{{.CommandExample}} {{.FirstName}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

COMMIT;
