from __future__ import annotations

from pathlib import Path

ROOT = Path.cwd()


def replace_once(path: str, old: str, new: str) -> None:
    target = ROOT / path
    text = target.read_text(encoding="utf-8")
    count = text.count(old)
    if count != 1:
        raise RuntimeError(f"{path}: expected one replacement target, found {count}")
    target.write_text(text.replace(old, new, 1), encoding="utf-8")


def append_text(path: str, content: str) -> None:
    target = ROOT / path
    text = target.read_text(encoding="utf-8")
    if content.strip() in text:
        raise RuntimeError(f"{path}: append content already present")
    target.write_text(text.rstrip() + "\n\n" + content.strip() + "\n", encoding="utf-8")


def write(path: str, content: str) -> None:
    target = ROOT / path
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text(content, encoding="utf-8")


# 1. Restore server-enforced workflow policy tests and full Go verification.
replace_once(
    ".github/workflows/ci.yml",
    """      - name: Workflow PR boundary and gate ownership
        run: bash scripts/ci/check-workflow-secrets.sh

      - name: Setup Go
""",
    """      - name: Workflow PR boundary and gate ownership
        run: bash scripts/ci/check-workflow-secrets.sh

      - name: Workflow policy regression tests
        run: bash scripts/ci/check-workflow-secrets_test.sh

      - name: Setup Go
""",
)
append_text(
    ".github/workflows/ci.yml",
    r'''
  full-go-tests:
    name: full-go-tests
    runs-on: ubuntu-latest
    timeout-minutes: 45
    steps:
      - name: Checkout hololive-bot
        uses: actions/checkout@9c091bb21b7c1c1d1991bb908d89e4e9dddfe3e0 # v7.0.0
        with:
          path: hololive-bot
          persist-credentials: false

      - name: Checkout shared-go
        uses: actions/checkout@9c091bb21b7c1c1d1991bb908d89e4e9dddfe3e0 # v7.0.0
        with:
          repository: park285/shared-go
          path: shared-go
          persist-credentials: false

      - name: Checkout iris-client-go
        uses: actions/checkout@9c091bb21b7c1c1d1991bb908d89e4e9dddfe3e0 # v7.0.0
        with:
          repository: park285/iris-client-go
          path: iris-client-go
          persist-credentials: false

      - name: Setup Go
        uses: actions/setup-go@924ae3a1cded613372ab5595356fb5720e22ba16 # v6.5.0
        with:
          go-version-file: hololive-bot/go.mod
          cache-dependency-path: |
            hololive-bot/go.work.sum
            hololive-bot/go.sum
            hololive-bot/admin-dashboard/backend/go.sum
            hololive-bot/hololive/*/go.sum
            shared-go/go.sum
            iris-client-go/go.sum

      - name: Run full workspace tests and vet
        working-directory: hololive-bot
        run: |
          source scripts/ci/go-workspace-modules.sh
          mapfile -t packages < <(
            {
              printf './...\n'
              go_workspace_package_patterns | sed 's#^\./\.\./#../#'
            } | awk 'NF'
          )
          printf 'workspace packages:\n%s\n' "${packages[*]}"
          go test -count=1 "${packages[@]}"
          go vet "${packages[@]}"
''',
)
replace_once(
    ".github/workflows/security.yml",
    """on:
  push:
""",
    """on:
  pull_request:
  push:
""",
)

