# Phase 04. Parser drift raw fixture capture

## 목표

parser drift가 발생했을 때 raw HTML 일부를 artifact로 남기고, 이 파일을 parser fixture test로 바로 승격할 수 있게 합니다.

## 코드 레벨 의사결정

1. snapshot은 fetcher 레벨이 아니라 operation 레벨에서 저장합니다.
   - fetcher는 URL과 HTML만 알 수 있습니다.
   - operation/stage/reason은 `GetUpcomingEvents`, `GetShorts`, `GetCommunityPosts` 같은 함수만 압니다.

2. snapshot은 기본 OFF입니다.
   - 운영 디스크 보호를 위해 config로 켭니다.

3. 반드시 제한을 둡니다.
   - reason 제한: parser_drift, empty_response 중심
   - body size 제한: 기본 512KiB
   - interval 제한: 같은 operation/channel/stage/reason은 30분에 1회
   - filename safe 처리
   - dedupe hash 포함

## 변경 대상

- `scraper/snapshot.go` 신규
- `scraper/snapshot_file_sink.go` 신규
- `scraper/snapshot_capture.go` 신규
- `scraper/client_options.go` 수정
- `scraper/client_operation_guard.go` 수정

## Diff

```diff
diff --git a/hololive/hololive-shared/pkg/service/youtube/scraper/snapshot.go b/hololive/hololive-shared/pkg/service/youtube/scraper/snapshot.go
new file mode 100644
index 0000000..7777777
--- /dev/null
+++ b/hololive/hololive-shared/pkg/service/youtube/scraper/snapshot.go
@@
+package scraper
+
+import (
+	"context"
+	"crypto/sha256"
+	"encoding/hex"
+	"strings"
+	"time"
+)
+
+type Snapshot struct {
+	Operation  string
+	ChannelID  string
+	URL        string
+	Source     FailureSource
+	Reason     FailureReason
+	Stage      string
+	StatusCode int
+	Body       []byte
+	CapturedAt time.Time
+}
+
+type SnapshotSink interface {
+	Capture(ctx context.Context, snapshot Snapshot) error
+}
+
+type SnapshotPolicy struct {
+	Enabled        bool
+	MaxBodyBytes   int
+	MinInterval    time.Duration
+	AllowedReasons map[FailureReason]bool
+}
+
+func DefaultSnapshotPolicy() SnapshotPolicy {
+	return SnapshotPolicy{
+		Enabled:      false,
+		MaxBodyBytes: 512 << 10,
+		MinInterval:  30 * time.Minute,
+		AllowedReasons: map[FailureReason]bool{
+			FailureReasonParserDrift:   true,
+			FailureReasonEmptyResponse: true,
+		},
+	}
+}
+
+func (p SnapshotPolicy) allows(reason FailureReason) bool {
+	if !p.Enabled {
+		return false
+	}
+	if len(p.AllowedReasons) == 0 {
+		return true
+	}
+	return p.AllowedReasons[reason]
+}
+
+func trimSnapshotBody(body string, maxBytes int) []byte {
+	body = strings.TrimSpace(body)
+	if body == "" {
+		return nil
+	}
+	raw := []byte(body)
+	if maxBytes > 0 && len(raw) > maxBytes {
+		return raw[:maxBytes]
+	}
+	return raw
+}
+
+func SnapshotID(snapshot Snapshot) string {
+	sum := sha256.Sum256([]byte(snapshot.Operation + "\n" + snapshot.ChannelID + "\n" + snapshot.Stage + "\n" + string(snapshot.Body)))
+	return hex.EncodeToString(sum[:])
+}
```

