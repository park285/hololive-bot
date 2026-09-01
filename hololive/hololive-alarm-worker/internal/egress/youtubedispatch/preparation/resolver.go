// Package preparation resolves logical YouTube delivery groups and freezes
// provider operations before any external effect is allowed.
package preparation

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle"
	"github.com/kapu/hololive-shared/pkg/domain"
	ytcontentid "github.com/kapu/hololive-shared/pkg/service/youtube/contentid"
)

// DeliverySnapshot is one physical row joined with the outbox identity needed
// to derive its canonical logical key.
type DeliverySnapshot struct {
	DeliveryID     int64
	OutboxID       int64
	Kind           domain.OutboxKind
	ContentID      string
	Payload        string
	RoomID         string
	Status         lifecycle.DeliveryStatus
	AttemptCount   int
	NextAttemptAt  time.Time
	CreatedAt      time.Time
	LockedAt       time.Time
	RowVersion     int64
	InCurrentBatch bool
	Lease          lifecycle.PreparationLease
}

func (d DeliverySnapshot) validate() error {
	if d.DeliveryID <= 0 {
		return errors.New("delivery id must be positive")
	}

	if d.OutboxID <= 0 {
		return errors.New("outbox id must be positive")
	}

	if !d.Status.Valid() {
		return fmt.Errorf("delivery status %q is invalid", d.Status)
	}

	if d.AttemptCount < 0 {
		return errors.New("attempt count is negative")
	}

	if d.RowVersion < 0 {
		return errors.New("row version is negative")
	}

	if d.CreatedAt.IsZero() {
		return errors.New("created at is zero")
	}

	if d.Status == lifecycle.StatusSending && d.LockedAt.IsZero() {
		return errors.New("sending delivery is missing locked at")
	}

	if d.InCurrentBatch {
		if !d.Lease.Valid() {
			return errors.New("current batch delivery is missing preparation lease")
		}

		if d.Lease.DeliveryID() != d.DeliveryID || d.Lease.RowVersion() != d.RowVersion || !d.Lease.LockedAt().Equal(d.LockedAt) {
			return errors.New("current batch preparation lease does not match snapshot")
		}

		if d.Status != lifecycle.StatusPending {
			return errors.New("current batch delivery is not pending")
		}
	}

	return nil
}

func (d DeliverySnapshot) logicalKey() (ytcontentid.LogicalKey, error) {
	key, err := ytcontentid.ResolveDeliveryKey(d.Kind, d.ContentID, d.Payload, d.RoomID)
	if err != nil {
		return ytcontentid.LogicalKey{}, fmt.Errorf("delivery %d logical identity: %w", d.DeliveryID, err)
	}

	return key, nil
}

type LedgerEvidence struct {
	Key        ytcontentid.LogicalKey
	Status     lifecycle.LedgerStatus
	RecordedAt time.Time
}

type ResolutionKind uint8

const (
	LogicalActive ResolutionKind = iota + 1
	LogicalFulfilled
	LogicalUnresolved
	LogicalInFlight
	LogicalOwnerPendingElsewhere
	LogicalFailed
	LogicalInvariantBreach
)

type InvariantReason string

const (
	InvariantInvalidSnapshot         InvariantReason = "invalid_snapshot"
	InvariantInvalidLogicalIdentity  InvariantReason = "invalid_logical_identity"
	InvariantPhysicalKeyConflict     InvariantReason = "physical_key_conflict"
	InvariantDuplicateSnapshot       InvariantReason = "duplicate_snapshot_conflict"
	InvariantGroupScanOverflow       InvariantReason = "group_scan_overflow"
	InvariantQuarantinedAndSending   InvariantReason = "quarantined_and_sending"
	InvariantMultipleSending         InvariantReason = "multiple_sending"
	InvariantLedgerQuarantineSending InvariantReason = "ledger_quarantine_and_sending"
	InvariantTerminalLedgerMissing   InvariantReason = "terminal_ledger_missing"
	InvariantNoResolvableOwner       InvariantReason = "no_resolvable_owner"
)

type Resolution struct {
	key       ytcontentid.LogicalKey
	kind      ResolutionKind
	owner     DeliverySnapshot
	followers []DeliverySnapshot
	members   []DeliverySnapshot
	due       time.Time
	reason    InvariantReason
	detail    string
}

