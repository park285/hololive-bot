package eventgrade

import (
	"context"
	"crypto/rand"
	_ "embed"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	observation "github.com/kapu/hololive-shared/pkg/contracts/eventgrade"
)

//go:embed queries/claim_catalog.sql
var claimCatalogSQL string

//go:embed queries/claim_cutoffs.sql
var claimCutoffsSQL string

//go:embed queries/publish.sql
var publishSQL string

//go:embed queries/release.sql
var releaseSQL string

//go:embed queries/session_user.sql
var sessionUserSQL string

const (
	CollectorRole   = "hololive_collector_c"
	cycleInterval   = time.Minute
	leaseTTL        = time.Minute
	cutoffsPerCycle = 3
	releaseTimeout  = 5 * time.Second
)

var regions = [...]string{"jp", "us", "as"}

type gameClient interface {
	catalog(ctx context.Context, region string) (catalogResponse, error)
	border(ctx context.Context, req borderRequest) (borderResponse, error)
	acknowledge(ctx context.Context, region string, conflicts []string) error
}

type Runner struct {
	pool   *pgxpool.Pool
	game   gameClient
	owner  string
	logger *slog.Logger
}

type lease struct {
	jobKey    string
	epoch     int64
	claimedAt time.Time
}

type cutoffLease struct {
	lease

	eventID           string
	chapterID         string
	finalAfter        time.Time
	bindingObservedAt time.Time
}

func VerifySession(ctx context.Context, pool *pgxpool.Pool) error {
	var role string

	if err := pool.QueryRow(ctx, sessionUserSQL).Scan(&role); err != nil {
		return fmt.Errorf("read event grade database identity: %w", err)
	}

	if role != CollectorRole {
		return errors.New("event grade collection requires the collector c database login")
	}

	return nil
}

func NewRunner(pool *pgxpool.Pool, game gameClient, instanceID string, logger *slog.Logger) *Runner {
	return &Runner{pool: pool, game: game, owner: instanceID + ":" + rand.Text(), logger: logger}
}

func (r *Runner) Run(ctx context.Context, helperDone <-chan struct{}) error {
	ticker := time.NewTicker(cycleInterval)
	defer ticker.Stop()

	for {
		r.Cycle(ctx)

		select {
		case <-ctx.Done():
			return nil
		case <-helperDone:
			return errors.New("event grade helper exited")
		case <-ticker.C:
		}
	}
}

func (r *Runner) Cycle(ctx context.Context) {
	for _, region := range regions {
		if ctx.Err() != nil {
			return
		}

		if err := r.collectCatalog(ctx, region); err != nil {
			r.logger.WarnContext(ctx, "Event grade catalog collection failed", slog.String("region", region), slog.Any("error", err))
		}

		if err := r.collectCutoffs(ctx, region); err != nil {
			r.logger.WarnContext(ctx, "Event grade cutoff collection failed", slog.String("region", region), slog.Any("error", err))
		}
	}
}

func (r *Runner) collectCatalog(ctx context.Context, region string) error {
	var claim lease

	err := r.pool.QueryRow(ctx, claimCatalogSQL, region, r.owner, leaseTTL.Milliseconds()).Scan(&claim.jobKey, &claim.epoch, &claim.claimedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("claim catalog: %w", err)
	}

	started := time.Now().UTC().Truncate(time.Millisecond)

	resp, err := r.game.catalog(ctx, region)
	if err != nil {
		return errors.Join(fmt.Errorf("collect catalog: %w", err), r.release(ctx, claim))
	}

	observed := time.Now().UTC().Truncate(time.Millisecond)

	if resp.Version != protocolVersion || resp.Region != region || resp.Chapters == nil || resp.Bindings == nil || resp.Conflicts == nil || resp.QuarantinePending == nil {
		return errors.Join(errors.New("invalid event grade catalog identity"), r.release(ctx, claim))
	}

	body := observation.CatalogObservation{Chapters: resp.Chapters, Bindings: resp.Bindings, Conflicts: resp.Conflicts, QuarantineMissing: *resp.QuarantinePending}
	if err := r.publish(ctx, claim, started, observed, body); err != nil {
		return err
	}

	if len(resp.Conflicts) > 0 {
		if err := r.game.acknowledge(ctx, region, resp.Conflicts); err != nil {
			r.logger.WarnContext(ctx, "Event grade quarantine acknowledgement failed", slog.String("region", region), slog.Any("error", err))
		}
	}

	return nil
}

func (r *Runner) collectCutoffs(ctx context.Context, region string) error {
	rows, err := r.pool.Query(ctx, claimCutoffsSQL, region, r.owner, leaseTTL.Milliseconds(), cutoffsPerCycle)
	if err != nil {
		return fmt.Errorf("claim cutoffs: %w", err)
	}

	claims, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (cutoffLease, error) {
		var c cutoffLease

		scanErr := row.Scan(&c.jobKey, &c.eventID, &c.chapterID, &c.finalAfter, &c.bindingObservedAt, &c.epoch, &c.claimedAt)

		return c, scanErr
	})
	if err != nil {
		return fmt.Errorf("decode claimed cutoffs: %w", err)
	}

	var errs []error

	for i := range claims {
		if err := r.collectCutoff(ctx, region, &claims[i]); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

func (r *Runner) collectCutoff(ctx context.Context, region string, claim *cutoffLease) error {
	resp, err := r.game.border(ctx, borderRequest{
		Region: region, EventID: claim.eventID, ChapterID: claim.chapterID, FinalAfter: claim.finalAfter, BindingObservedAt: claim.bindingObservedAt,
	})
	if err != nil {
		return errors.Join(fmt.Errorf("collect cutoff %s: %w", claim.chapterID, err), r.release(ctx, claim.lease))
	}

	b := resp.Border
	if resp.Version != protocolVersion || b.Region != region || b.EventID != claim.eventID || b.ChapterID != claim.chapterID ||
		b.BoardKind != observation.BoardKindScore || b.Final == nil || b.Ranks == nil {
		return errors.Join(fmt.Errorf("invalid event grade cutoff identity for %s", claim.chapterID), r.release(ctx, claim.lease))
	}

	body := observation.CutoffObservation{EventID: b.EventID, ChapterID: b.ChapterID, BoardKind: b.BoardKind, Final: b.Final, Ranks: b.Ranks}

	return r.publish(ctx, claim.lease, b.StartedAt, b.ObservedAt, body)
}

func (r *Runner) publish(ctx context.Context, claim lease, started, observed time.Time, body any) error {
	payload, err := jsonv2.Marshal(body)
	if err != nil {
		return errors.Join(fmt.Errorf("encode event grade observation: %w", err), r.release(ctx, claim))
	}

	var seq int64

	if err := r.pool.QueryRow(ctx, publishSQL, claim.jobKey, r.owner, claim.epoch, started, observed, string(payload)).Scan(&seq); err != nil {
		return errors.Join(fmt.Errorf("publish %s: %w", claim.jobKey, err), r.release(ctx, claim))
	}

	return nil
}

func (r *Runner) release(ctx context.Context, claim lease) error {
	releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), releaseTimeout)
	defer cancel()

	var released bool

	if err := r.pool.QueryRow(releaseCtx, releaseSQL, claim.jobKey, r.owner, claim.epoch).Scan(&released); err != nil {
		return fmt.Errorf("release %s: %w", claim.jobKey, err)
	}

	return nil
}
