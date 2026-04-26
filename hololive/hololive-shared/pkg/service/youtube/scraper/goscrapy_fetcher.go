package scraper

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/jsonutil"
	"github.com/tech-engine/goscrapy/pkg/core"
	"github.com/tech-engine/goscrapy/pkg/gos"
	goslogger "github.com/tech-engine/goscrapy/pkg/logger"

	"github.com/kapu/hololive-shared/pkg/constants"
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

type goscrapyFetchResult struct {
	response    pageFetchResponse
	gotResponse bool
	err         error
}

func (f goscrapyPageFetcher) FetchPage(ctx context.Context, req pageFetchRequest) (pageFetchResponse, error) {
	runner := f.runner
	if runner == nil {
		runner = defaultGoscrapyRunner{}
	}

	resp, gotResponse, err := runner.Run(ctx, f.client, req)
	if err != nil && !gotResponse && f.fallback != nil {
		slog.Warn("goscrapy fetch failed before response, falling back to nethttp",
			"url", safeFetchURL(req.URL),
			"error", err.Error())
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

	app := gos.New[struct{}](gos.WithClient(client.currentHTTPClient())).WithLogger(goslogger.NewNoopLogger())
	resultCh := make(chan goscrapyFetchResult, 1)
	errCh := make(chan error, 1)

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

	go func() {
		errCh <- app.Start(appCtx)
	}()

	select {
	case result := <-resultCh:
		cancel()
		waitGoScrapyEngine(errCh)
		return result.response, result.gotResponse, result.err
	case err := <-errCh:
		select {
		case result := <-resultCh:
			return result.response, result.gotResponse, result.err
		default:
		}
		if err == nil {
			err = errors.New("goscrapy stopped before response")
		}
		return pageFetchResponse{}, false, fmt.Errorf("goscrapy fetch page: %w", err)
	case <-ctx.Done():
		cancel()
		select {
		case result := <-resultCh:
			waitGoScrapyEngine(errCh)
			return result.response, result.gotResponse, result.err
		default:
		}
		waitGoScrapyEngine(errCh)
		return pageFetchResponse{}, false, fmt.Errorf("goscrapy fetch canceled: %w", ctx.Err())
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
		_, _ = jsonutil.ReadAllLimit(body, 4*1024)
		return out, nil
	}

	data, err := jsonutil.ReadAllLimit(body, constants.YouTubeConfig.MaxPageBodyBytes)
	if err != nil {
		return out, fmt.Errorf("failed to read response body: %w", err)
	}
	out.Body = data
	return out, nil
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
