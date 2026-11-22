package geminidecision

import (
	"bytes"
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"sync/atomic"
	"time"

	"github.com/park285/shared-go/v2/pkg/httputil"
	"github.com/park285/shared-go/v2/pkg/netguard"
)

const Model = "gemini-decision-1"

const (
	apiHost          = "generativelanguage.googleapis.com"
	baseURL          = "https://" + apiHost
	decisionsURL     = baseURL + "/v1beta/decisions"
	requestTimeout   = 2 * time.Minute
	maxResponseBytes = 64 << 10
)

var (
	ErrUnavailable     = errors.New("gemini decisions api is unavailable")
	ErrBlocked         = errors.New("gemini decisions input blocked")
	ErrRejected        = errors.New("gemini decisions request rejected")
	ErrInvalidResponse = errors.New("decode decision response")
)

type Answer struct {
	Type          string             `json:"type"`
	Noul          *float64           `json:"noul"`
	Choice        *string            `json:"choice"`
	Confidence    *float64           `json:"confidence"`
	Probabilities map[string]float64 `json:"probabilities"`
}

type Feedback struct {
	BlockReason string `json:"block_reason"`
}

type Response = Result[map[string]Answer, Feedback]

type Result[A, F any] struct {
	Answers A `json:"answers"`
	Usage   struct {
		TotalInputTokens int32 `json:"total_input_tokens"`
	} `json:"usage"`
	PromptFeedback F `json:"prompt_feedback"`
}

type Client struct {
	http *http.Client
	key  string
}

func GuardedHTTPClient() *http.Client {
	policy := netguard.Policy{
		Timeout:      10 * time.Second,
		AllowedHosts: []string{apiHost},
		AllowedPorts: []string{"443"},
		Schemes:      []string{"https"},
	}

	client := netguard.GuardedClient(&http.Client{Timeout: requestTimeout}, policy)

	client.CheckRedirect = netguard.RedirectPolicy(netguard.RedirectConfig{Policy: policy, DisableFollow: true})

	return client
}

func New(key string) *Client {
	return NewWithHTTPClient(key, GuardedHTTPClient())
}

func NewWithHTTPClient(key string, client *http.Client) *Client {
	bounded := *client

	bounded.CheckRedirect = netguard.RedirectPolicy(netguard.RedirectConfig{DisableFollow: true})

	return &Client{http: &bounded, key: key}
}

func (c *Client) Create[A, F any](ctx context.Context, request Request) (Result[A, F], error) {
	body, err := request.encode()
	if err != nil {
		return Result[A, F]{}, fmt.Errorf("encode decision request: %w", SafeClientError(err))
	}

	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	var connected atomic.Bool

	ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{GotConn: func(httptrace.GotConnInfo) { connected.Store(true) }})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, decisionsURL, bytes.NewReader(body))
	if err != nil {
		return Result[A, F]{}, fmt.Errorf("create decision request: %w", SafeClientError(err))
	}

	req.GetBody = nil
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", c.key)

	resp, err := c.http.Do(req)
	if err != nil {
		return Result[A, F]{}, fmt.Errorf("create decision: %w", sendFailure(err, connected.Load()))
	}

	defer resp.Body.Close()

	raw, err := httputil.ReadAllLimited(resp.Body, maxResponseBytes)
	if err != nil {
		return Result[A, F]{}, fmt.Errorf("read decision response: %w", SafeClientError(err))
	}

	if resp.StatusCode != http.StatusOK {
		return Result[A, F]{}, failure(resp.StatusCode, raw)
	}

	var response Result[A, F]

	if err := jsonv2.Unmarshal(raw, &response); err != nil {
		return Result[A, F]{}, ErrInvalidResponse
	}

	return response, nil
}

func sendFailure(err error, connected bool) error {
	safe := SafeClientError(err)

	if urlErr, ok := errors.AsType[*url.Error](safe); ok {
		if cause, ok := errors.AsType[transportError](urlErr.Err); ok {
			cause.timedOut = connected && errors.Is(err, context.DeadlineExceeded)
			urlErr.Err = cause
		}
	}

	return safe
}

func (c *Client) Decide(ctx context.Context, request Request) (Response, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	response, err := c.Create[map[string]Answer, Feedback](ctx, request)
	if err != nil {
		if _, ok := errors.AsType[*url.Error](err); ok && ctx.Err() == nil {
			return Response{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
		}

		return Response{}, err
	}

	if response.PromptFeedback.BlockReason != "" {
		return Response{}, ErrBlocked
	}

	return response, nil
}