```diff
diff --git a/hololive/hololive-shared/pkg/service/youtube/scraper/snapshot_file_sink.go b/hololive/hololive-shared/pkg/service/youtube/scraper/snapshot_file_sink.go
new file mode 100644
index 0000000..8888888
--- /dev/null
+++ b/hololive/hololive-shared/pkg/service/youtube/scraper/snapshot_file_sink.go
@@
+package scraper
+
+import (
+	"context"
+	"fmt"
+	"os"
+	"path/filepath"
+	"strings"
+)
+
+type FileSnapshotSink struct {
+	Dir string
+}
+
+func NewFileSnapshotSink(dir string) FileSnapshotSink {
+	return FileSnapshotSink{Dir: dir}
+}
+
+func (s FileSnapshotSink) Capture(ctx context.Context, snapshot Snapshot) error {
+	if strings.TrimSpace(s.Dir) == "" {
+		return nil
+	}
+	select {
+	case <-ctx.Done():
+		return ctx.Err()
+	default:
+	}
+
+	if snapshot.CapturedAt.IsZero() {
+		return nil
+	}
+
+	id := SnapshotID(snapshot)
+	date := snapshot.CapturedAt.UTC().Format("20060102")
+	name := fmt.Sprintf(
+		"%s_%s_%s_%s_%s.html",
+		date,
+		safeFilePart(snapshot.Operation),
+		safeFilePart(snapshot.ChannelID),
+		safeFilePart(snapshot.Stage),
+		id[:12],
+	)
+
+	dir := filepath.Join(s.Dir, date)
+	if err := os.MkdirAll(dir, 0o755); err != nil {
+		return err
+	}
+	return os.WriteFile(filepath.Join(dir, name), snapshot.Body, 0o644)
+}
+
+func safeFilePart(value string) string {
+	value = strings.TrimSpace(value)
+	if value == "" {
+		return "unknown"
+	}
+	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", " ", "_")
+	return replacer.Replace(value)
+}
```

```diff
diff --git a/hololive/hololive-shared/pkg/service/youtube/scraper/snapshot_capture.go b/hololive/hololive-shared/pkg/service/youtube/scraper/snapshot_capture.go
new file mode 100644
index 0000000..9999999
--- /dev/null
+++ b/hololive/hololive-shared/pkg/service/youtube/scraper/snapshot_capture.go
@@
+package scraper
+
+import (
+	"context"
+	"fmt"
+	"log/slog"
+	"strings"
+	"time"
+)
+
+func (c *Client) captureSnapshot(ctx context.Context, snapshot Snapshot) {
+	if c == nil || c.snapshotSink == nil {
+		return
+	}
+	policy := c.snapshotPolicy
+	if !policy.allows(snapshot.Reason) {
+		return
+	}
+	if snapshot.CapturedAt.IsZero() {
+		snapshot.CapturedAt = time.Now().UTC()
+	}
+	if policy.MaxBodyBytes > 0 && len(snapshot.Body) > policy.MaxBodyBytes {
+		snapshot.Body = snapshot.Body[:policy.MaxBodyBytes]
+	}
+	if len(snapshot.Body) == 0 {
+		return
+	}
+	if !c.allowSnapshotInterval(ctx, snapshot, policy.MinInterval) {
+		return
+	}
+	if err := c.snapshotSink.Capture(ctx, snapshot); err != nil {
+		slog.Warn("failed to capture youtube scraper snapshot",
+			"operation", snapshot.Operation,
+			"channel_id", snapshot.ChannelID,
+			"source", snapshot.Source,
+			"reason", snapshot.Reason,
+			"stage", snapshot.Stage,
+			"error", err)
+	}
+}
+
+func (c *Client) allowSnapshotInterval(ctx context.Context, snapshot Snapshot, interval time.Duration) bool {
+	if interval <= 0 || c == nil || c.stateStore == nil {
+		return true
+	}
+	key := snapshotIntervalStateKey(snapshot)
+	var marker bool
+	if err := c.stateStore.Get(ctx, key, &marker); err == nil && marker {
+		return false
+	}
+	if err := c.stateStore.Set(ctx, key, true, interval); err != nil {
+		slog.Warn("failed to persist youtube scraper snapshot interval marker",
+			"key", key,
+			"error", err)
+	}
+	return true
+}
+
+func snapshotIntervalStateKey(snapshot Snapshot) string {
+	return fmt.Sprintf(
+		"youtube:scraper:snapshot-interval:%s:%s:%s:%s",
+		strings.TrimSpace(snapshot.Operation),
+		strings.TrimSpace(snapshot.ChannelID),
+		strings.TrimSpace(snapshot.Stage),
+		strings.TrimSpace(string(snapshot.Reason)),
+	)
+}
```

