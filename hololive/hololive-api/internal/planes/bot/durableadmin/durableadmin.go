package durableadmin

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"text/tabwriter"
	"time"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/durability"
)

const (
	exitOK     = 0
	exitFailed = 1
	exitUsage  = 2

	operatorTimeout = 30 * time.Second

	defaultManualReviewListLimit = 50
	maxManualReviewListLimit     = 1000
)

const usageText = `usage: bot-durable-admin <command> [flags]

commands:
  list-manual-review [--limit=N]
  grant-replay   --outbox-id=ID --actor=ACTOR --reason=REASON
  discard        --outbox-id=ID --actor=ACTOR --reason=REASON --observed-iris-state=STATE

list-manual-review, grant-replay and discard attach to the running store at BOT_DURABLE_STORE_PATH.`

type operatorRequest struct {
	outboxID          int64
	actor             string
	reason            string
	observedIrisState string
}

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return usageExit(stderr, errors.New("missing command"))
	}

	logger := slog.New(slog.NewJSONHandler(stderr, nil))

	switch args[0] {
	case "grant-replay":
		return runOperator(ctx, args, stdout, stderr, logger, durability.ManualReviewReplayed,
			func(ctx context.Context, store *durability.Store, request operatorRequest) (durability.ManualReviewOutcome, error) {
				return store.GrantManualReplay(ctx, request.outboxID, request.actor, request.reason)
			})
	case "discard":
		return runOperator(ctx, args, stdout, stderr, logger, durability.ManualReviewDiscarded,
			func(ctx context.Context, store *durability.Store, request operatorRequest) (durability.ManualReviewOutcome, error) {
				return store.DiscardManualReview(ctx, request.outboxID, request.actor, request.reason, request.observedIrisState)
			})
	case "list-manual-review":
		return runListManualReview(ctx, args, stdout, stderr, logger)
	case "help", "-h", "--help":
		if _, err := fmt.Fprintln(stdout, usageText); err != nil {
			return exitFailed
		}

		return exitOK
	default:
		return usageExit(stderr, fmt.Errorf("unknown command %q", args[0]))
	}
}

func runOperator(
	ctx context.Context,
	args []string,
	stdout, stderr io.Writer,
	logger *slog.Logger,
	applied durability.ManualReviewOutcome,
	apply func(context.Context, *durability.Store, operatorRequest) (durability.ManualReviewOutcome, error),
) int {
	request, err := parseOperatorRequest(args[0], args[1:], stderr)
	if err != nil {
		return usageExit(stderr, err)
	}

	var outcome durability.ManualReviewOutcome

	err = withAttachedStore(ctx, logger, func(ctx context.Context, store *durability.Store) error {
		var applyErr error

		outcome, applyErr = apply(ctx, store, request)

		return applyErr
	})

	if outcome != "" {
		if _, writeErr := fmt.Fprintf(stdout, "outcome=%s\n", outcome); writeErr != nil {
			err = errors.Join(err, fmt.Errorf("write operator outcome: %w", writeErr))
		}
	}

	if err != nil {
		logger.Error("apply operator decision", slog.String("command", args[0]), slog.Any("error", err))

		return exitFailed
	}

	if outcome != applied {
		return exitFailed
	}

	return exitOK
}

func parseOperatorRequest(command string, args []string, stderr io.Writer) (operatorRequest, error) {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(stderr)

	var request operatorRequest

	flags.Int64Var(&request.outboxID, "outbox-id", 0, "bot_reply_outbox id in manual_review")
	flags.StringVar(&request.actor, "actor", "", "operator identity recorded in the audit row")
	flags.StringVar(&request.reason, "reason", "", "reason recorded in the audit row")

	if command == "discard" {
		flags.StringVar(&request.observedIrisState, "observed-iris-state", "", "Iris state observed for the reply client request id")
	}

	if err := flags.Parse(args); err != nil {
		return operatorRequest{}, fmt.Errorf("parse %s flags: %w", command, err)
	}

	if flags.NArg() != 0 {
		return operatorRequest{}, fmt.Errorf("%s takes no positional arguments, got %d", command, flags.NArg())
	}

	if request.outboxID <= 0 {
		return operatorRequest{}, fmt.Errorf("%s requires a positive --outbox-id", command)
	}

	return request, nil
}

func runListManualReview(ctx context.Context, args []string, stdout, stderr io.Writer, logger *slog.Logger) int {
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(stderr)

	limit := flags.Int("limit", defaultManualReviewListLimit, "maximum manual_review rows to print, oldest update first")

	if err := flags.Parse(args[1:]); err != nil {
		return usageExit(stderr, fmt.Errorf("parse %s flags: %w", args[0], err))
	}

	if flags.NArg() != 0 || *limit < 1 || *limit > maxManualReviewListLimit {
		return usageExit(stderr, fmt.Errorf("%s takes only --limit between 1 and %d", args[0], maxManualReviewListLimit))
	}

	var rows []durability.ManualReviewRow

	if err := withAttachedStore(ctx, logger, func(ctx context.Context, store *durability.Store) error {
		listed, listErr := store.ManualReviewRows(ctx, *limit)
		if listErr != nil {
			return fmt.Errorf("read manual review rows: %w", listErr)
		}

		rows = listed

		return nil
	}); err != nil {
		logger.Error("list manual review rows", slog.Any("error", err))

		return exitFailed
	}

	if err := writeManualReviewRows(stdout, rows); err != nil {
		logger.Error("write manual review rows", slog.Any("error", err))

		return exitFailed
	}

	return exitOK
}

func writeManualReviewRows(w io.Writer, rows []durability.ManualReviewRow) error {
	table := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)

	if _, err := fmt.Fprintln(table, "id\tclient_request_id\tiris_request_id\tattempts\treplay_grants\tcreated_at\tupdated_at\tlast_error"); err != nil {
		return fmt.Errorf("write header: %w", err)
	}

	for _, row := range rows {
		if _, err := fmt.Fprintf(table, "%d\t%s\t%s\t%d\t%d\t%s\t%s\t%s\n", row.ID, row.ClientRequestID, row.IrisRequestID,
			row.Attempts, row.OperatorReplayGrants, row.CreatedAt.UTC().Format(time.RFC3339),
			row.UpdatedAt.UTC().Format(time.RFC3339), row.LastError); err != nil {
			return fmt.Errorf("write row %d: %w", row.ID, err)
		}
	}

	if err := table.Flush(); err != nil {
		return fmt.Errorf("flush table: %w", err)
	}

	return nil
}

func withAttachedStore(ctx context.Context, logger *slog.Logger, fn func(context.Context, *durability.Store) error) (err error) {
	path, err := apiconfig.LoadBotDurableStorePath()
	if err != nil {
		return fmt.Errorf("load bot durable store path: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, operatorTimeout)
	defer cancel()

	store, err := durability.AttachStore(ctx, durability.StoreOptions(path, logger))
	if err != nil {
		return fmt.Errorf("attach bot durable store: %w", err)
	}

	defer func() { err = errors.Join(err, store.Close(context.WithoutCancel(ctx))) }()

	return fn(ctx, store)
}

func usageExit(stderr io.Writer, err error) int {
	if errors.Is(err, flag.ErrHelp) {
		return exitOK
	}

	if _, writeErr := fmt.Fprintf(stderr, "%v\n\n%s\n", err, usageText); writeErr != nil {
		return exitFailed
	}

	return exitUsage
}
