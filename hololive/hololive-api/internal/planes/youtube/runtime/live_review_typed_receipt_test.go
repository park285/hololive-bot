package runtime

import (
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
)

type liveReviewTypedCase struct {
	name       string
	before     []string
	facts      string
	wantClosed int64
}

// NULL 행과 object·key 집합의 차이는 형변환 뒤에도 남아야 합니다.
func liveReviewTypedShapeCases() []liveReviewTypedCase {
	return []liveReviewTypedCase{
		{
			name:   "absent_head_not_all_null_object",
			before: []string{`DELETE FROM youtube_live_reconciliation_heads`},
			facts:  `jsonb_set(current.facts,'{head}',youtube_live_review_semantic_facts('{"head":{}}'::jsonb)->'head')`,
		},
		{
			name:  "absent_pending_not_all_null_object",
			facts: `jsonb_set(current.facts,'{pending}',youtube_live_review_semantic_facts('{"pending":{}}'::jsonb)->'pending')`,
		},
		{
			name:  "missing_null_root_key",
			facts: `current.facts-'pending'`,
		},
		{
			name:  "missing_null_nested_key",
			facts: `current.facts#-'{session,ended_at}'`,
		},
		{
			name:  "extra_root_key",
			facts: `current.facts||'{"extra":null}'::jsonb`,
		},
		{
			name:  "extra_nested_key",
			facts: `jsonb_set(current.facts,'{head,extra}','null'::jsonb)`,
		},
	}
}

// 변환 가능한 JSON scalar라도 원래 타입이 다르면 같은 사실이 아닙니다.
func liveReviewTypedCoercionCases() []liveReviewTypedCase {
	return []liveReviewTypedCase{
		{
			name:  "numeric_string_not_number",
			facts: `jsonb_set(current.facts,'{head,consecutive_absence_slots}','"0"'::jsonb)`,
		},
		{
			name:   "boolean_string_not_boolean",
			before: []string{`UPDATE youtube_live_sessions SET is_premiere=false`},
			facts:  `jsonb_set(current.facts,'{session,is_premiere}','"false"'::jsonb)`,
		},
		{
			name:  "number_not_text",
			facts: `jsonb_set(current.facts,'{session,title}','123'::jsonb)`,
		},
	}
}

// 손상된 문서는 현재 의미 사실과 불일치할 뿐 조회 전체의 오류가 되지 않습니다.
func liveReviewTypedMalformedCases() []liveReviewTypedCase {
	return []liveReviewTypedCase{
		{
			name:  "malformed_root_array",
			facts: `jsonb_build_array(current.facts)`,
		},
		{
			name:  "malformed_root_scalar",
			facts: `'true'::jsonb`,
		},
		{
			name:  "malformed_root_null",
			facts: `'null'::jsonb`,
		},
		{
			name:  "malformed_nested_array",
			facts: `jsonb_set(current.facts,'{head}','[]'::jsonb)`,
		},
		{
			name:  "malformed_nested_scalar",
			facts: `jsonb_set(current.facts,'{pending}','"invalid"'::jsonb)`,
		},
		{
			name:  "malformed_boolean",
			facts: `jsonb_set(current.facts,'{session,is_premiere}','"not-a-boolean"'::jsonb)`,
		},
		{
			name:  "malformed_numeric_object",
			facts: `jsonb_set(current.facts,'{head,consecutive_absence_slots}','{"value":0}'::jsonb)`,
		},
		{
			name:  "malformed_epoch_string",
			facts: `jsonb_set(current.facts,'{session,started_at}','"not-a-number"'::jsonb)`,
		},
	}
}

// JSON 숫자의 scale·범위·무한대·정밀도는 원래 JSONB 비교와 같습니다.
func liveReviewTypedNumericCases() []liveReviewTypedCase {
	return []liveReviewTypedCase{
		{
			name:       "numeric_scale",
			facts:      `jsonb_set(current.facts,'{head,consecutive_absence_slots}','0.0'::jsonb)`,
			wantClosed: 1,
		},
		{
			name:  "out_of_source_range_numeric",
			facts: `jsonb_set(current.facts,'{head,last_absence_observation_id}','1e100'::jsonb)`,
		},
		{
			name: "infinite_epochs",
			before: []string{
				`UPDATE youtube_live_sessions SET started_at='infinity',live_first_seen_at='-infinity'`,
			},
			facts:      `current.facts`,
			wantClosed: 1,
		},
		{
			name: "large_exact_values",
			before: []string{
				`UPDATE youtube_live_sessions SET started_at='294000-01-01 00:00:00.000001+00'`,
				`UPDATE youtube_live_reconciliation_heads SET last_absence_observation_id=9223372036854775807`,
			},
			facts:      `current.facts`,
			wantClosed: 1,
		},
	}
}