# 2. Tie calendar photo work to the command context while keeping the legacy API.
replace_once(
    "hololive/hololive-api/internal/planes/bot/internal/render/calendar.go",
    'import (\n\t"bytes"\n',
    'import (\n\t"bytes"\n\t"context"\n',
)
replace_once(
    "hololive/hololive-api/internal/planes/bot/internal/render/calendar.go",
    '''func (r *CalendarCardRenderer) RenderCalendarImage(month, year int, entries []domain.CalendarEntry) ([]byte, error) {
\tcacheKey := newCalendarCacheKey(month, year, entries)
\tif data, ok := r.cachedImage(cacheKey); ok {
\t\treturn data, nil
\t}

\tresult, err, _ := r.rendering.Do(cacheKey.string(), func() (any, error) {
\t\treturn r.renderCalendarImageOnce(cacheKey, month, year, entries)
\t})
\tif err != nil {
\t\treturn nil, err
\t}
\tdata, ok := result.([]byte)
\tif !ok {
\t\treturn nil, fmt.Errorf("calendar render cache returned %T", result)
\t}
\treturn bytes.Clone(data), nil
}
''',
    '''func (r *CalendarCardRenderer) RenderCalendarImage(month, year int, entries []domain.CalendarEntry) ([]byte, error) {
\treturn r.RenderCalendarImageContext(context.Background(), month, year, entries)
}

func (r *CalendarCardRenderer) RenderCalendarImageContext(ctx context.Context, month, year int, entries []domain.CalendarEntry) ([]byte, error) {
\tif ctx == nil {
\t\tctx = context.Background()
\t}
\tcacheKey := newCalendarCacheKey(month, year, entries)
\tif data, ok := r.cachedImage(cacheKey); ok {
\t\treturn data, nil
\t}

\tresult, err, _ := r.rendering.Do(cacheKey.string(), func() (any, error) {
\t\treturn r.renderCalendarImageOnce(ctx, cacheKey, month, year, entries)
\t})
\tif err != nil {
\t\treturn nil, err
\t}
\tdata, ok := result.([]byte)
\tif !ok {
\t\treturn nil, fmt.Errorf("calendar render cache returned %T", result)
\t}
\treturn bytes.Clone(data), nil
}
''',
)
replace_once(
    "hololive/hololive-api/internal/planes/bot/internal/render/calendar.go",
    "func (r *CalendarCardRenderer) renderCalendarImageOnce(cacheKey calendarCacheKey, month, year int, entries []domain.CalendarEntry) (any, error) {",
    "func (r *CalendarCardRenderer) renderCalendarImageOnce(ctx context.Context, cacheKey calendarCacheKey, month, year int, entries []domain.CalendarEntry) (any, error) {",
)
replace_once(
    "hololive/hololive-api/internal/planes/bot/internal/render/calendar.go",
    "data, diskCacheable, err := r.renderCalendarImage(month, year, entries)",
    "data, diskCacheable, err := r.renderCalendarImage(ctx, month, year, entries)",
)
replace_once(
    "hololive/hololive-api/internal/planes/bot/internal/render/calendar.go",
    "func (r *CalendarCardRenderer) renderCalendarImage(month, year int, entries []domain.CalendarEntry) (data []byte, diskCacheable bool, err error) {\n\tphotos, diskCacheable := fetchMemberPhotos(entries)",
    "func (r *CalendarCardRenderer) renderCalendarImage(ctx context.Context, month, year int, entries []domain.CalendarEntry) (data []byte, diskCacheable bool, err error) {\n\tphotos, diskCacheable := fetchMemberPhotos(ctx, entries)",
)
replace_once(
    "hololive/hololive-api/internal/planes/bot/internal/command/handlers/handlercore/command.go",
    "\tRenderCalendarImage(month, year int, entries []domain.CalendarEntry) ([]byte, error)",
    "\tRenderCalendarImageContext(ctx context.Context, month, year int, entries []domain.CalendarEntry) ([]byte, error)",
)
replace_once(
    "hololive/hololive-api/internal/planes/bot/internal/command/handlers/handler_calendar.go",
    "data, err := c.imageRenderer.RenderCalendarImage(month, year, entries)",
    "data, err := c.imageRenderer.RenderCalendarImageContext(ctx, month, year, entries)",
)
replace_once(
    "hololive/hololive-api/internal/planes/bot/internal/command/handlers/handler_calendar_test.go",
    "func (s *calendarImageRendererStub) RenderCalendarImage(_, _ int, _ []domain.CalendarEntry) ([]byte, error) {",
    "func (s *calendarImageRendererStub) RenderCalendarImageContext(_ context.Context, _, _ int, _ []domain.CalendarEntry) ([]byte, error) {",
)
replace_once(
    "hololive/hololive-api/internal/planes/bot/internal/render/calendar_photos.go",
    '''\tcalendarPhotoThumbnailSize = 1024
\tcalendarPhotoMaxBytes      = 2 << 20
''',
    '''\tcalendarPhotoThumbnailSize = 1024
\tcalendarPhotoMaxBytes      = 2 << 20
\tcalendarPhotoMaxDimension  = 4096
\tcalendarPhotoMaxPixels     = 8 << 20
''',
)
replace_once(
    "hololive/hololive-api/internal/planes/bot/internal/render/calendar_photos.go",
    '''func fetchMemberPhotos(entries []domain.CalendarEntry) (map[string]image.Image, bool) {
\tctx, cancel := context.WithTimeout(context.Background(), photoFetchBudget)
''',
    '''func fetchMemberPhotos(parent context.Context, entries []domain.CalendarEntry) (map[string]image.Image, bool) {
\tif parent == nil {
\t\tparent = context.Background()
\t}
\tctx, cancel := context.WithTimeout(parent, photoFetchBudget)
''',
)
replace_once(
    "hololive/hololive-api/internal/planes/bot/internal/render/calendar_photos.go",
    '''func decodeCalendarPhoto(data []byte, contentType string) (image.Image, error) {
\tswitch contentType {
\tcase "image/png":
\t\treturn png.Decode(bytes.NewReader(data))
\tcase "image/jpeg":
\t\treturn jpeg.Decode(bytes.NewReader(data))
\tcase "image/webp":
\t\treturn webp.Decode(bytes.NewReader(data))
\tdefault:
\t\treturn nil, fmt.Errorf("unsupported image format")
\t}
}
''',
    '''func decodeCalendarPhoto(data []byte, contentType string) (image.Image, error) {
\tconfig, err := decodeCalendarPhotoConfig(data, contentType)
\tif err != nil {
\t\treturn nil, err
\t}
\tif err := validateCalendarPhotoConfig(config); err != nil {
\t\treturn nil, err
\t}

\tswitch contentType {
\tcase "image/png":
\t\treturn png.Decode(bytes.NewReader(data))
\tcase "image/jpeg":
\t\treturn jpeg.Decode(bytes.NewReader(data))
\tcase "image/webp":
\t\treturn webp.Decode(bytes.NewReader(data))
\tdefault:
\t\treturn nil, fmt.Errorf("unsupported image format")
\t}
}

func decodeCalendarPhotoConfig(data []byte, contentType string) (image.Config, error) {
\treader := bytes.NewReader(data)
\tswitch contentType {
\tcase "image/png":
\t\treturn png.DecodeConfig(reader)
\tcase "image/jpeg":
\t\treturn jpeg.DecodeConfig(reader)
\tcase "image/webp":
\t\treturn webp.DecodeConfig(reader)
\tdefault:
\t\treturn image.Config{}, fmt.Errorf("unsupported image format")
\t}
}

func validateCalendarPhotoConfig(config image.Config) error {
\tif config.Width <= 0 || config.Height <= 0 {
\t\treturn fmt.Errorf("calendar photo has invalid dimensions %dx%d", config.Width, config.Height)
\t}
\tif config.Width > calendarPhotoMaxDimension || config.Height > calendarPhotoMaxDimension {
\t\treturn fmt.Errorf("calendar photo dimensions %dx%d exceed %d", config.Width, config.Height, calendarPhotoMaxDimension)
\t}
\tpixels := uint64(config.Width) * uint64(config.Height)
\tif pixels > calendarPhotoMaxPixels {
\t\treturn fmt.Errorf("calendar photo pixel count %d exceeds %d", pixels, calendarPhotoMaxPixels)
\t}
\treturn nil
}
''',
)
append_text(
    "hololive/hololive-api/internal/planes/bot/internal/render/calendar_photo_security_test.go",
    r'''
func TestValidateCalendarPhotoConfigRejectsImageBombDimensions(t *testing.T) {
\tt.Parallel()

\ttests := []struct {
\t\tname   string
\t\tconfig image.Config
\t}{
\t\t{name: "zero width", config: image.Config{Width: 0, Height: 1}},
\t\t{name: "dimension limit", config: image.Config{Width: calendarPhotoMaxDimension + 1, Height: 1}},
\t\t{name: "pixel limit", config: image.Config{Width: calendarPhotoMaxDimension, Height: 3000}},
\t}
\tfor _, tt := range tests {
\t\tt.Run(tt.name, func(t *testing.T) {
\t\t\tt.Parallel()
\t\t\tif err := validateCalendarPhotoConfig(tt.config); err == nil {
\t\t\t\tt.Fatalf("validateCalendarPhotoConfig(%+v) error = nil", tt.config)
\t\t\t}
\t\t})
\t}
}

func TestCalendarCardRendererContextCancellationStopsPhotoFetch(t *testing.T) {
\tphotoURL := "https://yt3.googleusercontent.com/avatar=s88-c"
\trecorder := &calledRoundTripper{body: tinyPNG(t), contentType: "image/png"}
\twithCalendarPhotoClient(t, newCalendarPhotoTestClient(recorder))

\tctx, cancel := context.WithCancel(context.Background())
\tcancel()
\tdata, err := NewCalendarCardRenderer().RenderCalendarImageContext(ctx, 6, 2026, []domain.CalendarEntry{{
\t\tKind: domain.CelebrationKindBirthday,
\t\tMember: &domain.Member{
\t\t\tShortKoreanName: "페코라",
\t\t\tPhoto:           photoURL,
\t\t},
\t\tDay: 15,
\t}})
\tif err != nil {
\t\tt.Fatalf("RenderCalendarImageContext() error = %v", err)
\t}
\tassertValidPNG(t, data)
\tif got := recorder.requests.Load(); got != 0 {
\t\tt.Fatalf("cancelled render fetched %d photos, want 0", got)
\t}
}
''',
)

