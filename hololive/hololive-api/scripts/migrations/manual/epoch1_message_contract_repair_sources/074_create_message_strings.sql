CREATE TABLE IF NOT EXISTS message_strings (
    id         BIGSERIAL    PRIMARY KEY,
    namespace  VARCHAR(32)  NOT NULL,
    key        VARCHAR(64)  NOT NULL,
    value      TEXT         NOT NULL,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT ux_message_strings UNIQUE (namespace, key)
);

COMMENT ON TABLE message_strings IS '보간 없는 사용자-facing 룩업 문자열(이모지/라벨) - DB가 SSOT, 폴백 없음';

INSERT INTO message_strings(namespace, key, value) VALUES
  ('org','Hololive','Holo'),
  ('org','Nijisanji','니지산지'),
  ('org','Independents','개인세'),
  ('org','Stellive','스텔라이브'),
  ('alarmtype','LIVE','방송'),
  ('alarmtype','COMMUNITY','커뮤니티'),
  ('alarmtype','SHORTS','쇼츠'),
  ('alarmtype','BIRTHDAY','생일'),
  ('alarmtype','ANNIVERSARY','주년'),
  ('alarmtype','ALL','전체'),
  ('newscat','birthday_live','생일 라이브'),
  ('newscat','solo_live','솔로 라이브'),
  ('newscat','collab','콜라보'),
  ('newscat','event','이벤트'),
  ('newscat','goods','굿즈'),
  ('newscat','other','기타'),
  ('social','歌の再生リスト','음악 플레이리스트'),
  ('social','公式グッズ','공식 굿즈'),
  ('social','オフィシャルグッズ','공식 굿즈'),
  ('misc','vtuber_fallback','VTuber'),
  ('misc','chzzk_title','치지직 라이브')
ON CONFLICT (namespace, key) DO NOTHING;
