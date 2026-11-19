package geminidecision

import (
	"bytes"
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/park285/shared-go/v2/pkg/httputil"
	"github.com/park285/shared-go/v2/pkg/netguard"
)

const Model = "gemini-decision-1"

const (
	apiHost          = "generativelanguage.googleapis.com"
	endpoint         = "https://" + apiHost + "/v1beta/decisions"
	requestTimeout   = 2 * time.Minute
	maxResponseBytes = 64 << 10
	maxMessageBytes  = 200
)

var (
	ErrUnavailable = errors.New("gemini decisions api is unavailable")
	ErrBlocked     = errors.New("gemini decisions input blocked")
	ErrRejected    = errors.New("gemini decisions request rejected")
)

var safetyBlockCodes = []string{"safety", "prohibited_content", "blocklist", "image_safety", "content_blocked"}

type Input struct {
	Type       string `json:"type"`
	Text       string `json:"text,omitempty"`
	MIMEType   string `json:"mime_type,omitempty"`
	Data       []byte `json:"data,omitempty"`
	Resolution string `json:"resolution,omitempty"`
}

type Question struct {
	Type         string `json:"type"`
	Instructions string `json:"instructions"`
	Criteria     any    `json:"criteria,omitempty"`
}

type Request struct {
	Input     []Input
	Questions map[string]Question
}

type Answer struct {
	Type          string             `json:"type"`
	Noul          *float64           `json:"noul"`
	Choice        *string            `json:"choice"`
	Confidence    *float64           `json:"confidence"`
	Probabilities map[string]float64 `json:"probabilities"`
}

type Response struct {
	Answers map[string]Answer `json:"answers"`
	Usage   struct {
		TotalInputTokens int32 `json:"total_input_tokens"`
	} `json:"usage"`
}

type wireRequest struct {
	Model     string              `json:"model"`
	Input     []Input             `json:"input"`
	Questions map[string]Question `json:"questions"`
}

type wireResponse struct {
	Response

	PromptFeedback struct {
		BlockReason string `json:"block_reason"`
	} `json:"prompt_feedback"`
}

type wireError struct {
	Error struct {
		Code    any    `json:"code"`
		Status  string `json:"status"`
		Message string `json:"message"`
		Details []struct {
			Reason string `json:"reason"`
		} `json:"details"`
	} `json:"error"`
}

type Client struct {
	http     *http.Client
	endpoint string
	key      string
}

func New(key string) *Client {
	policy := netguard.Policy{
		Timeout:      10 * time.Second,
		AllowedHosts: []string{apiHost},
		AllowedPorts: []string{"443"},
		Schemes:      []string{"https"},
	}

	client := netguard.GuardedClient(&http.Client{Timeout: requestTimeout}, policy)

	client.CheckRedirect = netguard.RedirectPolicy(netguard.RedirectConfig{Policy: policy, DisableFollow: true})

	return &Client{http: client, endpoint: endpoint, key: key}
}

func (c *Client) Decide(ctx context.Context, request Request) (Response, error) {
	body, err := jsonv2.Marshal(wireRequest{Model: Model, Input: request.Input, Questions: request.Questions}, jsonv2.Deterministic(true))
	if err != nil {
		return Response{}, fmt.Errorf("encode decision request: %w", err)
	}

	status, raw, err := c.post(ctx, body)
	if err != nil {
		return Response{}, err
	}

	if status != http.StatusOK {
		return Response{}, failure(status, raw)
	}

	var decoded wireResponse

	if err := jsonv2.Unmarshal(raw, &decoded); err != nil {
		return Response{}, fmt.Errorf("decode decision response: %w", err)
	}

	if reason := decoded.PromptFeedback.BlockReason; reason != "" {
		return Response{}, fmt.Errorf("%w: prompt feedback %s", ErrBlocked, clip(reason))
	}

	return decoded.Response, nil
}

func (c *Client) post(ctx context.Context, body []byte) (int, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, nil, fmt.Errorf("create decision request: %w", err)
	}

	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("x-goog-api-key", c.key)

	response, err := c.http.Do(request)
	if err != nil {
		err = fmt.Errorf("send decision request: %w", err)

		if ctx.Err() != nil {
			return 0, nil, err
		}

		return 0, nil, unreachable(err)
	}

	defer response.Body.Close()

	raw, err := httputil.ReadAllLimited(response.Body, maxResponseBytes)
	if err != nil {
		return 0, nil, fmt.Errorf("read decision response: %w", err)
	}

	return response.StatusCode, raw, nil
}

func failure(status int, raw []byte) error {
	var decoded wireError

	if err := jsonv2.Unmarshal(raw, &decoded); err != nil {
		decoded = wireError{}
	}

	var code string

	if value, isString := decoded.Error.Code.(string); isString {
		code = value
	}

	reasons := make([]string, 0, len(decoded.Error.Details))

	for _, detail := range decoded.Error.Details {
		reasons = append(reasons, detail.Reason)
	}

	err := fmt.Errorf("decision status %d %s %s: %s", status, clip(decoded.Error.Status), clip(code), clip(decoded.Error.Message))

	switch {
	case status == http.StatusBadRequest && slices.Contains(safetyBlockCodes, code):
		return fmt.Errorf("%w: %w", ErrBlocked, err)
	case status == http.StatusUnauthorized, status == http.StatusForbidden, status == http.StatusNotFound,
		status == http.StatusTooManyRequests, status == http.StatusServiceUnavailable,
		status == http.StatusBadRequest && (decoded.Error.Status == "FAILED_PRECONDITION" || slices.Contains(reasons, "API_KEY_INVALID")):
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	case status == http.StatusBadRequest:
		return fmt.Errorf("%w: %w", ErrRejected, err)
	default:
		return err
	}
}

func unreachable(err error) error {
	if _, ok := errors.AsType[*url.Error](err); ok {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}

	return err
}

func clip(text string) string {
	if len(text) <= maxMessageBytes {
		return text
	}

	return strings.ToValidUTF8(text[:maxMessageBytes], "")
}