# 3. Reject persistence-invalid alarm identities before per-item dedup claims.
replace_once(
    "hololive/hololive-shared/pkg/domain/alarm.go",
    '''func (n *AlarmNotification) UserCount() int {
\treturn len(n.Users)
}

func (n *AlarmNotification) ValidateLiveDispatchRoute() error {
''',
    '''func (n *AlarmNotification) UserCount() int {
\treturn len(n.Users)
}

const maxAlarmDispatchIdentifierBytes = 64

// ValidateDispatchPersistenceIdentity enforces the PostgreSQL ledger identity
// contract before a notification joins a shared batch transaction.
func (n *AlarmNotification) ValidateDispatchPersistenceIdentity() error {
\tif n == nil {
\t\treturn fmt.Errorf("alarm dispatch persistence: notification is nil")
\t}
\tif err := validateAlarmDispatchIdentifier("room id", n.RoomID); err != nil {
\t\treturn err
\t}
\tif n.Stream == nil {
\t\treturn fmt.Errorf("alarm dispatch persistence: stream is nil")
\t}
\tif err := validateAlarmDispatchIdentifier("stream id", n.Stream.ID); err != nil {
\t\treturn err
\t}
\tchannelID, err := n.dispatchChannelID()
\tif err != nil {
\t\treturn err
\t}
\treturn validateAlarmDispatchIdentifier("channel id", channelID)
}

func (n *AlarmNotification) dispatchChannelID() (string, error) {
\tcandidates := make([]string, 0, 3)
\tif n.Channel != nil {
\t\tcandidates = append(candidates, n.Channel.ID)
\t}
\tif n.Stream != nil {
\t\tcandidates = append(candidates, n.Stream.ChannelID)
\t\tif n.Stream.Channel != nil {
\t\t\tcandidates = append(candidates, n.Stream.Channel.ID)
\t\t}
\t}
\tresolved := ""
\tfor _, candidate := range candidates {
\t\tcandidate = strings.TrimSpace(candidate)
\t\tif candidate == "" {
\t\t\tcontinue
\t\t}
\t\tif resolved != "" && candidate != resolved {
\t\t\treturn "", fmt.Errorf("alarm dispatch persistence: channel ids disagree")
\t\t}
\t\tresolved = candidate
\t}
\treturn resolved, nil
}

func validateAlarmDispatchIdentifier(name, value string) error {
\ttrimmed := strings.TrimSpace(value)
\tif trimmed == "" {
\t\treturn fmt.Errorf("alarm dispatch persistence: %s is empty", name)
\t}
\tif trimmed != value {
\t\treturn fmt.Errorf("alarm dispatch persistence: %s has surrounding whitespace", name)
\t}
\tif len(value) > maxAlarmDispatchIdentifierBytes {
\t\treturn fmt.Errorf("alarm dispatch persistence: %s is too long: %d > %d bytes", name, len(value), maxAlarmDispatchIdentifierBytes)
\t}
\treturn nil
}

func (n *AlarmNotification) ValidateLiveDispatchRoute() error {
''',
)
replace_once(
    "hololive/hololive-alarm-worker/internal/service/alarm/checker/internal/checking/notifier/notifier_prepare.go",
    '''\tif err := payload.notification.ValidateLiveDispatchRoute(); err != nil {
\t\treturn nil, nil, sendOutcomeFailed, fmt.Errorf("send one: validate live dispatch route: %w", err)
\t}

\tclaimKeys, claimed, err := n.claimDedup(ctx, payload)
''',
    '''\tif err := payload.notification.ValidateLiveDispatchRoute(); err != nil {
\t\treturn nil, nil, sendOutcomeFailed, fmt.Errorf("send one: validate live dispatch route: %w", err)
\t}
\tif err := payload.notification.ValidateDispatchPersistenceIdentity(); err != nil {
\t\treturn nil, nil, sendOutcomeFailed, fmt.Errorf("send one: validate dispatch persistence identity: %w", err)
\t}

\tclaimKeys, claimed, err := n.claimDedup(ctx, payload)
''',
)
replace_once(
    "hololive/hololive-shared/pkg/domain/alarm_test.go",
    'import (\n\t"testing"\n',
    'import (\n\t"strings"\n\t"testing"\n',
)
append_text(
    "hololive/hololive-shared/pkg/domain/alarm_test.go",
    r'''
func TestAlarmNotificationValidateDispatchPersistenceIdentity(t *testing.T) {
\tt.Parallel()

\tvalid := func() *domain.AlarmNotification {
\t\treturn &domain.AlarmNotification{
\t\t\tAlarmType: domain.AlarmTypeLive,
\t\t\tRoomID:    "room-1",
\t\t\tChannel:   &domain.Channel{ID: "UC_channel"},
\t\t\tStream:    &domain.Stream{ID: "stream-1", ChannelID: "UC_channel"},
\t\t}
\t}
\tif err := valid().ValidateDispatchPersistenceIdentity(); err != nil {
\t\tt.Fatalf("valid identity error = %v", err)
\t}

\ttests := []struct {
\t\tname   string
\t\tmutate func(*domain.AlarmNotification)
\t}{
\t\t{name: "overlong room", mutate: func(n *domain.AlarmNotification) { n.RoomID = strings.Repeat("r", 65) }},
\t\t{name: "overlong stream", mutate: func(n *domain.AlarmNotification) { n.Stream.ID = strings.Repeat("s", 65) }},
\t\t{name: "overlong channel", mutate: func(n *domain.AlarmNotification) { n.Channel.ID = strings.Repeat("c", 65); n.Stream.ChannelID = n.Channel.ID }},
\t\t{name: "ambiguous channel", mutate: func(n *domain.AlarmNotification) { n.Stream.ChannelID = "UC_other" }},
\t\t{name: "surrounding whitespace", mutate: func(n *domain.AlarmNotification) { n.Stream.ID = " stream-1 " }},
\t}
\tfor _, tt := range tests {
\t\tt.Run(tt.name, func(t *testing.T) {
\t\t\tt.Parallel()
\t\t\tnotification := valid()
\t\t\ttt.mutate(notification)
\t\t\tif err := notification.ValidateDispatchPersistenceIdentity(); err == nil {
\t\t\t\tt.Fatal("ValidateDispatchPersistenceIdentity() error = nil")
\t\t\t}
\t\t})
\t}
}
''',
)
write(
    "hololive/hololive-alarm-worker/internal/service/alarm/checker/internal/checking/notifier/notifier_persistence_security_test.go",
    r'''package notifier

import (
\t"context"
\t"strings"
\t"testing"
\t"time"

\t"github.com/kapu/hololive-shared/pkg/domain"
)

func TestPrepareOneRejectsPersistenceInvalidIdentityBeforeDedup(t *testing.T) {
\tt.Parallel()

\tstart := time.Now().UTC().Add(time.Hour)
\tnotification := &domain.AlarmNotification{
\t\tAlarmType: domain.AlarmTypeLive,
\t\tRoomID:    "room-1",
\t\tStream: &domain.Stream{
\t\t\tID:             strings.Repeat("s", 65),
\t\t\tChannelID:      "UC_channel",
\t\t\tStartScheduled: &start,
\t\t},
\t}

\t_, _, outcome, err := (&Notifier{}).prepareOne(context.Background(), notification)
\tif err == nil {
\t\tt.Fatal("prepareOne() error = nil")
\t}
\tif outcome != sendOutcomeFailed {
\t\tt.Fatalf("prepareOne() outcome = %v, want failed", outcome)
\t}
}
''',
)

