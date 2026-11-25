package store

import (
	"context"
	"fmt"
	"slices"

	"github.com/park285/shared-go/postgres/pgxdb"

	"github.com/kapu/hololive-alarm-worker/internal/egress/sendfence"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle"
	"github.com/kapu/hololive-shared/pkg/dbx"
	ytcontentid "github.com/kapu/hololive-shared/pkg/service/youtube/contentid"
)

type handoffUnit struct {
	owner transitionRow
	key   ytcontentid.LogicalKey
	rows  []transitionRow
}

type handoffMembers struct {
	byID        map[int64]transitionRow
	keyByID     map[int64]ytcontentid.LogicalKey
	ownersByKey map[ytcontentid.LogicalKey]int
	requested   []requestedLogicalGroup
}

type handoffGroup struct {
	parentIDs []int64
	rows      []transitionRow
	complete  bool
}

func (s *TransitionStore) lockHandoffUnits(
	ctx context.Context,
	tx pgxdb.Querier,
	action string,
	claim *HandoffClaim,
) ([]handoffUnit, []sendfence.Observation, error) {
	members, err := loadHandoffMembers(ctx, tx, claim.MemberIDs)
	if err != nil {
		return nil, nil, err
	}

	group, err := s.lockHandoffGroup(ctx, tx, members.requested)
	if err != nil {
		return nil, nil, err
	}

	if miss := handoffGroupGrewMiss(action, claim, members, group); miss != nil {
		return nil, nil, miss
	}

	units, observed := s.observeHandoffUnits(claim, members, group.rows, group.complete)

	if handoffObservationsMatch(claim, observed) {
		if observeErr := observeHandoffLedger(ctx, tx, units, observed); observeErr != nil {
			return nil, nil, observeErr
		}
	}

	applied := handoffObservationsMatch(claim, observed)
	for i := range observed {
		observed[i].Applied = applied
	}

	return units, observed, nil
}

func loadHandoffMembers(ctx context.Context, tx pgxdb.Querier, memberIDs []int64) (handoffMembers, error) {
	var rows []transitionRow

	if err := dbx.SelectSQL(ctx, tx, &rows, "load handoff members", handoffMembersSQL, memberIDs); err != nil {
		return handoffMembers{}, fmt.Errorf("load frozen members: %w", err)
	}

	members := handoffMembers{
		byID:        make(map[int64]transitionRow, len(rows)),
		keyByID:     make(map[int64]ytcontentid.LogicalKey, len(rows)),
		ownersByKey: make(map[ytcontentid.LogicalKey]int, len(rows)),
		requested:   make([]requestedLogicalGroup, 0, len(rows)),
	}

	for i := range rows {
		members.byID[rows[i].ID] = rows[i]

		key, keyErr := rows[i].logicalKey()
		if keyErr != nil {
			continue
		}

		members.keyByID[rows[i].ID] = key
		members.ownersByKey[key]++

		outbox := rows[i].domainOutbox()

		members.requested = append(members.requested, requestedLogicalGroup{
			key: key, delivery: rows[i].domainDelivery(), outbox: outbox,
			candidate: logicalIdentityCandidates(key, outbox),
		})
	}

	return members, nil
}

func (s *TransitionStore) lockHandoffGroup(ctx context.Context, tx pgxdb.Querier, requested []requestedLogicalGroup) (handoffGroup, error) {
	if len(requested) == 0 {
		return handoffGroup{}, nil
	}

	groupArgs := s.logicalGroupArgs(requested)

	parentIDs, err := lockHandoffParents(ctx, tx, groupArgs)
	if err != nil {
		return handoffGroup{}, err
	}

	rows, err := queryLogicalGroupRows(ctx, tx, groupArgs)
	if err != nil {
		return handoffGroup{}, fmt.Errorf("lock logical group rows: %w", err)
	}

	return handoffGroup{
		parentIDs: parentIDs, rows: rows,
		complete: s.logicalGroupWindowComplete(len(requested), len(rows)),
	}, nil
}

func lockHandoffParents(ctx context.Context, tx pgxdb.Querier, groupArgs []any) ([]int64, error) {
	var parentIDs []int64

	if err := dbx.SelectSQL(ctx, tx, &parentIDs, "load handoff group parents", handoffGroupParentsSQL, groupArgs...); err != nil {
		return nil, fmt.Errorf("load logical group parents: %w", err)
	}

	if err := lockOutboxAggregateParents(ctx, tx, parentIDs); err != nil {
		return nil, err
	}

	return parentIDs, nil
}

