// Package geminidecision은 Gemini Developer API의 Decisions API(gemini-decision-1) 호출과 API 키 파일 적재를 맡는다.
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

// Model은 Decisions API가 받는 유일한 모델이다.
const Model = "gemini-decision-1"

const (
	apiHost          = "generativelanguage.googleapis.com"
	endpoint         = "https://" + apiHost + "/v1beta/decisions"
	requestTimeout   = 2 * time.Minute
	maxResponseBytes = 64 << 10
	maxMessageBytes  = 200
)

var (
	// ErrUnavailable은 어느 입력도 판정할 수 없는 실패다(연결 실패, 키·권한·모델·할당량·지역 오류).
	ErrUnavailable = errors.New("gemini decisions api is unavailable")
	// ErrBlocked는 입력이 안전 필터에 막혔다는 뜻이다. 같은 입력은 다시 보내도 막힌다.
	ErrBlocked = errors.New("gemini decisions input blocked")
	// ErrRejected는 안전 차단이 아닌 400이다. 응답만으로는 입력 탓인지 요청 꼴 탓인지 가릴 수 없다.
	ErrRejected = errors.New("gemini decisions request rejected")
)

var safetyBlockCodes = []string{"safety", "prohibited_content", "blocklist", "image_safety", "content_blocked"}

// Input은 공유 입력의 content block 하나다. Data는 JSON에서 base64가 된다.
type Input struct {
	Type       string `json:"type"`
	Text       string `json:"text,omitempty"`
	MIMEType   string `json:"mime_type,omitempty"`
	Data       []byte `json:"data,omitempty"`
	Resolution string `json:"resolution,omitempty"`
}

// Question의 Criteria는 질문 유형(noul·choice·score)이 정한 꼴을 그대로 직렬화한다.
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

// wireError의 code는 Decisions API 자체의 400에서는 문자열이고 게이트웨이 오류에서는 정수다.
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

// Decide는 질문들을 한 요청으로 묻는다. 실패는 ErrUnavailable, ErrBlocked, ErrRejected 가운데 하나로 감싸고,
// 그 밖의 실패(5xx, 응답 대기 시간 초과, 해석할 수 없는 응답)는 결과 불명이라 감싸지 않는다.
func (c *Client) Decide(ctx context.Context, request Request) (Response, error) {
	// map인 질문과 선택지를 정렬해 쓴다. 기본 직렬화는 요청마다 선택지 순서를 바꿔 같은 입력의 답이 흔들린다.
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
		// 이름 확인·연결은 netguard의 자체 시간 제한으로 이 기한보다 먼저 끝나므로, 기한이 지나 끝난 실패는
		// 연결 뒤 응답을 기다리다 난 것이다. 결과를 알 수 없어 사용 불가로 감싸지 않는다.
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

// 잘못된 키(ErrorInfo reason API_KEY_INVALID)와 지원하지 않는 지역(FAILED_PRECONDITION)도 400으로 오므로 입력 탓 400보다 먼저 가른다.
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

// unreachable은 요청 기한 안에 끝난 전송 실패(이름 확인·연결 실패와 그 시간 초과)를 ErrUnavailable로 감싼다.
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
