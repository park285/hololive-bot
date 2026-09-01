package preparation

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle"
	ytcontentid "github.com/kapu/hololive-shared/pkg/service/youtube/contentid"
)

type ImmutableSendRequest struct {
	roomID     string
	message    string
	dedupeKeys []string
}

func NewImmutableSendRequest(roomID, message string, dedupeKeys []string) (ImmutableSendRequest, error) {
	normalizedRoomID := strings.TrimSpace(roomID)
	if normalizedRoomID == "" {
		return ImmutableSendRequest{}, errors.New("new immutable send request: room id is empty")
	}

	if strings.TrimSpace(message) == "" {
		return ImmutableSendRequest{}, errors.New("new immutable send request: message is empty")
	}

	if len(dedupeKeys) == 0 {
		return ImmutableSendRequest{}, errors.New("new immutable send request: dedupe keys are empty")
	}

	keys := make([]string, 0, len(dedupeKeys))
	for i := range dedupeKeys {
		key := strings.TrimSpace(dedupeKeys[i])
		if key == "" {
			return ImmutableSendRequest{}, fmt.Errorf("new immutable send request: dedupe key[%d] is empty", i)
		}

		keys = append(keys, key)
	}

	return ImmutableSendRequest{roomID: normalizedRoomID, message: message, dedupeKeys: keys}, nil
}

func (r ImmutableSendRequest) RoomID() string       { return r.roomID }
func (r ImmutableSendRequest) Message() string      { return r.message }
func (r ImmutableSendRequest) DedupeKeys() []string { return slices.Clone(r.dedupeKeys) }

type PreparedOwner struct {
	key             ytcontentid.LogicalKey
	deliveryID      int64
	outboxID        int64
	lease           lifecycle.PreparationLease
	expectedStatus  lifecycle.DeliveryStatus
	expectedVersion int64
	expectedAttempt int
	nextStatus      lifecycle.DeliveryStatus
	nextVersion     int64
	followerIDs     []int64
	preparedAt      time.Time
}

func NewPreparedOwner(resolution Resolution, preparedAt time.Time) (PreparedOwner, error) {
	if resolution.kind != LogicalActive {
		return PreparedOwner{}, errors.New("new prepared owner: logical resolution is not active")
	}

	owner := resolution.owner
	if !owner.Lease.Valid() {
		return PreparedOwner{}, errors.New("new prepared owner: owner has no valid preparation lease")
	}

	canonicalPreparedAt, err := lifecycle.CanonicalTime(preparedAt)
	if err != nil {
		return PreparedOwner{}, fmt.Errorf("new prepared owner: prepared at: %w", err)
	}

	followerIDs := make([]int64, 0, len(resolution.followers))
	for i := range resolution.followers {
		followerIDs = append(followerIDs, resolution.followers[i].DeliveryID)
	}

	return PreparedOwner{
		key: resolution.key, deliveryID: owner.DeliveryID, outboxID: owner.OutboxID,
		lease: owner.Lease, expectedStatus: lifecycle.StatusPending,
		expectedVersion: owner.RowVersion, expectedAttempt: owner.AttemptCount,
		nextStatus: lifecycle.StatusSending, nextVersion: owner.RowVersion + 1,
		followerIDs: followerIDs, preparedAt: canonicalPreparedAt,
	}, nil
}

func (o PreparedOwner) Key() ytcontentid.LogicalKey              { return o.key }
func (o PreparedOwner) DeliveryID() int64                        { return o.deliveryID }
func (o PreparedOwner) OutboxID() int64                          { return o.outboxID }
func (o PreparedOwner) Lease() lifecycle.PreparationLease        { return o.lease }
func (o PreparedOwner) ExpectedStatus() lifecycle.DeliveryStatus { return o.expectedStatus }
func (o PreparedOwner) ExpectedVersion() int64                   { return o.expectedVersion }
func (o PreparedOwner) ExpectedAttempt() int                     { return o.expectedAttempt }
func (o PreparedOwner) NextStatus() lifecycle.DeliveryStatus     { return o.nextStatus }
func (o PreparedOwner) NextVersion() int64                       { return o.nextVersion }
func (o PreparedOwner) FollowerIDs() []int64                     { return slices.Clone(o.followerIDs) }
func (o PreparedOwner) PreparedAt() time.Time                    { return o.preparedAt }

type PreparedSendOperation struct {
	operationID     string
	clientRequestID string
	owners          []PreparedOwner
	ledgerKeys      []ytcontentid.LogicalKey
	tracking        []lifecycle.TrackingRequirement
	request         ImmutableSendRequest
	preparedAt      time.Time
}

