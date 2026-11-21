package geminidecision

import (
	"bytes"
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"sync/atomic"
	"time"

	"github.com/park285/shared-go/v2/pkg/httputil"
	"github.com/park285/shared-go/v2/pkg/netguard"
	"google.golang.org/genai/interactions"
	"google.golang.org/genai/interactions/models/apierrors"
	"google.golang.org/genai/interactions/models/components"
	"google.golang.org/genai/interactions/models/decisions"
	"google.golang.org/genai/interactions/models/operations"
	"google.golang.org/genai/interactions/retry"
)

const Model = "gemini-decision-1"

const (
	apiHost          = "generativelanguage.googleapis.com"
	baseURL          = "https://" + apiHost
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
	decisions *interactions.Decisions
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
	transport := bounded.Transport

	if transport == nil {
		transport = http.DefaultTransport
	}

	bounded.Transport = decisionTransport{base: transport}
	bounded.CheckRedirect = netguard.RedirectPolicy(netguard.RedirectConfig{DisableFollow: true})

	sdk := interactions.New(
		interactions.WithClient(&bounded),
		interactions.WithServerURL(baseURL),
		interactions.WithAPIVersion("v1beta"),
		interactions.WithSecurity(components.Security{APIKey: &key}),
		interactions.WithTimeout(requestTimeout),
	)

	return &Client{decisions: sdk.Decisions}
}

type callKey struct{}

type decisionCall struct {
	request Request
	status  int
	data    []byte
	bodyErr error
}

type decisionTransport struct {
	base http.RoundTripper
}

func (t decisionTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	call, ok := req.Context().Value(callKey{}).(*decisionCall)
	if !ok {
		return nil, errors.New("missing decision call")
	}

	var awaitingConn atomic.Bool

	ctx := httptrace.WithClientTrace(req.Context(), &httptrace.ClientTrace{
		GetConn: func(string) { awaitingConn.Store(true) },
		GotConn: func(httptrace.GotConnInfo) { awaitingConn.Store(false) },
	})

	out, err := call.request.prepareHTTPRequest(ctx, req)
	if err != nil {
		call.bodyErr = fmt.Errorf("prepare decision request: %w", SafeClientError(err))

		return nil, call.bodyErr
	}

	resp, err := t.base.RoundTrip(out)
	if err != nil {
		if deadline := ctx.Err(); !awaitingConn.Load() && errors.Is(deadline, context.DeadlineExceeded) && errors.Is(err, deadline) {
			return nil, deadline
		}

		return nil, err
	}

	if resp == nil {
		return nil, errors.New("decision transport returned no response")
	}

	call.status = resp.StatusCode

	if resp.Body == nil {
		return resp, nil
	}

	defer resp.Body.Close()

	raw, err := httputil.ReadAllLimited(resp.Body, maxResponseBytes)
	if err != nil {
		call.bodyErr = fmt.Errorf("read decision response: %w", SafeClientError(err))

		return nil, call.bodyErr
	}

	call.data = raw
	resp.Body = io.NopCloser(bytes.NewReader(raw))

	return resp, nil
}

func (c *Client) Create[A, F any](ctx context.Context, request Request) (Result[A, F], error) {
	call := decisionCall{request: request}

	ctx = context.WithValue(ctx, callKey{}, &call)

	_, err := c.decisions.Create(ctx, operations.CreateDecisionRequest{Body: decisions.CreateDecisionRequest{
		Model: Model, Input: request.Input, Questions: request.Questions,
	}}, operations.WithRetries(retry.Config{Strategy: "none"}))

	if call.bodyErr != nil {
		return Result[A, F]{}, call.bodyErr
	}

	if _, sendFailed := errors.AsType[*url.Error](err); sendFailed || call.status == 0 {
		return Result[A, F]{}, fmt.Errorf("create decision: %w", SafeClientError(err))
	}

	if apiErr, ok := errors.AsType[*apierrors.APIError](err); ok && apiErr.StatusCode != http.StatusOK {
		return Result[A, F]{}, failure(apiErr.StatusCode, []byte(apiErr.Body))
	}

	if call.status != http.StatusOK {
		return Result[A, F]{}, failure(call.status, call.data)
	}

	var response Result[A, F]

	if err := jsonv2.Unmarshal(call.data, &response); err != nil {
		return Result[A, F]{}, ErrInvalidResponse
	}

	return response, nil
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