func (r Resolution) Key() ytcontentid.LogicalKey      { return r.key }
func (r Resolution) Kind() ResolutionKind             { return r.kind }
func (r Resolution) Owner() DeliverySnapshot          { return r.owner }
func (r Resolution) Followers() []DeliverySnapshot    { return slices.Clone(r.followers) }
func (r Resolution) Members() []DeliverySnapshot      { return slices.Clone(r.members) }
func (r Resolution) Due() time.Time                   { return r.due }
func (r Resolution) InvariantReason() InvariantReason { return r.reason }
func (r Resolution) Detail() string                   { return r.detail }
func (r Resolution) ProviderAllowed() bool            { return r.kind == LogicalActive }

type ResolverConfig struct {
	LogicalGroupScanLimit int
	RetryBackoff          time.Duration
	LockTimeout           time.Duration
	RequireTerminalLedger bool
}

type Resolver struct {
	config ResolverConfig
}

func NewResolver(config ResolverConfig) (Resolver, error) {
	if config.LogicalGroupScanLimit <= 0 {
		return Resolver{}, errors.New("new logical resolver: scan limit must be positive")
	}

	if config.RetryBackoff <= 0 {
		return Resolver{}, errors.New("new logical resolver: retry backoff must be positive")
	}

	if config.LockTimeout <= 0 {
		return Resolver{}, errors.New("new logical resolver: lock timeout must be positive")
	}

	return Resolver{config: config}, nil
}

// ResolveGroups performs ledger-first, bounded logical resolution. The caller
// supplies batch-loaded ledger and retained rows; this function performs no I/O.
func (r Resolver) ResolveGroups(
	rows []DeliverySnapshot,
	ledger []LedgerEvidence,
	requestedKeys []ytcontentid.LogicalKey,
	at time.Time,
) []Resolution {
	canonicalAt, err := lifecycle.CanonicalTime(at)
	if err != nil {
		return []Resolution{breach(ytcontentid.LogicalKey{}, nil, InvariantInvalidSnapshot, err.Error())}
	}

	ledgerByKey, invalidLedger := indexLedgerEvidence(ledger)
	if invalidLedger != nil {
		return []Resolution{*invalidLedger}
	}

	byID, invalid := indexDeliverySnapshots(rows)
	if len(invalid) > 0 {
		return invalid
	}

	groups := groupDeliverySnapshots(byID, ledgerByKey, requestedKeys)
	keys := slices.SortedFunc(maps.Keys(groups), compareLogicalKey)
	result := make([]Resolution, 0, len(keys))

	for _, key := range keys {
		members := groups[key]
		slices.SortFunc(members, compareDeliveryOwner)

		result = append(result, r.resolveGroup(key, members, ledgerByKey[key], canonicalAt))
	}

	return result
}

func indexLedgerEvidence(ledger []LedgerEvidence) (map[ytcontentid.LogicalKey]LedgerEvidence, *Resolution) {
	byKey := make(map[ytcontentid.LogicalKey]LedgerEvidence, len(ledger))
	for i := range ledger {
		if !ledger[i].Status.Valid() {
			invalid := breach(ledger[i].Key, nil, InvariantInvalidSnapshot, "invalid ledger status")

			return nil, &invalid
		}

		if existing, ok := byKey[ledger[i].Key]; ok && existing.Status != ledger[i].Status {
			invalid := breach(ledger[i].Key, nil, InvariantDuplicateSnapshot, "conflicting ledger evidence")

			return nil, &invalid
		}

		byKey[ledger[i].Key] = ledger[i]
	}

	return byKey, nil
}

func indexDeliverySnapshots(rows []DeliverySnapshot) (map[int64]keyedSnapshot, []Resolution) {
	byID := make(map[int64]keyedSnapshot, len(rows))
	invalid := make([]Resolution, 0)

	for i := range rows {
		key, rowBreach := validateAndResolveSnapshot(rows[i])
		if rowBreach != nil {
			invalid = append(invalid, *rowBreach)

			continue
		}

		duplicateBreach := mergeIndexedSnapshot(byID, key, rows[i])
		if duplicateBreach != nil {
			invalid = append(invalid, *duplicateBreach)
		}
	}

	return byID, invalid
}

