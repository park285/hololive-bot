package sendfence

import (
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

type Reason string

const (
	ReasonMissing           Reason = "missing"
	ReasonStatusChanged     Reason = "status_changed"
	ReasonOwnerChanged      Reason = "owner_changed"
	ReasonVersionChanged    Reason = "version_changed"
	ReasonLeaseExpired      Reason = "lease_expired"
	ReasonMemberTerminal    Reason = "member_terminal"
	ReasonMembershipChanged Reason = "membership_changed"
	ReasonRequestChanged    Reason = "request_changed"
	ReasonPeerMissed        Reason = "peer_missed"
)

func (r Reason) OwnershipHeld() bool {
	switch r {
	case ReasonMemberTerminal, ReasonMembershipChanged, ReasonRequestChanged, ReasonPeerMissed:
		return true
	case ReasonMissing, ReasonStatusChanged, ReasonOwnerChanged, ReasonVersionChanged, ReasonLeaseExpired:
		return false
	}

	return false
}

type Observation struct {
	ID                 int64
	Status             string
	StatusMatches      bool
	OwnerMatches       bool
	VersionMatches     bool
	LeaseValid         bool
	MembershipMatches  bool
	MembersNonTerminal bool
	RequestMatches     bool
	Applied            bool
}

func ScanObservation(row pgx.CollectableRow) (Observation, error) {
	var o Observation

	err := row.Scan(
		&o.ID, &o.Status,
		&o.StatusMatches, &o.OwnerMatches, &o.VersionMatches, &o.LeaseValid,
		&o.MembershipMatches, &o.MembersNonTerminal, &o.RequestMatches, &o.Applied,
	)
	if err != nil {
		return Observation{}, fmt.Errorf("scan send fence observation: %w", err)
	}

	return o, nil
}

func (o *Observation) reason() Reason {
	switch {
	case !o.StatusMatches:
		return ReasonStatusChanged
	case !o.OwnerMatches:
		return ReasonOwnerChanged
	case !o.VersionMatches:
		return ReasonVersionChanged
	case !o.LeaseValid:
		return ReasonLeaseExpired
	case !o.MembersNonTerminal:
		return ReasonMemberTerminal
	case !o.MembershipMatches:
		return ReasonMembershipChanged
	case !o.RequestMatches:
		return ReasonRequestChanged
	default:
		return ReasonPeerMissed
	}
}

type RowMiss struct {
	ID     int64
	Reason Reason
	Status string
}

type MissError struct {
	Action   string
	Expected int
	Applied  int
	Rows     []RowMiss
}

func (m *MissError) Error() string {
	var rows strings.Builder

	for i, row := range m.Rows {
		if i > 0 {
			rows.WriteString(", ")
		}

		fmt.Fprintf(&rows, "%d=%s", row.ID, row.Reason)

		if row.Status != "" {
			fmt.Fprintf(&rows, "(%s)", row.Status)
		}
	}

	return fmt.Sprintf("%s: send fence miss: applied %d of %d: %s", m.Action, m.Applied, m.Expected, rows.String())
}

func (m *MissError) UnappliedDeliveryIDs() []int64 {
	ids := make([]int64, 0, len(m.Rows))
	for _, row := range m.Rows {
		ids = append(ids, row.ID)
	}

	return ids
}

func (m *MissError) Has(reason Reason) bool {
	for _, row := range m.Rows {
		if row.Reason == reason {
			return true
		}
	}

	return false
}

var ErrGenerationExhausted = errors.New("send request generations exhausted")

func Check(action string, want []int64, observed []Observation) error {
	byID := make(map[int64]*Observation, len(observed))
	for i := range observed {
		byID[observed[i].ID] = &observed[i]
	}

	miss := MissError{Action: action, Expected: len(want)}

	for _, id := range want {
		observation, ok := byID[id]

		switch {
		case !ok:
			miss.Rows = append(miss.Rows, RowMiss{ID: id, Reason: ReasonMissing})
		case observation.Applied:
			miss.Applied++
		default:
			miss.Rows = append(miss.Rows, RowMiss{ID: id, Reason: observation.reason(), Status: observation.Status})
		}
	}

	if len(miss.Rows) == 0 {
		return nil
	}

	return &miss
}
