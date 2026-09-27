-- live_snapshot current generation을 2(세션 메타데이터 포함)로 맞춘다. migration 144는 generation 1로 시드했고, 운영 DB는
-- youtube-collector runbook "Live metadata contract"의 활성화 2단계(승인된 internal operation)로 이미 2로 전환했다
-- (T18 2026-09-26: current generation 2, 미처리 generation 1 관측 0건). hololive-api decoder와 collector가 generation 1
-- 경로를 지웠으므로(PLN-20260926-stack-audit-refactoring T11 C6) 빈 DB bootstrap과 dbtest도 같은 계약을 갖게 한다.
-- 운영에서는 current_generation = 1 가드 때문에 갱신 대상이 없다. 번호는 운영 적용된 live-evidence 218~220과 부재 증거 보존 221 다음(222·223·224 뒤)이다.
UPDATE observation_contract_generations
SET current_generation = 2,
    updated_by = 'migration-225',
    updated_at = NOW()
WHERE observation_kind = 'live_snapshot'
  AND provider IN ('youtubejs', 'holodex')
  AND current_generation = 1;