func validateAndResolveSnapshot(row DeliverySnapshot) (ytcontentid.LogicalKey, *Resolution) {
	if err := row.validate(); err != nil {
		invalid := breach(ytcontentid.LogicalKey{}, []DeliverySnapshot{row}, InvariantInvalidSnapshot, err.Error())

		return ytcontentid.LogicalKey{}, &invalid
	}

	key, err := row.logicalKey()
	if err != nil {
		invalid := breach(ytcontentid.LogicalKey{}, []DeliverySnapshot{row}, InvariantInvalidLogicalIdentity, err.Error())

		return ytcontentid.LogicalKey{}, &invalid
	}

	return key, nil
}

func mergeIndexedSnapshot(byID map[int64]keyedSnapshot, key ytcontentid.LogicalKey, row DeliverySnapshot) *Resolution {
	existing, ok := byID[row.DeliveryID]
	if !ok {
		byID[row.DeliveryID] = keyedSnapshot{key: key, row: row}

		return nil
	}

	if existing.key != key {
		invalid := breach(key, []DeliverySnapshot{existing.row, row}, InvariantPhysicalKeyConflict, "one delivery resolved to multiple logical keys")

		return &invalid
	}

	merged, ok := mergeSameSnapshot(existing.row, row)
	if !ok {
		invalid := breach(key, []DeliverySnapshot{existing.row, row}, InvariantDuplicateSnapshot, "duplicate delivery snapshots disagree")

		return &invalid
	}

	byID[row.DeliveryID] = keyedSnapshot{key: key, row: merged}

	return nil
}

func groupDeliverySnapshots(
	byID map[int64]keyedSnapshot,
	ledger map[ytcontentid.LogicalKey]LedgerEvidence,
	requestedKeys []ytcontentid.LogicalKey,
) map[ytcontentid.LogicalKey][]DeliverySnapshot {
	groups := make(map[ytcontentid.LogicalKey][]DeliverySnapshot)

	for id := range maps.Keys(byID) {
		groups[byID[id].key] = append(groups[byID[id].key], byID[id].row)
	}

	for _, key := range requestedKeys {
		if _, ok := groups[key]; !ok {
			groups[key] = nil
		}
	}

	for key := range maps.Keys(ledger) {
		if _, ok := groups[key]; !ok {
			groups[key] = nil
		}
	}

	return groups
}

type keyedSnapshot struct {
	key ytcontentid.LogicalKey
	row DeliverySnapshot
}

func mergeSameSnapshot(a, b DeliverySnapshot) (DeliverySnapshot, bool) {
	left := a
	right := b

	left.InCurrentBatch = false
	left.Lease = lifecycle.PreparationLease{}
	right.InCurrentBatch = false
	right.Lease = lifecycle.PreparationLease{}

	if left != right {
		return DeliverySnapshot{}, false
	}

	if b.InCurrentBatch {
		a.InCurrentBatch = true
		a.Lease = b.Lease
	}

	return a, true
}

func (r Resolver) resolveGroup(
	key ytcontentid.LogicalKey,
	members []DeliverySnapshot,
	ledger LedgerEvidence,
	at time.Time,
) Resolution {
	if len(members) > r.config.LogicalGroupScanLimit {
		return breach(key, members, InvariantGroupScanOverflow, "logical group exceeds scan limit")
	}

	if ledger.Status == lifecycle.LedgerSent {
		return newResolution(key, LogicalFulfilled, DeliverySnapshot{}, members, time.Time{})
	}

	counts := countStatuses(members)
	if invariant := r.groupInvariant(key, members, ledger, counts); invariant != nil {
		return *invariant
	}

	if terminal := retainedTerminalResolution(key, members, ledger, counts); terminal != nil {
		return *terminal
	}

	if sendingIndex := slices.IndexFunc(members, func(member DeliverySnapshot) bool {
		return member.Status == lifecycle.StatusSending
	}); sendingIndex >= 0 {
		due := maxTime(at.Add(r.config.RetryBackoff), members[sendingIndex].LockedAt.Add(r.config.LockTimeout))

		return newResolution(key, LogicalInFlight, members[sendingIndex], followersOf(members, sendingIndex), due)
	}

	return resolveActiveOwner(key, members)
}

func countStatuses(members []DeliverySnapshot) map[lifecycle.DeliveryStatus]int {
	counts := make(map[lifecycle.DeliveryStatus]int)

	for i := range members {
		counts[members[i].Status]++
	}

	return counts
}

