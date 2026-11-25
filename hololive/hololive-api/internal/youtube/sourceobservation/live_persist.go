package sourceobservation

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/park285/shared-go/postgres/pgxdb"

	"github.com/kapu/hololive-api/internal/youtube/reconcile/live"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
)

const (
	liveSessionBatchSize      = 64
	liveSessionColumnCount    = 16
	liveSessionUpdateArgCount = 2*liveSessionColumnCount - 1
)

type LiveSessionConflictError struct {
	VideoID   string
	Operation string
}

func (e *LiveSessionConflictError) Error() string {
	return fmt.Sprintf("%s %s: row changed after locked read", e.Operation, e.VideoID)
}

func persistLiveDecision(ctx context.Context, tx dbx.Tx, stored map[string]live.SessionState, decision *live.Decision) error {
	slices.SortFunc(decision.Sessions, func(a, b live.SessionState) int { return strings.Compare(a.VideoID, b.VideoID) })
	slices.SortFunc(decision.PendingEnds, func(a, b live.PendingEnd) int { return strings.Compare(a.VideoID, b.VideoID) })

	if err := persistLiveSessions(ctx, tx, stored, decision.Sessions); err != nil {
		return fmt.Errorf("persist live sessions and heads: %w", err)
	}

	if err := persistLiveEvidence(ctx, tx, decision); err != nil {
		return fmt.Errorf("persist live evidence: %w", err)
	}

	return nil
}

func liveDecisionRuntimeVideoIDs(decision *live.Decision) []string {
	videoIDs := make([]string, 0, len(decision.Sessions)+len(decision.PendingEnds))
	for i := range decision.Sessions {
		videoIDs = append(videoIDs, decision.Sessions[i].VideoID)
	}

	for i := range decision.PendingEnds {
		videoIDs = append(videoIDs, decision.PendingEnds[i].VideoID)
	}

	return videoIDs
}

func persistLiveSessions(ctx context.Context, tx dbx.Tx, stored map[string]live.SessionState, sessions []live.SessionState) error {
	for start := 0; start < len(sessions); start += liveSessionBatchSize {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("persist live sessions at %d: %w", start, err)
		}

		end := min(start+liveSessionBatchSize, len(sessions))
		statements := liveDecisionStatements(stored, sessions[start:end])

		if err := execLiveSessionStatements(ctx, tx, statements); err != nil {
			return fmt.Errorf("persist live session batch at %d: %w", start, err)
		}
	}

	return nil
}

func execLiveSessionStatements(ctx context.Context, tx dbx.Tx, statements []dbx.Statement) error {
	err := dbx.ExecStatements(ctx, tx, statements)
	if err == nil {
		return nil
	}

	missed, ok := errors.AsType[*dbx.NoRowsError](err)
	if !ok {
		return fmt.Errorf("exec session statements: %w", err)
	}

	return &LiveSessionConflictError{VideoID: fmt.Sprint(statements[missed.Index].Args[0]), Operation: missed.Operation}
}

func liveDecisionStatements(stored map[string]live.SessionState, sessions []live.SessionState) []dbx.Statement {
	statements := make([]dbx.Statement, 0, 2*len(sessions))
	for i := range sessions {
		session := &sessions[i]
		if session.ChannelID != "" {
			if statement, write := liveSessionWrite(stored, session); write {
				statements = append(statements, statement)
			}
		}

		if session.HeadPresent || session.LifecycleOrigin == live.OriginObserved {
			statements = append(statements, liveHeadStatement(session))
		}
	}

	return statements
}

func liveSessionWrite(stored map[string]live.SessionState, next *live.SessionState) (dbx.Statement, bool) {
	prior, ok := stored[next.VideoID]
	if !ok || !prior.Present {
		return dbx.Statement{
			Operation:  "insert live session",
			SQL:        repositoryLiveSessionInsertSQL,
			Args:       appendLiveSessionArgs(make([]any, 0, liveSessionColumnCount), next, next.Title),
			RequireRow: true,
		}, true
	}

	if sameLiveSessionRow(&prior, next) {
		return dbx.Statement{}, false
	}

	var title any

	if next.Title != prior.Title {
		title = next.Title
	}

	args := appendLiveSessionArgs(make([]any, 0, liveSessionUpdateArgCount), next, title)

	args = append(args,
		prior.ChannelID, string(prior.Status), prior.Title, prior.TopicID, prior.ThumbnailURL,
		prior.ScheduledStartTime, prior.StartedAt, prior.EndedAt, prior.LiveFirstSeenAt,
		prior.LastSeenAt, prior.IsPremiere, string(prior.LifecycleOrigin),
		prior.StatusObservedAt, prior.ScheduleObservedAt, prior.TitleObservedAt,
	)

	return dbx.Statement{Operation: "update live session", SQL: repositoryLiveSessionUpdateSQL, Args: args, RequireRow: true}, true
}