```diff
diff --git a/hololive/hololive-shared/pkg/service/youtube/scraper/client_options.go b/hololive/hololive-shared/pkg/service/youtube/scraper/client_options.go
index ccccccc..aaa4444 100644
--- a/hololive/hololive-shared/pkg/service/youtube/scraper/client_options.go
+++ b/hololive/hololive-shared/pkg/service/youtube/scraper/client_options.go
@@
 	fetcherEngine    FetcherEngine
 	channelHealthPolicy ChannelHealthPolicy
 	channelHealth    *ChannelHealthStore
+	snapshotSink     SnapshotSink
+	snapshotPolicy   SnapshotPolicy
@@
 func WithChannelHealthPolicy(policy ChannelHealthPolicy) ClientOption {
 	return func(c *Client) {
 		c.channelHealthPolicy = policy
 	}
 }
+
+func WithSnapshotSink(sink SnapshotSink) ClientOption {
+	return func(c *Client) {
+		c.snapshotSink = sink
+	}
+}
+
+func WithSnapshotPolicy(policy SnapshotPolicy) ClientOption {
+	return func(c *Client) {
+		c.snapshotPolicy = policy
+	}
+}
@@
 		uaProvider:    ua.NewRotatingProvider(ua.StrategySessionTTL, 45*time.Minute),
 		rateLimiter:   NewRateLimiter(3 * time.Second),
 		backoffState:  NewBackoffState(),
 		fetcherEngine: FetcherEngineNetHTTP,
+		snapshotPolicy: DefaultSnapshotPolicy(),
 	}
```

```diff
diff --git a/hololive/hololive-shared/pkg/service/youtube/scraper/client_operation_guard.go b/hololive/hololive-shared/pkg/service/youtube/scraper/client_operation_guard.go
index 5555555..bbb4444 100644
--- a/hololive/hololive-shared/pkg/service/youtube/scraper/client_operation_guard.go
+++ b/hololive/hololive-shared/pkg/service/youtube/scraper/client_operation_guard.go
@@
 func (c *Client) recordParserDrift(
 	ctx context.Context,
 	operation string,
@@
 ) error {
 	err := NewParserDriftError(operation, stage, cause)
 	detail := ClassifyFailure(err, source)
+	c.captureSnapshot(ctx, Snapshot{
+		Operation:  operation,
+		ChannelID:  channelID,
+		URL:        pageURL,
+		Source:     source,
+		Reason:     detail.Reason,
+		Stage:      stage,
+		StatusCode: detail.StatusCode,
+		Body:       trimSnapshotBody(html, c.snapshotPolicy.MaxBodyBytes),
+		CapturedAt: time.Now().UTC(),
+	})
 	c.recordChannelSourceFailure(ctx, channelID, detail)
 	return err
 }
```

## 테스트 추가

```diff
diff --git a/hololive/hololive-shared/pkg/service/youtube/scraper/snapshot_test.go b/hololive/hololive-shared/pkg/service/youtube/scraper/snapshot_test.go
new file mode 100644
index 0000000..ccc4444
--- /dev/null
+++ b/hololive/hololive-shared/pkg/service/youtube/scraper/snapshot_test.go
@@
+package scraper
+
+import (
+	"strings"
+	"testing"
+
+	"github.com/stretchr/testify/require"
+)
+
+func TestTrimSnapshotBody(t *testing.T) {
+	body := strings.Repeat("a", 1024)
+	got := trimSnapshotBody(body, 128)
+	require.Len(t, got, 128)
+}
+
+func TestSnapshotPolicyAllowsOnlyConfiguredReason(t *testing.T) {
+	policy := SnapshotPolicy{
+		Enabled: true,
+		AllowedReasons: map[FailureReason]bool{
+			FailureReasonParserDrift: true,
+		},
+	}
+	require.True(t, policy.allows(FailureReasonParserDrift))
+	require.False(t, policy.allows(FailureReasonTransport))
+}
+
+func TestSnapshotIDStable(t *testing.T) {
+	snapshot := Snapshot{
+		Operation: "upcoming_events",
+		ChannelID: "UCxxx",
+		Stage: "extract_yt_initial_data",
+		Body: []byte("<html></html>"),
+	}
+	require.Equal(t, SnapshotID(snapshot), SnapshotID(snapshot))
+}
```

## 실행

```bash
go test ./hololive/hololive-shared/pkg/service/youtube/scraper -run 'TestSnapshot|TestTrim'
```

## 완료 기준

- parser drift 발생 시 snapshot sink가 설정된 경우 HTML 일부가 저장됩니다.
- snapshot 저장은 reason/size/interval 제한을 받습니다.
- snapshot 저장 실패는 scraper 실패로 전파되지 않습니다.
- 저장 파일명은 channelID, operation, stage, hash를 포함합니다.
