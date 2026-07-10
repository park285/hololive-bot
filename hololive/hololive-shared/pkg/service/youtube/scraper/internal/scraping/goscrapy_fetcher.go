package scraping

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/panicguard"
	"github.com/park285/shared-go/pkg/jsonutil"
	"github.com/tech-engine/goscrapy/pkg/core"
	"github.com/tech-engine/goscrapy/pkg/engine"
	"github.com/tech-engine/goscrapy/pkg/gos"
	goslogger "github.com/tech-engine/goscrapy/pkg/logger"
)

type goscrapyRunner interface {
	Run(ctx context.Context, client *Client, req pageFetchRequest) (pageFetchResponse, bool, error)
}

type goscrapyPageFetcher struct {
	client   *Client
	runner   goscrapyRunner
	fallback pageFetcher
}

type defaultGoscrapyRunner struct{}

type discardGoScrapyPipeline struct{}

type goscrapyFetchResult struct {
	response    pageFetchResponse
	gotResponse bool
	err         error
}

type goscrapyWaitKind uint8

const (
	goscrapyWaitResult goscrapyWaitKind = iota
	goscrapyWaitEngine
	goscrapyWaitCanceled
)

type goscrapyWaitEvent struct {
	kind   goscrapyWaitKind
	result goscrapyFetchResult
	err    error
}

type goscrapyWaitOutcome struct {
	response    pageFetchResponse
	gotResponse bool
	err         error
	done        bool
}

type goscrapyWaitState struct {
	ctx      context.Context
	cancel   context.CancelFunc
	resultCh <-chan goscrapyFetchResult
	errCh    <-chan error
	eventCh  <-chan goscrapyWaitEvent
}

var goscrapyWaitFinishers = []func(goscrapyWaitState, *goscrapyWaitEvent) goscrapyWaitOutcome{
	finishGoScrapyResultEvent,
	finishGoScrapyEngineEvent,
	finishGoScrapyCanceledEvent,
}

func (f goscrapyPageFetcher) FetchPage(ctx context.Context, req pageFetchRequest) (pageFetchResponse, error) {
	runner := f.runner
	if runner == nil {
		runner = defaultGoscrapyRunner{}
	}

	resp, gotResponse, err := runner.Run(ctx, f.client, req)
	if err != nil && !gotResponse && f.fallback != nil {
		observeScraperFetchFallback(FetcherEngineGoScrapy, FetcherEngineNetHTTP, err)
		slog.Warn("goscrapy fetch failed before response, falling back to nethttp",
			"url", safeFetchURL(req.URL),
			"error", safeFetchError(err, req.URL))
		return f.fallback.FetchPage(ctx, req)
	}
	return resp, err
}

func (defaultGoscrapyRunner) Run(ctx context.Context, client *Client, req pageFetchRequest) (pageFetchResponse, bool, error) {
	if err := ctx.Err(); err != nil {
		return pageFetchResponse{}, false, fmt.Errorf("goscrapy fetch canceled: %w", err)
	}

	appCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	app, err := newGoScrapyFetchApp(client, nil)
	if err != nil {
		return pageFetchResponse{}, false, fmt.Errorf("goscrapy init: %w", err)
	}

	resultCh := make(chan goscrapyFetchResult, 1)

	appReq := app.Request(appCtx)
	appReq.Url(req.URL).Method(http.MethodGet).Header(req.Header.Clone())
	app.Parse(appReq, func(_ context.Context, resp core.IResponseReader) {
		out, err := readGoScrapyResponse(resp)
		select {
		case resultCh <- goscrapyFetchResult{response: out, gotResponse: true, err: err}:
		default:
		}
		cancel()
	})

	signals := startGoScrapyEngineSignals(ctx, appCtx, app.Start)
	defer signals.stopContextWatch()

	waitState := goscrapyWaitState{
		ctx:      ctx,
		cancel:   cancel,
		resultCh: resultCh,
		errCh:    signals.errCh,
		eventCh:  signals.eventCh,
	}
	return waitForGoScrapyFetch(waitState)
}

