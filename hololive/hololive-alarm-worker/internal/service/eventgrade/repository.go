package eventgrade

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	contract "github.com/kapu/hololive-shared/pkg/contracts/alarm"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

//go:embed queries/register.sql
var registerSQL string

//go:embed queries/catalog.sql
var catalogSQL string

//go:embed queries/chapter.sql
var chapterSQL string

//go:embed queries/block_binding.sql
var blockBindingSQL string

//go:embed queries/block_reassigned.sql
var blockReassignedSQL string

//go:embed queries/block_missing.sql
var blockMissingSQL string

//go:embed queries/border.sql
var borderSQL string

//go:embed queries/lock_chapter.sql
var lockChapterSQL string

//go:embed queries/snapshot_state.sql
var snapshotStateSQL string

//go:embed queries/delete_provisional.sql
var deleteProvisionalSQL string

//go:embed queries/invalidate_final.sql
var invalidateFinalSQL string

//go:embed queries/decide.sql
var decideSQL string

//go:embed queries/expire.sql
var expireSQL string

//go:embed queries/pending.sql
var pendingSQL string

//go:embed queries/settle_dispatch.sql
var settleDispatchSQL string

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

type Chapter struct {
	EventID     string    `json:"event_id"`
	ChapterID   string    `json:"chapter_id"`
	EventType   string    `json:"event_type"`
	StartsAt    time.Time `json:"starts_at"`
	EndsAt      time.Time `json:"ends_at"`
	EventEndsAt time.Time `json:"event_ends_at"`
	RevealsAt   time.Time `json:"reveals_at"`
}