func appendLiveSessionArgs(args []any, session *live.SessionState, title any) []any {
	return append(args,
		session.VideoID, session.ChannelID, string(session.Status), title,
		session.TopicID, session.ThumbnailURL, session.ScheduledStartTime,
		session.StartedAt, session.EndedAt, session.LiveFirstSeenAt, session.LastSeenAt,
		session.IsPremiere,
		string(session.LifecycleOrigin),
		session.StatusObservedAt, session.ScheduleObservedAt, session.TitleObservedAt,
	)
}

func sameLiveSessionRow(a, b *live.SessionState) bool {
	return a.ChannelID == b.ChannelID && a.Status == b.Status && a.Title == b.Title &&
		a.TopicID == b.TopicID && a.ThumbnailURL == b.ThumbnailURL &&
		sameOptionalTime(a.ScheduledStartTime, b.ScheduledStartTime) &&
		sameOptionalTime(a.StartedAt, b.StartedAt) && sameOptionalTime(a.EndedAt, b.EndedAt) &&
		sameOptionalTime(a.LiveFirstSeenAt, b.LiveFirstSeenAt) && a.LastSeenAt.Equal(b.LastSeenAt) &&
		sameOptionalBool(a.IsPremiere, b.IsPremiere) && a.LifecycleOrigin == b.LifecycleOrigin &&
		sameOptionalTime(a.StatusObservedAt, b.StatusObservedAt) &&
		sameOptionalTime(a.ScheduleObservedAt, b.ScheduleObservedAt) &&
		sameOptionalTime(a.TitleObservedAt, b.TitleObservedAt)
}

func sameOptionalTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}

	return a.Equal(*b)
}

func sameOptionalBool(a, b *bool) bool {
	if a == nil || b == nil {
		return a == b
	}

	return *a == *b
}

func upsertConfirmedPremiereSession(ctx context.Context, tx dbx.Tx, session *live.SessionState) error {
	if session == nil {
		return errors.New("upsert live session: session state is nil")
	}

	if session.ChannelID == "" {
		return nil
	}

	if _, err := pgxdb.Exec(ctx, tx, repositoryLivePremiereClassificationUpsertSQL, appendLiveSessionArgs(make([]any, 0, liveSessionColumnCount), session, session.Title)...); err != nil {
		return fmt.Errorf("upsert live session: %w", err)
	}

	return nil
}

func liveHeadStatement(session *live.SessionState) dbx.Statement {
	var kind, observationID, nextCheck, reason any

	if session.Clock.EndCandidateKind != nil && session.Clock.EndCandidateObservationID != nil && session.Clock.NextEndCheckAt != nil {
		kind = string(*session.Clock.EndCandidateKind)
		observationID = *session.Clock.EndCandidateObservationID
		nextCheck = *session.Clock.NextEndCheckAt
	}

	if session.EndReason != nil {
		reason = string(*session.EndReason)
	}

	return dbx.Statement{
		Operation: "upsert live head",
		SQL:       liveHeadStatementSQL,
		Args: []any{
			session.VideoID, string(session.Status),
			session.Clock.LastUpcomingPositiveAt, session.Clock.LastUpcomingPositiveSeenAt,
			session.Clock.LastLivePositiveAt, session.Clock.LastLivePositiveSeenAt,
			session.Clock.LastEndEvidenceAt, session.Clock.LastCompleteAbsenceAt,
			session.LastAbsenceScheduledFor, session.Clock.ConsecutiveAbsenceSlots,
			kind, observationID, nextCheck, session.Clock.EndedAt, reason,
			session.FirstAbsenceScheduledFor, session.SecondAbsenceScheduledFor,
			session.LastAbsenceObservationID, ignoredAbsenceArg(session),
			session.Clock.UnresolvableSince,
		},
	}
}

func ignoredAbsenceArg(session *live.SessionState) any {
	if session.Status == domain.LiveStatusEnded {
		return []time.Time{}
	}

	slots, loaded := session.IgnoredAbsences.Slots()
	if !loaded {
		return nil
	}

	if slots == nil {
		return []time.Time{}
	}

	return slots
}