func newGoScrapyFetchApp(client *Client, logger core.ILogger) (*gos.App[struct{}], error) {
	if logger == nil {
		logger = goslogger.NewNoopLogger()
	}

	app, err := gos.New[struct{}](&gos.Config{
		Client: client.currentHTTPClient(),
		Logger: logger,
	})
	if err != nil {
		return nil, err
	}
	app.WithPipelines(discardGoScrapyPipeline{})
	return app, nil
}

func (discardGoScrapyPipeline) Open(context.Context) error {
	return nil
}

func (discardGoScrapyPipeline) ProcessItem(engine.IPipelineItem, core.IOutput[struct{}]) error {
	return nil
}

func (discardGoScrapyPipeline) Close() {
}

type goscrapyEngineSignals struct {
	errCh            <-chan error
	eventCh          <-chan goscrapyWaitEvent
	stopContextWatch func() bool
}

func startGoScrapyEngineSignals(ctx, appCtx context.Context, start func(context.Context) error) goscrapyEngineSignals {
	errCh := make(chan error, 1)
	eventCh := make(chan goscrapyWaitEvent, 2)
	panicguard.Go(nil, "goscrapy-engine", func() {
		err := panicguard.RunE(nil, "goscrapy-engine", func() error {
			return start(appCtx)
		})
		errCh <- err
		eventCh <- goscrapyWaitEvent{kind: goscrapyWaitEngine, err: err}
	})
	stopContextWatch := context.AfterFunc(ctx, func() {
		eventCh <- goscrapyWaitEvent{kind: goscrapyWaitCanceled}
	})
	return goscrapyEngineSignals{errCh: errCh, eventCh: eventCh, stopContextWatch: stopContextWatch}
}

func waitForGoScrapyFetch(state goscrapyWaitState) (pageFetchResponse, bool, error) {
	for {
		event := nextGoScrapyWaitEvent(state)
		outcome := goscrapyWaitFinishers[event.kind](state, &event)
		if outcome.done {
			return outcome.response, outcome.gotResponse, outcome.err
		}
	}
}

func nextGoScrapyWaitEvent(state goscrapyWaitState) goscrapyWaitEvent {
	select {
	case result := <-state.resultCh:
		return goscrapyWaitEvent{kind: goscrapyWaitResult, result: result}
	case event := <-state.eventCh:
		return event
	}
}

func finishGoScrapyResultEvent(state goscrapyWaitState, event *goscrapyWaitEvent) goscrapyWaitOutcome {
	response, gotResponse, err := finishGoScrapyResult(&event.result, state.cancel, state.errCh)
	return goscrapyWaitOutcome{response: response, gotResponse: gotResponse, err: err, done: true}
}

func finishGoScrapyEngineEvent(state goscrapyWaitState, event *goscrapyWaitEvent) goscrapyWaitOutcome {
	response, gotResponse, err := finishGoScrapyEngineError(event.err, state.resultCh)
	return goscrapyWaitOutcome{response: response, gotResponse: gotResponse, err: err, done: true}
}

func finishGoScrapyCanceledEvent(state goscrapyWaitState, _ *goscrapyWaitEvent) goscrapyWaitOutcome {
	response, gotResponse, err := finishCanceledGoScrapy(state)
	return goscrapyWaitOutcome{response: response, gotResponse: gotResponse, err: err, done: true}
}

func finishCanceledGoScrapy(state goscrapyWaitState) (pageFetchResponse, bool, error) {
	state.cancel()
	if result, ok := pollGoScrapyResult(state.resultCh); ok {
		waitGoScrapyEngine(state.errCh)
		return result.response, result.gotResponse, result.err
	}
	waitGoScrapyEngine(state.errCh)
	return pageFetchResponse{}, false, fmt.Errorf("goscrapy fetch canceled: %w", state.ctx.Err())
}