# 4. Dynamic alarm targets may exceed estimates, but the global limiter must
# shed demand instead of making persisted user state a startup kill switch.
replace_once(
    "hololive/hololive-youtube-producer/internal/runtime/polling/budget_validator.go",
    '''func validateYouTubeProducerAggregateBudget(summary youtubeProducerBudgetSummary) error {
\tif summary.CombinedRPM <= summary.BudgetRPM {
\t\treturn nil
\t}
\treturn fmt.Errorf(
\t\t"youtube-producer combined active scraper RPM %.3f exceeds YouTube producer budget %.3f; increase poll intervals or reduce target channels",
\t\tsummary.CombinedRPM,
\t\tsummary.BudgetRPM,
\t)
}
''',
    '''func validateYouTubeProducerAggregateBudget(summary youtubeProducerBudgetSummary) error {
\tif summary.BudgetRPM <= 0 {
\t\treturn fmt.Errorf("youtube-producer budget RPM must be positive")
\t}
\t// Channel registrations include persisted user alarm targets. Their count is
\t// not trusted as a process-start admission control. Runtime source limiters
\t// enforce the actual request budget, while logYouTubeProducerBudgetSummary
\t// retains the oversubscription signal for operators.
\treturn nil
}
''',
)
replace_once(
    "hololive/hololive-youtube-producer/internal/runtime/polling/youtube_producer_poller_registrations_test.go",
    '''func TestBudgetRejectsAggressiveBackfillInterval(t *testing.T) {
''',
    '''func TestBudgetOversubscriptionDoesNotAbortStartup(t *testing.T) {
''',
)
replace_once(
    "hololive/hololive-youtube-producer/internal/runtime/polling/youtube_producer_poller_registrations_test.go",
    '''\tsummary := summarizeYouTubeProducerBudget(registrations)
\trequire.Error(t, validateYouTubeProducerPollerBudget(summary))
''',
    '''\tsummary := summarizeYouTubeProducerBudget(registrations)
\trequire.Greater(t, summary.CombinedRPM, summary.BudgetRPM)
\trequire.NoError(t, validateYouTubeProducerPollerBudget(summary))
''',
)