// typed 영수증 비교는 JSONB의 숫자 동등성과 구조 차이를 그대로 보존해야 합니다.
func TestLiveReviewTypedReceiptsPreserveJSONBConsumerSemantics(t *testing.T) {
	pool := seedLiveReviewSourceSharing(t)
	insertLegacyFormatReceipt(t, pool, "50000000-0000-0000-0000-000000000001", "shared-review")

	for _, cases := range [][]liveReviewTypedCase{
		liveReviewTypedShapeCases(), liveReviewTypedCoercionCases(),
		liveReviewTypedMalformedCases(), liveReviewTypedNumericCases(),
	} {
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				tx, err := pool.Begin(t.Context())
				require.NoError(t, err)

				defer rollbackLiveReviewTx(t, tx)

				for _, statement := range tc.before {
					_, execErr := tx.Exec(t.Context(), statement)
					require.NoError(t, execErr)
				}

				// 불변 원본 영수증은 유지하고 rollback되는 트랜잭션 안에서 파생 사실만 교체합니다.
				// 현재 snapshot의 기존 JSON 의미 함수를 써 새 decoder를 oracle에 포함하지 않습니다.
				tag, err := tx.Exec(t.Context(), `
					TRUNCATE youtube_live_review_receipt_facts;
					INSERT INTO youtube_live_review_receipt_facts(receipt_id,video_id,recorded_at,facts)
					SELECT receipt.receipt_id,receipt.video_id,receipt.recorded_at,`+tc.facts+`
					FROM youtube_live_review_receipts receipt
					CROSS JOIN LATERAL (
					    SELECT youtube_live_review_semantic_facts(snapshot.original_snapshot) AS facts
					    FROM youtube_live_review_snapshot(receipt.video_id) snapshot
					) current
					WHERE receipt.receipt_id='50000000-0000-0000-0000-000000000001'`)
				require.NoError(t, err)
				require.EqualValues(t, 1, tag.RowsAffected())

				dbtest.RefreshAllLiveRuntimeState(t, tx)
				assertLiveReviewQuerySemanticEquivalence(t, tx, []string{"fresh"}, tc.wantClosed)
				assertLiveReviewFunctionSemanticEquivalence(t, tx)
			})
		}
	}
}

func assertLiveReviewFunctionSemanticEquivalence(t *testing.T, tx pgx.Tx) {
	t.Helper()

	assertLiveReviewRowsEqual(t, tx, `
		WITH actual AS (
		    SELECT video_id,facts,reviewable FROM youtube_live_review_current_facts(ARRAY['shared-review'])
		), expected AS (
		    SELECT 'shared-review'::text AS video_id,youtube_live_review_semantic_facts(original_snapshot) AS facts,reviewable
		    FROM youtube_live_review_snapshot('shared-review')
		)
		(SELECT video_id,facts,reviewable FROM actual EXCEPT ALL SELECT video_id,facts,reviewable FROM expected)
		UNION ALL
		(SELECT video_id,facts,reviewable FROM expected EXCEPT ALL SELECT video_id,facts,reviewable FROM actual)`)

	assertLiveReviewRowsEqual(t, tx, `WITH `+liveReviewSourceOracleSQL+`,
		actual AS (SELECT video_id,reviewed_at FROM youtube_live_review_current_receipts(ARRAY['shared-review'])),
		expected AS (SELECT video_id,reviewed_at FROM oracle_states WHERE reviewed_at IS NOT NULL)
		(SELECT video_id,reviewed_at FROM actual EXCEPT ALL SELECT video_id,reviewed_at FROM expected)
		UNION ALL
		(SELECT video_id,reviewed_at FROM expected EXCEPT ALL SELECT video_id,reviewed_at FROM actual)`)

	assertLiveReviewRowsEqual(t, tx, `WITH `+liveReviewSourceOracleSQL+`,
		actual AS (SELECT reviewed_at FROM youtube_live_review_current_receipt('shared-review')),
		expected AS (SELECT reviewed_at FROM oracle_states)
		(SELECT reviewed_at FROM actual EXCEPT ALL SELECT reviewed_at FROM expected)
		UNION ALL
		(SELECT reviewed_at FROM expected EXCEPT ALL SELECT reviewed_at FROM actual)`)
}