func handoffGroupGrewMiss(action string, claim *HandoffClaim, members handoffMembers, group handoffGroup) error {
	grown := make(map[ytcontentid.LogicalKey]struct{})
	unkeyed := false

	for i := range group.rows {
		if _, prelocked := slices.BinarySearch(group.parentIDs, group.rows[i].OutboxID); prelocked {
			continue
		}

		key, keyErr := group.rows[i].logicalKey()
		if keyErr != nil {
			unkeyed = true

			continue
		}

		grown[key] = struct{}{}
	}

	if len(grown) == 0 && !unkeyed {
		return nil
	}

	changed := make(map[int64]bool, len(claim.MemberIDs))

	for _, id := range claim.MemberIDs {
		key, keyed := members.keyByID[id]
		if _, hit := grown[key]; keyed && hit {
			changed[id] = true
		}
	}

	observed := make([]sendfence.Observation, 0, len(claim.MemberIDs))

	for _, id := range claim.MemberIDs {
		observed = append(observed, sendfence.Observation{
			ID: id, Status: string(members.byID[id].Status),
			StatusMatches: true, OwnerMatches: true, VersionMatches: true, LeaseValid: true,
			MembershipMatches: len(changed) > 0 && !changed[id], MembersNonTerminal: true, RequestMatches: true,
		})
	}

	if err := sendfence.Check(action, claim.MemberIDs, observed); err != nil {
		return fmt.Errorf("logical group gained parents: %w", err)
	}

	return nil
}

func (s *TransitionStore) observeHandoffUnits(
	claim *HandoffClaim,
	members handoffMembers,
	locked []transitionRow,
	complete bool,
) ([]handoffUnit, []sendfence.Observation) {
	rowsByID, rowsByKey := indexHandoffGroupRows(locked, members.ownersByKey)
	units := make([]handoffUnit, 0, len(claim.MemberIDs))
	observed := make([]sendfence.Observation, 0, len(claim.MemberIDs))

	for _, id := range claim.MemberIDs {
		row, ok := rowsByID[id]
		rowLocked := ok

		if !ok {
			if row, ok = members.byID[id]; !ok {
				continue
			}
		}

		key, keyed := members.keyByID[id]
		unitRows := rowsByKey[key]
		version, sent := claim.SentMembers[id]

		observed = append(observed, sendfence.Observation{
			ID: id, Status: string(row.Status),
			StatusMatches: row.Status == lifecycle.StatusSent, OwnerMatches: true,
			VersionMatches: sent && row.RowVersion == version, LeaseValid: true,
			MembershipMatches: complete && rowLocked && keyed && members.ownersByKey[key] == 1 && len(unitRows) <= s.config.LogicalGroupLimit &&
				!slices.ContainsFunc(unitRows, unresolvedOrInFlight),
			MembersNonTerminal: true, RequestMatches: true,
		})
		units = append(units, handoffUnit{owner: row, key: key, rows: unitRows})
	}

	return units, observed
}

func indexHandoffGroupRows(
	locked []transitionRow,
	ownersByKey map[ytcontentid.LogicalKey]int,
) (map[int64]transitionRow, map[ytcontentid.LogicalKey][]transitionRow) {
	rowsByID := make(map[int64]transitionRow, len(locked))
	rowsByKey := make(map[ytcontentid.LogicalKey][]transitionRow, len(ownersByKey))

	for i := range locked {
		rowsByID[locked[i].ID] = locked[i]

		key, keyErr := locked[i].logicalKey()
		if keyErr != nil {
			continue
		}

		if _, wanted := ownersByKey[key]; wanted {
			rowsByKey[key] = append(rowsByKey[key], locked[i])
		}
	}

	return rowsByID, rowsByKey
}

func handoffObservationsMatch(claim *HandoffClaim, observed []sendfence.Observation) bool {
	return len(observed) == len(claim.MemberIDs) && !slices.ContainsFunc(observed, func(o sendfence.Observation) bool {
		return !o.StatusMatches || !o.VersionMatches || !o.MembershipMatches
	})
}

func observeHandoffLedger(ctx context.Context, tx pgxdb.Querier, units []handoffUnit, observed []sendfence.Observation) error {
	keys := make([]ytcontentid.LogicalKey, 0, len(units))
	for i := range units {
		keys = append(keys, units[i].key)
	}

	ledgerByKey, _, err := loadTransitionLedger(ctx, tx, keys)
	if err != nil {
		return fmt.Errorf("lock logical ledger: %w", err)
	}

	for i := range observed {
		observed[i].MembershipMatches = observed[i].MembershipMatches &&
			ledgerSourcedBy(ledgerByKey, units[i].key, units[i].owner.ID)
	}

	return nil
}

func ledgerSourcedBy(ledgerByKey map[ytcontentid.LogicalKey]DeliveryLedgerRecord, key ytcontentid.LogicalKey, ownerID int64) bool {
	record, ok := ledgerByKey[key]

	return ok && record.Status == LedgerStatusSent && record.SourceDeliveryID != nil && *record.SourceDeliveryID == ownerID
}

func unresolvedOrInFlight(row transitionRow) bool {
	return row.Status == lifecycle.StatusSending || row.Status == lifecycle.StatusQuarantined
}
