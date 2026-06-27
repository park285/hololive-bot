-- 040_unify_indie_org.sql
-- Holodex API org 값 "Independents"와 내부 org 통일
UPDATE members SET org = 'Independents' WHERE org = 'Indie';
COMMENT ON COLUMN members.org IS '소속 조직 (Hololive, Nijisanji, VSPO, Independents 등)';