func (r Resolver) groupInvariant(
	key ytcontentid.LogicalKey,
	members []DeliverySnapshot,
	ledger LedgerEvidence,
	counts map[lifecycle.DeliveryStatus]int,
) *Resolution {
	if ledger.Status == lifecycle.LedgerQuarantined && counts[lifecycle.StatusSending] > 0 {
		invalid := breach(key, members, InvariantLedgerQuarantineSending, "quarantined ledger conflicts with retained sending row")

		return &invalid
	}

	if counts[lifecycle.StatusQuarantined] > 0 && counts[lifecycle.StatusSending] > 0 {
		invalid := breach(key, members, InvariantQuarantinedAndSending, "retained quarantine conflicts with sending row")

		return &invalid
	}

	if counts[lifecycle.StatusSending] > 1 {
		invalid := breach(key, members, InvariantMultipleSending, "multiple retained sending rows")

		return &invalid
	}

	if r.config.RequireTerminalLedger && (counts[lifecycle.StatusSent] > 0 || counts[lifecycle.StatusQuarantined] > 0) {
		invalid := breach(key, members, InvariantTerminalLedgerMissing, "retained terminal row has no ledger evidence")

		return &invalid
	}

	return nil
}

func retainedTerminalResolution(
	key ytcontentid.LogicalKey,
	members []DeliverySnapshot,
	ledger LedgerEvidence,
	counts map[lifecycle.DeliveryStatus]int,
) *Resolution {
	if ledger.Status == lifecycle.LedgerQuarantined || counts[lifecycle.StatusQuarantined] > 0 {
		resolved := newResolution(key, LogicalUnresolved, DeliverySnapshot{}, members, time.Time{})

		return &resolved
	}

	if counts[lifecycle.StatusSent] > 0 {
		resolved := newResolution(key, LogicalFulfilled, DeliverySnapshot{}, members, time.Time{})

		return &resolved
	}

	return nil
}

func resolveActiveOwner(key ytcontentid.LogicalKey, members []DeliverySnapshot) Resolution {
	ownerIndex := slices.IndexFunc(members, func(member DeliverySnapshot) bool {
		return member.Status == lifecycle.StatusPending || member.Status == lifecycle.StatusFailed
	})
	if ownerIndex < 0 {
		return breach(key, members, InvariantNoResolvableOwner, "logical group has no active or terminal evidence")
	}

	owner := members[ownerIndex]
	followers := followersOf(members, ownerIndex)

	if owner.Status == lifecycle.StatusFailed {
		return newResolution(key, LogicalFailed, owner, followers, time.Time{})
	}

	if !owner.InCurrentBatch {
		return newResolution(key, LogicalOwnerPendingElsewhere, owner, followers, owner.NextAttemptAt)
	}

	return newResolution(key, LogicalActive, owner, followers, owner.NextAttemptAt)
}

func newResolution(key ytcontentid.LogicalKey, kind ResolutionKind, owner DeliverySnapshot, followers []DeliverySnapshot, due time.Time) Resolution {
	members := slices.Clone(followers)

	if owner.DeliveryID != 0 {
		members = append(members, owner)
		slices.SortFunc(members, compareDeliveryOwner)
	}

	return Resolution{key: key, kind: kind, owner: owner, followers: slices.Clone(followers), members: members, due: due}
}

func breach(key ytcontentid.LogicalKey, members []DeliverySnapshot, reason InvariantReason, detail string) Resolution {
	return Resolution{key: key, kind: LogicalInvariantBreach, members: slices.Clone(members), reason: reason, detail: detail}
}

func followersOf(members []DeliverySnapshot, ownerIndex int) []DeliverySnapshot {
	followers := make([]DeliverySnapshot, 0, len(members)-1)
	for i := range members {
		if i != ownerIndex {
			followers = append(followers, members[i])
		}
	}

	return followers
}

func compareDeliveryOwner(a, b DeliverySnapshot) int {
	if byCreatedAt := a.CreatedAt.Compare(b.CreatedAt); byCreatedAt != 0 {
		return byCreatedAt
	}

	return cmp.Compare(a.DeliveryID, b.DeliveryID)
}

func compareLogicalKey(a, b ytcontentid.LogicalKey) int {
	if byKind := cmp.Compare(a.Kind, b.Kind); byKind != 0 {
		return byKind
	}

	if byID := cmp.Compare(a.LogicalID, b.LogicalID); byID != 0 {
		return byID
	}

	return cmp.Compare(a.RoomID, b.RoomID)
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a.UTC().Truncate(time.Microsecond)
	}

	return b.UTC().Truncate(time.Microsecond)
}