func NewPreparedSendOperation(
	operationID string,
	clientRequestID string,
	owners []PreparedOwner,
	tracking []lifecycle.TrackingRequirement,
	request ImmutableSendRequest,
	preparedAt time.Time,
) (PreparedSendOperation, error) {
	if strings.TrimSpace(operationID) == "" {
		return PreparedSendOperation{}, errors.New("new prepared send operation: operation id is empty")
	}

	if strings.TrimSpace(clientRequestID) == "" {
		return PreparedSendOperation{}, errors.New("new prepared send operation: client request id is empty")
	}

	if len(owners) == 0 {
		return PreparedSendOperation{}, errors.New("new prepared send operation: owners are empty")
	}

	if request.roomID == "" || len(request.dedupeKeys) == 0 {
		return PreparedSendOperation{}, errors.New("new prepared send operation: request is invalid")
	}

	canonicalPreparedAt, err := lifecycle.CanonicalTime(preparedAt)
	if err != nil {
		return PreparedSendOperation{}, fmt.Errorf("new prepared send operation: prepared at: %w", err)
	}

	ownerIDs := make(map[int64]struct{}, len(owners))
	ledgerKeys := make([]ytcontentid.LogicalKey, 0, len(owners))
	keySet := make(map[ytcontentid.LogicalKey]struct{}, len(owners))

	for i := range owners {
		if owners[i].deliveryID <= 0 || !owners[i].lease.Valid() {
			return PreparedSendOperation{}, fmt.Errorf("new prepared send operation: owner[%d] is invalid", i)
		}

		if owners[i].key.RoomID != request.roomID {
			return PreparedSendOperation{}, fmt.Errorf("new prepared send operation: owner[%d] room does not match request", i)
		}

		if _, ok := ownerIDs[owners[i].deliveryID]; ok {
			return PreparedSendOperation{}, fmt.Errorf("new prepared send operation: duplicate owner id %d", owners[i].deliveryID)
		}

		ownerIDs[owners[i].deliveryID] = struct{}{}
		if _, ok := keySet[owners[i].key]; ok {
			return PreparedSendOperation{}, fmt.Errorf("new prepared send operation: duplicate logical key %s", owners[i].key.Hash())
		}

		keySet[owners[i].key] = struct{}{}
		ledgerKeys = append(ledgerKeys, owners[i].key)
	}

	deduplicatedTracking, err := DeduplicateTrackingRequirements(tracking)
	if err != nil {
		return PreparedSendOperation{}, fmt.Errorf("new prepared send operation: %w", err)
	}

	return PreparedSendOperation{
		operationID: strings.TrimSpace(operationID), clientRequestID: strings.TrimSpace(clientRequestID),
		owners: slices.Clone(owners), ledgerKeys: ledgerKeys, tracking: deduplicatedTracking,
		request: request, preparedAt: canonicalPreparedAt,
	}, nil
}

func (o PreparedSendOperation) OperationID() string     { return o.operationID }
func (o PreparedSendOperation) ClientRequestID() string { return o.clientRequestID }
func (o PreparedSendOperation) Owners() []PreparedOwner { return slices.Clone(o.owners) }
func (o PreparedSendOperation) LedgerKeys() []ytcontentid.LogicalKey {
	return slices.Clone(o.ledgerKeys)
}

func (o PreparedSendOperation) Tracking() []lifecycle.TrackingRequirement {
	return slices.Clone(o.tracking)
}
func (o PreparedSendOperation) Request() ImmutableSendRequest { return o.request }
func (o PreparedSendOperation) PreparedAt() time.Time         { return o.preparedAt }

// Coordinator keeps cache use downstream of durable logical resolution.
type Coordinator struct {
	resolver Resolver
}

func NewCoordinator(resolver Resolver) Coordinator {
	return Coordinator{resolver: resolver}
}

type PrepareBatchInput struct {
	Rows            []DeliverySnapshot
	Ledger          []LedgerEvidence
	RequestedKeys   []ytcontentid.LogicalKey
	At              time.Time
	ProceedCacheHit bool
}

// ResolveForPreparation always executes ledger/group resolution. A Proceed
// cache hit may avoid repeated post-level work later, but never bypasses this gate.
func (c Coordinator) ResolveForPreparation(input PrepareBatchInput) []Resolution {
	return c.resolver.ResolveGroups(input.Rows, input.Ledger, input.RequestedKeys, input.At)
}
