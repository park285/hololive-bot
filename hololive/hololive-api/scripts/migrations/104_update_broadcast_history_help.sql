BEGIN;

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_HELP', NULL, '홀로라이브 봇 명령어

[방송]
  {{.Prefix}}라이브 - 방송 중 목록
  {{.Prefix}}라이브 [멤버명] - 멤버 라이브 확인
  {{.Prefix}}예정 - 예정 방송 목록
  {{.Prefix}}예정 [멤버명] - 멤버 예정 방송
  {{.Prefix}}멤버 [이름] - 일주일 이내 방송 일정
  {{.Prefix}}방송이력 [멤버명] - 종료된 방송 이력
  {{.Prefix}}방송이력 경마 30 - 최근 30일 경마
  {{.Prefix}}방송이력 카테고리:게임 14일 개수:10 - 타입·기간·개수
  타입: 게임/잡담/노래/ASMR/멤버십/멤버/이벤트/경마/동시시청/뉴스/기타/미분류
  {{.Prefix}}방송이력 썸네일 [video_id] - 종료 방송 썸네일

[멤버]
  {{.Prefix}}멤버 - 전체 멤버 목록
  {{.Prefix}}정보 [멤버명] - 프로필 조회

[알람]
  {{.Prefix}}알람 추가 [멤버명]
  {{.Prefix}}알람 제거 [멤버명]
  {{.Prefix}}알람 목록
  {{.Prefix}}알람 초기화

[뉴스]
  {{.Prefix}}뉴스 - 주간 뉴스 요약
  {{.Prefix}}뉴스알림 켜기 / 끄기 / 상태

[행사]
  {{.Prefix}}행사 - 행사 알림 상태
  {{.Prefix}}행사 켜기 / 끄기

[기념일]
  {{.Prefix}}기념일 - 이번 달 생일·주년
  {{.Prefix}}기념일 다음달 / 저번달

[기타]
  {{.Prefix}}구독자 [멤버명] - 구독자 수
  {{.Prefix}}도움말 - 도움말')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

COMMIT;