# 5. Remove package-global sync.Once coupling from health unit tests.
write(
    "hololive/hololive-shared/pkg/health/health.go",
    r'''// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package health

import (
\t"runtime"
\t"sync"
\t"time"
)

type state struct {
\tonce sync.Once
\tmu   sync.RWMutex

\tstartTime time.Time
\tversion   string
}

func newState() *state {
\treturn &state{version: "dev"}
}

var defaultState = newState()

func Init(v string) {
\tdefaultState.init(v)
}

func (s *state) init(v string) {
\tif s == nil {
\t\treturn
\t}
\ts.once.Do(func() {
\t\ts.mu.Lock()
\t\tdefer s.mu.Unlock()
\t\ts.startTime = time.Now()
\t\tif v != "" {
\t\t\ts.version = v
\t\t}
\t})
}

type Response struct {
\tStatus     string `json:"status"`
\tVersion    string `json:"version"`
\tUptime     string `json:"uptime"`
\tGoroutines int    `json:"goroutines"`
}

func Get() Response {
\treturn defaultState.get()
}

func (s *state) get() Response {
\tversion, startTime := s.snapshot()
\treturn Response{
\t\tStatus:     "ok",
\t\tVersion:    version,
\t\tUptime:     uptimeSince(startTime),
\t\tGoroutines: runtime.NumGoroutine(),
\t}
}

func GetVersion() string {
\tversion, _ := defaultState.snapshot()
\treturn version
}

func GetUptime() string {
\t_, startTime := defaultState.snapshot()
\treturn uptimeSince(startTime)
}

func (s *state) snapshot() (string, time.Time) {
\tif s == nil {
\t\treturn "dev", time.Time{}
\t}
\ts.mu.RLock()
\tdefer s.mu.RUnlock()
\treturn s.version, s.startTime
}

func uptimeSince(startTime time.Time) string {
\tif startTime.IsZero() {
\t\treturn "0s"
\t}
\treturn formatDuration(time.Since(startTime))
}

func formatDuration(d time.Duration) string {
\treturn d.Round(time.Second).String()
}
''',
)
write(
    "hololive/hololive-shared/pkg/health/health_init_test.go",
    r'''package health

import (
\t"strings"
\t"testing"
)

func TestStateInitSetsVersionAndStartTime(t *testing.T) {
\tt.Parallel()

\ts := newState()
\ts.init("v2.0.0")
\tversion, startTime := s.snapshot()
\tif version != "v2.0.0" {
\t\tt.Fatalf("version = %q, want v2.0.0", version)
\t}
\tif startTime.IsZero() {
\t\tt.Fatal("start time is zero")
\t}
}

func TestStateInitIsIdempotent(t *testing.T) {
\tt.Parallel()

\ts := newState()
\ts.init("first")
\tfirstVersion, firstStart := s.snapshot()
\ts.init("second")
\tsecondVersion, secondStart := s.snapshot()
\tif firstVersion != secondVersion || !firstStart.Equal(secondStart) {
\t\tt.Fatalf("second init changed state: first=%q/%v second=%q/%v", firstVersion, firstStart, secondVersion, secondStart)
\t}
}

func TestStateGetReturnsCompleteResponse(t *testing.T) {
\tt.Parallel()

\ts := newState()
\ts.init("test")
\tresp := s.get()
\tif resp.Status != "ok" {
\t\tt.Fatalf("status = %q, want ok", resp.Status)
\t}
\tif resp.Version != "test" {
\t\tt.Fatalf("version = %q, want test", resp.Version)
\t}
\tif resp.Goroutines <= 0 {
\t\tt.Fatalf("goroutines = %d, want > 0", resp.Goroutines)
\t}
\tif !strings.Contains(resp.Uptime, "s") {
\t\tt.Fatalf("uptime = %q, want duration suffix", resp.Uptime)
\t}
}

func TestUninitializedStateReportsZeroUptime(t *testing.T) {
\tt.Parallel()

\ts := newState()
\tresp := s.get()
\tif resp.Version != "dev" || resp.Uptime != "0s" {
\t\tt.Fatalf("uninitialized state = %#v", resp)
\t}
}
''',
)
