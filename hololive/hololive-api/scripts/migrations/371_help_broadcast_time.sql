-- 텍스트 도움말(CMD_HELP)의 [방송] 묶음에 !방송시간 명령을 추가한다.
-- 직전 표준 기본값(sha256으로 판정)만 바꾸고 사용자 지정 기본값과 채널 override는 그대로 둔다.
-- 반복 적용은 해시 불일치로 아무것도 바꾸지 않는다. 본문이 바뀐 행은 trigger가 row_version을 올려 렌더러 캐시가 새 본문을 읽는다.
BEGIN;
CREATE TEMP TABLE help_broadcast_time_seed (
    template_key TEXT NOT NULL,
    format_variant TEXT NOT NULL,
    standard_sha256 TEXT NOT NULL,
    body TEXT NOT NULL,
    PRIMARY KEY (template_key, format_variant)
) ON COMMIT DROP;

INSERT INTO help_broadcast_time_seed (template_key, format_variant, standard_sha256, body) VALUES
    ('CMD_HELP', 'plain', '4848d2c3f724b853a4513d395f9ebe6b4f10d0b9ae2abdd5478dbc233a0c86dd', $body$홀로라이브 봇 명령어

[방송]
{{mdescape .Prefix}}라이브 - 방송 중 목록
{{mdescape .Prefix}}라이브 [멤버명] - 멤버 라이브 확인
{{mdescape .Prefix}}예정 - 예정 방송 목록
{{mdescape .Prefix}}예정 [멤버명] - 멤버 예정 방송
{{mdescape .Prefix}}멤버 [이름] - 일주일 이내 방송 일정
{{mdescape .Prefix}}방송시간 [멤버명] - 최근 방송 시각

[방송 이력]
{{mdescape .Prefix}}방송이력/방송기록 [멤버명] [타입] - 종료된 방송 이력
{{mdescape .Prefix}}방송이력 경마 30 - 최근 30일 경마
{{mdescape .Prefix}}방송기록 페코라 게임 - 멤버·타입 필터
{{mdescape .Prefix}}방송이력 카테고리:게임 14일 개수:10 - 타입·기간·개수
타입: 게임/잡담/노래/ASMR/멤버십/이벤트/경마/동시시청/뉴스/기타/미분류
{{mdescape .Prefix}}방송이력 썸네일 [video_id] - 종료 방송 썸네일

[멤버]
{{mdescape .Prefix}}멤버 - 전체 멤버 목록
{{mdescape .Prefix}}정보 [멤버명] - 프로필 조회

[알람]
{{mdescape .Prefix}}알람 추가 [멤버명]
{{mdescape .Prefix}}알람 제거 [멤버명]
{{mdescape .Prefix}}알람 목록
{{mdescape .Prefix}}알람 초기화

[뉴스]
{{mdescape .Prefix}}뉴스 - 주간 뉴스 요약
{{mdescape .Prefix}}뉴스알림 켜기 / 끄기 / 상태

[행사]
{{mdescape .Prefix}}행사 - 행사 알림 상태
{{mdescape .Prefix}}행사 켜기 / 끄기

[기념일]
{{mdescape .Prefix}}기념일 - 이번 달 생일·주년
{{mdescape .Prefix}}기념일 다음달 / 저번달

[기타]
{{mdescape .Prefix}}도움말 - 도움말$body$),
    ('CMD_HELP', 'richtext', '16b4cfe2ef9a1632b7124189f719b96f77883a186a3cacee2677972ba77b53c8', $body$**홀로라이브 봇 명령어**

**방송**
{{mdescape .Prefix}}라이브 - 방송 중 목록
{{mdescape .Prefix}}라이브 [멤버명] - 멤버 라이브 확인
{{mdescape .Prefix}}예정 - 예정 방송 목록
{{mdescape .Prefix}}예정 [멤버명] - 멤버 예정 방송
{{mdescape .Prefix}}멤버 [이름] - 일주일 이내 방송 일정
{{mdescape .Prefix}}방송시간 [멤버명] - 최근 방송 시각

**방송 이력**
{{mdescape .Prefix}}방송이력/방송기록 [멤버명] [타입] - 종료된 방송 이력
{{mdescape .Prefix}}방송이력 경마 30 - 최근 30일 경마
{{mdescape .Prefix}}방송기록 페코라 게임 - 멤버·타입 필터
{{mdescape .Prefix}}방송이력 카테고리:게임 14일 개수:10 - 타입·기간·개수
타입: 게임/잡담/노래/ASMR/멤버십/이벤트/경마/동시시청/뉴스/기타/미분류
{{mdescape .Prefix}}방송이력 썸네일 [video_id] - 종료 방송 썸네일

**멤버**
{{mdescape .Prefix}}멤버 - 전체 멤버 목록
{{mdescape .Prefix}}정보 [멤버명] - 프로필 조회

**알람**
{{mdescape .Prefix}}알람 추가 [멤버명]
{{mdescape .Prefix}}알람 제거 [멤버명]
{{mdescape .Prefix}}알람 목록
{{mdescape .Prefix}}알람 초기화

**뉴스**
{{mdescape .Prefix}}뉴스 - 주간 뉴스 요약
{{mdescape .Prefix}}뉴스알림 켜기 / 끄기 / 상태

**행사**
{{mdescape .Prefix}}행사 - 행사 알림 상태
{{mdescape .Prefix}}행사 켜기 / 끄기

**기념일**
{{mdescape .Prefix}}기념일 - 이번 달 생일·주년
{{mdescape .Prefix}}기념일 다음달 / 저번달

**기타**
{{mdescape .Prefix}}도움말 - 도움말$body$);

UPDATE notification_templates AS target
SET body = seed.body, updated_at = now()
FROM help_broadcast_time_seed AS seed
WHERE target.template_key = seed.template_key
  AND target.format_variant = seed.format_variant
  AND target.channel_id IS NULL
  AND encode(sha256(convert_to(target.body, 'UTF8')), 'hex') = seed.standard_sha256;
COMMIT;
