-- 텍스트 도움말(CMD_HELP)의 [방송] 구획에 !스케쥴 줄을 넣는다.
-- 009 적용 뒤 표준 본문(sha256으로 판정)만 바꾸고 사용자 지정 기본값과 채널 override는 그대로 둔다.
-- 반복 적용은 해시 불일치로 아무것도 바꾸지 않는다. 본문이 바뀐 행은 trigger가 row_version을 올려 렌더러 캐시가 새 본문을 읽는다.
UPDATE public.notification_templates AS target
SET body = replace(target.body, seed.anchor, seed.anchor || seed.line), updated_at = now()
FROM (VALUES
    ('plain', 'a3ee15861673511d23800d67e6bf4355f46fa707ce2f7ee30088132b3f02c058'),
    ('richtext', '84fe99c5c1dc4c4619ca147433000db4e23bff9ae72bd333e45c6f4344fa8a95')
) AS standard(format_variant, sha256_hex)
CROSS JOIN (VALUES (
    E'{{mdescape .Prefix}}방송시간 [멤버명] [N일] - 방송 시간 합계(기본 7일)\n',
    E'{{mdescape .Prefix}}스케쥴 [멤버명] - 이번 주 일정 포스터\n'
)) AS seed(anchor, line)
WHERE target.template_key = 'CMD_HELP'
  AND target.channel_id IS NULL
  AND target.format_variant = standard.format_variant
  AND encode(sha256(convert_to(target.body, 'UTF8')), 'hex') = standard.sha256_hex;
