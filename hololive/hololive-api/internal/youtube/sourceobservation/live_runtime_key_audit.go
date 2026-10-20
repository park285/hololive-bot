package sourceobservation

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

// liveEndFinalizerKeyAuditPath는 관측 없이 due 종료 후보를 확정하는 writer의 지표 path다.
// 관측 확정 writer의 path는 관측 kind 문자열이다.
const liveEndFinalizerKeyAuditPath = "live_end_finalizer"

var liveRuntimeKeyAuditSQL = mustSQL("repository_live_runtime_key_audit.sql")

var (
	liveRuntimeKeyAuditTriggerKeysTotal = precreateLiveRuntimeKeyAudit(promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "hololive_youtube_live_runtime_key_audit_trigger_keys_total",
		Help: "Live runtime state keys refreshed by source triggers in committed transactions whose Go writer keys were audited, by writer path.",
	}, []string{"path"}))
	liveRuntimeKeyAuditMissingTotal = precreateLiveRuntimeKeyAudit(promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "hololive_youtube_live_runtime_key_audit_missing_total",
		Help: "Live runtime state keys refreshed by source triggers but not collected by the Go writer in the same committed transaction, by writer path.",
	}, []string{"path"}))
	liveRuntimeKeyAuditExtraTotal = precreateLiveRuntimeKeyAudit(promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "hololive_youtube_live_runtime_key_audit_extra_total",
		Help: "Live runtime state keys collected by the Go writer that source triggers did not refresh in the same committed transaction, by writer path.",
	}, []string{"path"}))
)

// precreateLiveRuntimeKeyAudit는 모든 path의 0 시리즈를 등록 시점에 만든다.
// 첫 증가에 시리즈가 생기면 Prometheus가 그 샘플을 기준점으로 삼아 첫 사건을 0으로 센다.
func precreateLiveRuntimeKeyAudit(vec *prometheus.CounterVec) *prometheus.CounterVec {
	for _, path := range []string{
		string(contract.KindCommunityPage), string(contract.KindVideoList), string(contract.KindShortsList),
		string(contract.KindLiveSnapshot), string(contract.KindChannelProfile), string(contract.KindChannelPhoto),
		string(contract.KindSchedule), string(contract.KindChannelLiveCheck), string(contract.KindVideoLiveCheck),
		liveEndFinalizerKeyAuditPath,
	} {
		vec.WithLabelValues(path)
	}

	return vec
}

// liveRuntimeKeyAudit는 한 writer 트랜잭션의 key 비교 결과다. 비교하지 않은 트랜잭션은 빈 path를 가진다.
type liveRuntimeKeyAudit struct {
	path        string
	triggerKeys int64
	missing     int64
	extra       int64
}

// auditLiveRuntimeKeys는 원본 문장 트리거가 이 트랜잭션에서 refresh한 수명 모델 key(hololive.live_runtime_trigger_keys)와
// writer가 모은 videoIDs를 비교한다. 원본 쓰기가 모두 끝난 뒤 같은 트랜잭션에서 호출하고, 결과는 커밋 뒤 record로 남긴다.
// 수명 모델은 여전히 트리거가 갱신하므로 비교 결과는 저장 동작을 바꾸지 않는다.
func auditLiveRuntimeKeys(ctx context.Context, tx dbx.Tx, path string, videoIDs []string) (liveRuntimeKeyAudit, error) {
	audit := liveRuntimeKeyAudit{path: path}

	if err := tx.QueryRow(ctx, liveRuntimeKeyAuditSQL, videoIDs).Scan(&audit.triggerKeys, &audit.missing, &audit.extra); err != nil {
		return liveRuntimeKeyAudit{}, fmt.Errorf("audit youtube live runtime keys: %w", err)
	}

	return audit, nil
}

// record는 커밋된 트랜잭션의 비교 결과를 path별 지표로 센다. 롤백·재시도된 시도는 호출자가 넘기지 않는다.
func (a liveRuntimeKeyAudit) record(ctx context.Context) {
	if a.path == "" {
		return
	}

	liveRuntimeKeyAuditTriggerKeysTotal.WithLabelValues(a.path).Add(float64(a.triggerKeys))
	liveRuntimeKeyAuditMissingTotal.WithLabelValues(a.path).Add(float64(a.missing))
	liveRuntimeKeyAuditExtraTotal.WithLabelValues(a.path).Add(float64(a.extra))

	if a.missing > 0 {
		slog.ErrorContext(ctx, "youtube live runtime writer missed trigger-refreshed keys",
			slog.String("path", a.path), slog.Int64("missing", a.missing))
	}
}