func finishGoScrapyEngineError(err error, resultCh <-chan goscrapyFetchResult) (pageFetchResponse, bool, error) {
	if result, ok := pollGoScrapyResult(resultCh); ok {
		return result.response, result.gotResponse, result.err
	}
	if err == nil {
		err = errors.New("goscrapy stopped before response")
	}
	return pageFetchResponse{}, false, fmt.Errorf("goscrapy fetch page: %w", err)
}

func finishGoScrapyResult(result *goscrapyFetchResult, cancel context.CancelFunc, errCh <-chan error) (pageFetchResponse, bool, error) {
	cancel()
	waitGoScrapyEngine(errCh)
	return result.response, result.gotResponse, result.err
}

func pollGoScrapyResult(resultCh <-chan goscrapyFetchResult) (goscrapyFetchResult, bool) {
	select {
	case result := <-resultCh:
		return result, true
	default:
		return goscrapyFetchResult{}, false
	}
}

func readGoScrapyResponse(resp core.IResponseReader) (pageFetchResponse, error) {
	out := pageFetchResponse{
		StatusCode: resp.StatusCode(),
		Header:     resp.Header().Clone(),
	}
	body := resp.Body()
	if body == nil {
		return out, nil
	}

	if out.StatusCode != http.StatusOK {
		return out, drainGoScrapyResponseBody(body)
	}

	data, err := readSuccessfulGoScrapyResponseBody(body)
	if err != nil {
		return out, err
	}
	out.Body = data
	return out, nil
}

func drainGoScrapyResponseBody(body io.ReadCloser) error {
	_, readErr := jsonutil.ReadAllLimit(body, 4*1024)
	closeErr := body.Close()
	if readErr != nil && !errors.Is(readErr, jsonutil.ErrBodyTooLarge) {
		if closeErr != nil {
			return fmt.Errorf("drain goscrapy response body: %w", errors.Join(readErr, fmt.Errorf("close goscrapy response body: %w", closeErr)))
		}
		return fmt.Errorf("drain goscrapy response body: %w", readErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close goscrapy response body: %w", closeErr)
	}
	return nil
}

func readSuccessfulGoScrapyResponseBody(body io.ReadCloser) ([]byte, error) {
	data, err := jsonutil.ReadAllLimit(body, ytDefaults.MaxPageBodyBytes)
	closeErr := body.Close()
	if err != nil {
		if closeErr != nil {
			return nil, fmt.Errorf("read goscrapy response body: %w", errors.Join(err, fmt.Errorf("close goscrapy response body: %w", closeErr)))
		}
		return nil, responseBodyReadError(err)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close goscrapy response body: %w", closeErr)
	}
	return data, nil
}

func waitGoScrapyEngine(errCh <-chan error) {
	select {
	case <-errCh:
	case <-time.After(100 * time.Millisecond):
	}
}

func safeFetchURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "invalid-url"
	}
	return parsed.Scheme + "://" + parsed.Host + parsed.Path
}

func safeFetchError(err error, requestURL string) string {
	if err == nil {
		return ""
	}

	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		safeURL := safeFetchURL(urlErr.URL)
		if safeURL == "invalid-url" {
			safeURL = safeFetchURL(requestURL)
		}
		op := strings.TrimSpace(urlErr.Op)
		if op == "" {
			op = "url error"
		}
		if urlErr.Err == nil {
			return fmt.Sprintf("%s %s", op, safeURL)
		}
		return fmt.Sprintf("%s %s: %s", op, safeURL, sanitizeFetchErrorDetail(urlErr.Err.Error(), urlErr.URL, safeURL))
	}

	return sanitizeFetchErrorDetail(err.Error(), requestURL, safeFetchURL(requestURL))
}

func sanitizeFetchErrorDetail(message, rawURL, safeURL string) string {
	if message == "" {
		return ""
	}
	if rawURL == "" {
		return message
	}

	message = strings.ReplaceAll(message, rawURL, safeURL)
	if parsed, err := url.Parse(rawURL); err == nil && parsed.RawQuery != "" {
		message = strings.ReplaceAll(message, "?"+parsed.RawQuery, "")
		message = strings.ReplaceAll(message, parsed.RawQuery, "redacted")
	}
	return message
}
