// Copyright (c) 2025 Kapu
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

package messagestrings

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

type failingQuerier struct {
	err   error
	calls int
}

func (q *failingQuerier) Query(context.Context, string, ...any) (pgx.Rows, error) {
	q.calls++

	return nil, q.err
}

// valueRows는 (namespace, key, value) 행을 순서대로 돌려주는 pgx.Rows 대역이다.
type valueRows struct {
	rows [][3]string
	next int
}

func (r *valueRows) Close()                                       {}
func (r *valueRows) Err() error                                   { return nil }
func (r *valueRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *valueRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *valueRows) Values() ([]any, error)                       { return nil, nil }
func (r *valueRows) RawValues() [][]byte                          { return nil }
func (r *valueRows) Conn() *pgx.Conn                              { return nil }

// TypeMap은 값이 없는 Rows의 pgx 계약에 따라 nil을 반환한다.
func (r *valueRows) TypeMap() *pgtype.Map { return nil }

func (r *valueRows) Next() bool {
	r.next++

	return r.next <= len(r.rows)
}

func (r *valueRows) Scan(dest ...any) error {
	row := r.rows[r.next-1]

	for i := range dest {
		target, ok := dest[i].(*string)
		if !ok {
			return errors.New("valueRows scans strings only")
		}

		*target = row[i]
	}

	return nil
}

type valueQuerier struct {
	rows [][3]string
}

func (q valueQuerier) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return &valueRows{rows: q.rows}, nil
}

type lookupCounts struct {
	loadFailures float64
	unloaded     float64
	missing      float64
}

func snapshotLookupCounts(namespace string) lookupCounts {
	initMetrics()

	return lookupCounts{
		loadFailures: testutil.ToFloat64(loadFailuresTotal),
		unloaded:     testutil.ToFloat64(lookupMissTotal.WithLabelValues(lookupMissReasonUnloaded, namespace)),
		missing:      testutil.ToFloat64(lookupMissTotal.WithLabelValues(lookupMissReasonMissing, namespace)),
	}
}

func assertLookupDelta(t *testing.T, before, after, want lookupCounts) {
	t.Helper()

	got := lookupCounts{
		loadFailures: after.loadFailures - before.loadFailures,
		unloaded:     after.unloaded - before.unloaded,
		missing:      after.missing - before.missing,
	}
	if got != want {
		t.Fatalf("metric delta = %+v, want %+v", got, want)
	}
}

// 조회는 DB를 다시 읽지 않는다. Load를 부르지 않은 store는 빈 값과 unloaded metric만 남기고 query를 하지 않는다.
func TestTextOnUnloadedStoreDoesNotLazyLoad(t *testing.T) {
	q := &failingQuerier{err: errors.New("connection refused")}
	store := &Store{pool: q, logger: slog.Default()}

	before := snapshotLookupCounts(NamespaceMisc)

	if got := store.Text(MiscVTuberFallback); got != "" {
		t.Fatalf("Text on unloaded store = %q, want empty", got)
	}

	assertLookupDelta(t, before, snapshotLookupCounts(NamespaceMisc), lookupCounts{unloaded: 1})

	if q.calls != 0 {
		t.Fatalf("query calls = %d, want 0 (no lazy load)", q.calls)
	}
}

func TestLoadCountsFailure(t *testing.T) {
	q := &failingQuerier{err: errors.New("connection refused")}
	store := &Store{pool: q, logger: slog.Default()}

	before := snapshotLookupCounts(NamespaceMisc)
	err := store.Load(t.Context())

	if err == nil || !errors.Is(err, q.err) {
		t.Fatalf("Load error = %v, want wrapped %v", err, q.err)
	}

	assertLookupDelta(t, before, snapshotLookupCounts(NamespaceMisc), lookupCounts{loadFailures: 1})
}

func TestLookupMissSeriesPreRegisteredAtZero(t *testing.T) {
	initMetrics()

	if got, want := testutil.CollectAndCount(lookupMissTotal), 2*len(knownNamespaces); got != want {
		t.Fatalf("lookup series = %d, want %d (unloaded+missing x every Namespace* constant)", got, want)
	}

	fresh := lookupMissTotal.WithLabelValues(lookupMissReasonMissing, NamespaceTimeFmt)
	if v := testutil.ToFloat64(fresh); v != 0 {
		t.Fatalf("pre-registered series value = %v, want 0", v)
	}
}

func TestLookupCountsMissingWhenLoaded(t *testing.T) {
	store := &Store{pool: valueQuerier{}, logger: slog.Default()}
	if err := store.Load(t.Context()); err != nil {
		t.Fatalf("Load: %v", err)
	}

	before := snapshotLookupCounts(NamespaceOrg)
	if got, ok := store.Lookup(NamespaceOrg, "nonexistent"); ok || got != "" {
		t.Fatalf("Lookup missing key = (%q, %t), want (\"\", false)", got, ok)
	}

	assertLookupDelta(t, before, snapshotLookupCounts(NamespaceOrg), lookupCounts{missing: 1})
}

func TestLookupDoesNotCountHit(t *testing.T) {
	store := &Store{pool: valueQuerier{rows: [][3]string{{NamespaceOrg, "Hololive", "Holo"}}}, logger: slog.Default()}
	if err := store.Load(t.Context()); err != nil {
		t.Fatalf("Load: %v", err)
	}

	before := snapshotLookupCounts(NamespaceOrg)
	if got, ok := store.Lookup(NamespaceOrg, "Hololive"); !ok || got != "Holo" {
		t.Fatalf("Lookup hit = (%q, %t), want (Holo, true)", got, ok)
	}

	assertLookupDelta(t, before, snapshotLookupCounts(NamespaceOrg), lookupCounts{})
}

// 기동 검증은 빈 값·없는 key·빈 namespace를 모두 모아 한 번에 실패시킨다.
func TestValidateReportsEveryMissingEntry(t *testing.T) {
	store := &Store{pool: valueQuerier{rows: [][3]string{
		{NamespaceMisc, "vtuber_fallback", "VTuber"},
		{NamespaceMisc, "time_unknown", "  "},
	}}, logger: slog.Default()}
	if err := store.Load(t.Context()); err != nil {
		t.Fatalf("Load: %v", err)
	}

	err := store.Validate(Requirements{
		Keys:       []Key{MiscVTuberFallback, MiscTimeUnknown, MiscAlarmNoTitle},
		Namespaces: []string{NamespaceNewsCat},
	})
	if err == nil {
		t.Fatal("Validate() error = nil, want missing entries")
	}

	for _, want := range []string{"misc/time_unknown", "misc/alarm_no_title", "newscat/*"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Validate() error = %v, want mention of %s", err, want)
		}
	}

	if strings.Contains(err.Error(), "misc/vtuber_fallback") {
		t.Errorf("Validate() error = %v, must not report present key", err)
	}
}

func TestValidateRequiresLoad(t *testing.T) {
	store := &Store{pool: valueQuerier{}, logger: slog.Default()}

	if err := store.Validate(Requirements{Keys: []Key{MiscVTuberFallback}}); err == nil {
		t.Fatal("Validate() on unloaded store must fail")
	}
}