var gameID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,96}$`)

func (c Chapter) valid() bool {
	return gameID.MatchString(c.EventID) && gameID.MatchString(c.ChapterID) && (c.EventType == "normal" || c.EventType == "relay") && !c.StartsAt.IsZero() && c.StartsAt.Before(c.EndsAt) && !c.EventEndsAt.Before(c.EndsAt) && !c.RevealsAt.Before(c.EndsAt)
}

func (r *Repository) RegisterEventGrade(ctx context.Context, req contract.EventGradeRequest) (contract.EventGradeResponse, error) {
	if !req.Valid() {
		return contract.EventGradeResponse{}, errors.New("invalid event grade registration")
	}

	response := contract.EventGradeResponse{Region: req.Region, Status: "registered"}
	err := r.pool.QueryRow(ctx, registerSQL, req.Region, req.RoomID, req.UserID, req.Score).Scan(&response.EventType, &response.EndsAt, &response.RevealsAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return contract.EventGradeResponse{Region: req.Region, Status: "unavailable"}, nil
	}

	if err != nil {
		return contract.EventGradeResponse{}, fmt.Errorf("persist grade request: %w", err)
	}

	return response, nil
}

func (r *Repository) StoreCatalog(ctx context.Context, region contract.GradeRegion, observed time.Time, chapters []Chapter) error {
	bindings := make([]ChapterBinding, len(chapters))
	for i := range chapters {
		bindings[i] = ChapterBinding{EventID: chapters[i].EventID, ChapterID: chapters[i].ChapterID}
	}

	return r.StoreCatalogSnapshot(ctx, region, observed, chapters, bindings, nil)
}

func (r *Repository) StoreCatalogSnapshot(ctx context.Context, region contract.GradeRegion, observed time.Time, chapters []Chapter, bindings []ChapterBinding, conflicts []string) error {
	return r.storeCatalogSnapshot(ctx, region, observed, chapters, bindings, conflicts, false)
}

func (r *Repository) storeCatalogSnapshot(ctx context.Context, region contract.GradeRegion, observed time.Time, chapters []Chapter, bindings []ChapterBinding, conflicts []string, quarantineMissing bool) error {
	if err := validateCatalogSnapshot(region, observed, chapters, bindings, conflicts); err != nil {
		return err
	}

	err := dbx.InPgxTxOptions(ctx, r.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		return r.storeCatalogSnapshotTx(ctx, tx, region, observed, chapters, bindings, conflicts, quarantineMissing)
	})
	if err != nil {
		return fmt.Errorf("commit event grade catalog: %w", err)
	}

	return nil
}

type ChapterBinding struct {
	EventID   string `json:"event_id"`
	ChapterID string `json:"chapter_id"`
}

func validateCatalogSnapshot(region contract.GradeRegion, observed time.Time, chapters []Chapter, bindings []ChapterBinding, conflicts []string) error {
	if !region.Valid() || observed.IsZero() || chapters == nil || bindings == nil || len(chapters) > 128 || len(bindings) > 128 || len(conflicts) > 128 {
		return fmt.Errorf("%w: invalid event grade catalog", errRejected)
	}

	seen, err := validateChapterBindings(bindings, conflicts)
	if err != nil {
		return err
	}

	return validateCatalogChapters(chapters, seen)
}

func validateChapterBindings(bindings []ChapterBinding, conflicts []string) (map[string]string, error) {
	seen := make(map[string]string, len(bindings)+len(conflicts))
	for _, binding := range bindings {
		if !gameID.MatchString(binding.EventID) || !gameID.MatchString(binding.ChapterID) {
			return nil, fmt.Errorf("%w: invalid chapter binding", errRejected)
		}

		if _, dup := seen[binding.ChapterID]; dup {
			return nil, fmt.Errorf("%w: duplicate chapter binding", errRejected)
		}

		seen[binding.ChapterID] = binding.EventID
	}

	for _, chapterID := range conflicts {
		if !gameID.MatchString(chapterID) {
			return nil, fmt.Errorf("%w: invalid conflicting chapter identity", errRejected)
		}

		if _, dup := seen[chapterID]; dup {
			return nil, fmt.Errorf("%w: confirmed chapter marked conflicting", errRejected)
		}

		seen[chapterID] = ""
	}

	return seen, nil
}

func validateCatalogChapters(chapters []Chapter, bindings map[string]string) error {
	seen := make(map[string]struct{}, len(chapters))
	for i := range chapters {
		c := &chapters[i]
		if !c.valid() {
			return fmt.Errorf("%w: invalid event grade chapter", errRejected)
		}

		if eventID, ok := bindings[c.ChapterID]; !ok || eventID != c.EventID {
			return fmt.Errorf("%w: chapter missing matching binding", errRejected)
		}

		if _, dup := seen[c.ChapterID]; dup {
			return fmt.Errorf("%w: duplicate chapter identity", errRejected)
		}

		seen[c.ChapterID] = struct{}{}
	}

	return nil
}

func (r *Repository) storeCatalogSnapshotTx(ctx context.Context, tx pgx.Tx, region contract.GradeRegion, observed time.Time, chapters []Chapter, bindings []ChapterBinding, conflicts []string, quarantineMissing bool) error {
	tag, err := tx.Exec(ctx, catalogSQL, region, observed)
	if err != nil {
		return fmt.Errorf("update catalog observation: %w", err)
	}

	if tag.RowsAffected() != 1 {
		return fmt.Errorf("%w: out-of-order catalog observation", errRejected)
	}

	if quarantineMissing {
		chapterIDs := make([]string, len(bindings))
		for i := range bindings {
			chapterIDs[i] = bindings[i].ChapterID
		}

		if _, execErr := tx.Exec(ctx, blockMissingSQL, region, chapterIDs); execErr != nil {
			return fmt.Errorf("block bindings missing after interrupted collection: %w", execErr)
		}
	}

	blocked, err := r.blockReassignedBindings(ctx, tx, region, bindings)
	if err != nil {
		return err
	}

	if len(conflicts) > 0 {
		if _, err := tx.Exec(ctx, blockBindingSQL, region, conflicts); err != nil {
			return fmt.Errorf("block conflicting chapter bindings: %w", err)
		}
	}

	for i := range chapters {
		c := &chapters[i]

		if _, conflict := blocked[c.ChapterID]; conflict {
			continue
		}

		var rescheduled bool

		if err := tx.QueryRow(ctx, chapterSQL, region, c.EventID, c.ChapterID, c.EventType, c.StartsAt, c.EndsAt, c.EventEndsAt, c.RevealsAt, observed).Scan(&rescheduled); err != nil {
			return fmt.Errorf("store chapter: %w", err)
		}

		if rescheduled {
			finalAfter := c.EventEndsAt
			if c.RevealsAt.After(finalAfter) {
				finalAfter = c.RevealsAt
			}

			if _, err := tx.Exec(ctx, invalidateFinalSQL, region, c.EventID, c.ChapterID, finalAfter); err != nil {
				return fmt.Errorf("invalidate rescheduled final border: %w", err)
			}
		}
	}

	return nil
}

func (r *Repository) blockReassignedBindings(ctx context.Context, tx pgx.Tx, region contract.GradeRegion, bindings []ChapterBinding) (map[string]struct{}, error) {
	eventIDs := make([]string, len(bindings))
	chapterIDs := make([]string, len(bindings))

	for i := range bindings {
		eventIDs[i] = bindings[i].EventID
		chapterIDs[i] = bindings[i].ChapterID
	}

	rows, err := tx.Query(ctx, blockReassignedSQL, region, eventIDs, chapterIDs)
	if err != nil {
		return nil, fmt.Errorf("block reassigned chapter bindings: %w", err)
	}

	blocked, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("read reassigned chapter bindings: %w", err)
	}

	result := make(map[string]struct{}, len(blocked))
	for _, chapterID := range blocked {
		result[chapterID] = struct{}{}
	}

	return result, nil
}

type Border struct {
	Region     contract.GradeRegion `json:"region"`
	EventID    string               `json:"event_id"`
	ChapterID  string               `json:"chapter_id"`
	BoardKind  string               `json:"board_kind"`
	StartedAt  time.Time            `json:"started_at"`
	ObservedAt time.Time            `json:"observed_at"`
	Final      *bool                `json:"final"`
	Ranks      []RankScore          `json:"ranks"`
}

type RankScore struct {
	Rank  int32  `json:"rank"`
	Score *int64 `json:"score,string"`
}

func gradeCutoffs(rows []RankScore) ([]int32, []int64, bool) {
	if len(rows) == 0 || len(rows) > 32 {
		return nil, nil, false
	}

	ranks := make([]int32, 0, len(rows))
	scores := make([]int64, 0, len(rows))

	for _, row := range rows {
		if row.Rank <= 0 || row.Score == nil || *row.Score < 0 {
			return nil, nil, false
		}

		i, duplicate := slices.BinarySearch(ranks, row.Rank)
		if duplicate {
			return nil, nil, false
		}

		ranks = append(ranks, 0)
		copy(ranks[i+1:], ranks[i:])

		ranks[i] = row.Rank

		scores = append(scores, 0)
		copy(scores[i+1:], scores[i:])

		scores[i] = *row.Score
	}

	for i := 1; i < len(scores); i++ {
		if scores[i-1] < scores[i] {
			return nil, nil, false
		}
	}

	return ranks, scores, true
}

func validateBorder(b *Border) ([]int32, []int64, error) {
	ranks, scores, ok := gradeCutoffs(b.Ranks)
	if !ok || !b.Region.Valid() || !gameID.MatchString(b.EventID) || !gameID.MatchString(b.ChapterID) || b.BoardKind != "marathon_score" || b.StartedAt.IsZero() || b.ObservedAt.IsZero() || b.StartedAt.After(b.ObservedAt) || b.Final == nil {
		return nil, nil, fmt.Errorf("%w: invalid marathon border", errRejected)
	}

	return ranks, scores, nil
}

func (r *Repository) StoreBorder(ctx context.Context, b Border) error {
	ranks, scores, err := validateBorder(&b)
	if err != nil {
		return err
	}

	err = dbx.InPgxTxOptions(ctx, r.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		return r.storeBorderTx(ctx, tx, &b, ranks, scores)
	})
	if err != nil {
		return fmt.Errorf("store marathon border: %w", err)
	}

	return nil
}

func (r *Repository) storeBorderTx(ctx context.Context, tx pgx.Tx, b *Border, ranks []int32, scores []int64) error {
	tag, err := tx.Exec(ctx, lockChapterSQL, b.Region, b.EventID, b.ChapterID, b.StartedAt, b.ObservedAt, *b.Final)
	if err != nil {
		return fmt.Errorf("lock border chapter: %w", err)
	}

	if tag.RowsAffected() != 1 {
		return fmt.Errorf("%w: border observation outside chapter window", errRejected)
	}

	var exists, accepted bool

	if err := tx.QueryRow(ctx, snapshotStateSQL, b.Region, b.EventID, b.ChapterID, *b.Final, b.ObservedAt, ranks, scores).Scan(&exists, &accepted); err != nil {
		return fmt.Errorf("read stored border snapshot: %w", err)
	}

	if exists {
		if !accepted {
			return fmt.Errorf("%w: border snapshot conflicts with stored observation", errRejected)
		}

		if *b.Final {
			return nil
		}

		if _, err := tx.Exec(ctx, deleteProvisionalSQL, b.Region, b.EventID, b.ChapterID); err != nil {
			return fmt.Errorf("remove previous provisional border: %w", err)
		}
	}

	if _, err := tx.Exec(ctx, borderSQL, b.Region, b.EventID, b.ChapterID, b.StartedAt, b.ObservedAt, *b.Final, ranks, scores); err != nil {
		return fmt.Errorf("insert border snapshot: %w", err)
	}

	return nil
}
