package sourceobservation

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collection"
)

// BenchmarkPublishSetLargeBatch는 배치 상한 근처(1,024행, 인코딩 약 7.8 MiB)에서 발행 SQL 한 번의 시간과
// Go 쪽 할당을 재는 수동 측정 도구다. 절반은 이미 저장된 관측(중복), 절반은 신규다. 측정 대상이 아닌 fence 확인과
// lease 종료는 빼고 발행 SQL만 트랜잭션에서 실행한 뒤 매번 되돌린다. 결과는 db-hotpath 계획
// (docs/current/plans/2026-10-05-db-hotpath-optimization.md)에 남긴다.
//
//	go test -run '^$' -bench BenchmarkPublishSetLargeBatch -benchtime 10x ./internal/runtime/sourceobservation
func BenchmarkPublishSetLargeBatch(b *testing.B) {
	ctx := b.Context()
	pool := dbtest.NewPool(b)
	proof := seedPublishLease(ctx, b, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, testChannelID, "community_collect")
	observations := make([]contract.Envelope, collection.MaxPublishBatchSize)
	entries := make([]CheckpointEntry, len(observations))

	for i := range observations {
		observations[i] = largePublishEnvelope(b, &proof, i)
		entries[i] = checkpointForEnvelope(&observations[i])
	}

	encode := func(observations []contract.Envelope, entries []CheckpointEntry) []byte {
		b.Helper()

		encoded, _, err := encodePublishBatch(&PublishBatchInput{
			Lease:        proof,
			Checkpoint:   CheckpointUpdate{Entries: entries, CollectionLatency: time.Second},
			Observations: observations,
		})
		require.NoError(b, err)

		return encoded
	}

	half := len(observations) / 2

	require.NoError(b, dbx.InPgxTx(ctx, pool, func(tx dbx.Tx) error {
		_, _, err := publishObservationSet(ctx, tx, encode(observations[:half], entries[:half]), half)

		return err
	}))

	encoded := encode(observations, entries)

	b.ReportAllocs()

	for b.Loop() {
		tx, err := pool.Begin(ctx)
		require.NoError(b, err)

		result, _, err := publishObservationSet(ctx, tx, encoded, len(observations))
		require.NoError(b, err)
		require.NoError(b, tx.Rollback(ctx))

		if result.Results[0].Outcome != PublishDuplicate || result.Results[half].Outcome != PublishInserted {
			b.Fatalf("outcomes = %s/%s, want duplicate/inserted", result.Results[0].Outcome, result.Results[half].Outcome)
		}
	}

	b.StopTimer()

	executionMS, storageKB := explainPublishSet(b, pool, encoded)

	b.ReportMetric(float64(len(encoded)), "encoded-bytes")
	b.ReportMetric(executionMS, "server-ms")
	b.ReportMetric(storageKB, "cte-storage-kB")
}

// explainPublishSet은 발행 SQL을 되돌리는 트랜잭션에서 한 번 실행해 서버 실행 시간과 materialized CTE가 보관한
// 최대 저장 크기(PG 18 EXPLAIN의 Maximum Storage) 합을 돌려준다.
func explainPublishSet(b *testing.B, pool *pgxpool.Pool, encoded []byte) (executionMS, storageKB float64) {
	b.Helper()

	ctx := b.Context()
	tx, err := pool.Begin(ctx)
	require.NoError(b, err)

	defer func() { require.NoError(b, tx.Rollback(context.WithoutCancel(ctx))) }()

	var raw []byte

	require.NoError(b, tx.QueryRow(ctx, "EXPLAIN (ANALYZE, TIMING OFF, FORMAT JSON) "+sqlPublishSet, string(encoded)).Scan(&raw))

	var plans []struct {
		Plan          publishSetPlanNode `json:"Plan"`
		ExecutionTime float64            `json:"Execution Time"`
	}

	require.NoError(b, jsonv2.Unmarshal(raw, &plans))
	require.Len(b, plans, 1)

	return plans[0].ExecutionTime, plans[0].Plan.storageKB()
}

type publishSetPlanNode struct {
	MaximumStorage float64              `json:"Maximum Storage"`
	Plans          []publishSetPlanNode `json:"Plans"`
}

func (n *publishSetPlanNode) storageKB() float64 {
	total := n.MaximumStorage

	for i := range n.Plans {
		total += n.Plans[i].storageKB()
	}

	return total
}

// largePublishEnvelope는 subject마다 다른 약 7 KB community 관측을 만든다.
func largePublishEnvelope(tb testing.TB, proof *contract.LeaseProof, ordinal int) contract.Envelope {
	tb.Helper()

	subject := fmt.Sprintf("UC_LARGE_%04d", ordinal)

	payload, err := contract.MarshalPayloadV1(contract.CommunityPayloadV1{
		ChannelID: subject,
		Posts: []contract.CommunityPostV1{{
			PostID: fmt.Sprintf("post-%04d", ordinal), ChannelID: subject,
			ContentText: strings.Repeat(string(rune('a'+ordinal%26)), 6800),
		}},
		Coverage: contract.CommunityPageCoverageV1{ChannelID: subject, MaxResults: 10, PageCount: 1, Exhausted: true},
	})
	require.NoError(tb, err)

	envelope, err := contract.PrepareEnvelope(contract.Envelope{
		Provider: contract.ProviderYouTubeJS, ObservationKind: contract.KindCommunityPage,
		SubjectKey: subject, SchemaVersion: contract.SchemaVersionV1, ContractGeneration: 1,
		ScheduledFor: proof.ScheduledFor, ObservedAt: proof.ScheduledFor.Add(time.Second),
		Completeness: contract.CompletenessComplete, Continuity: contract.ContinuityContiguous,
		Payload: payload, CollectorInstance: proof.OwnerInstance, Lease: *proof,
	})
	require.NoError(tb, err)

	return envelope
}
