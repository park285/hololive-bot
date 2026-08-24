package providerhttp

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
)

type ProviderResponsePolicy struct {
	SuccessStatus       int
	SuccessContentTypes []string
	MaxSuccessBodyBytes int64
	MaxErrorBodyBytes   int64
	MaxDrainBytes       int64
}

func DefaultJSONPolicy(maxSuccessBodyBytes int64) ProviderResponsePolicy {
	if maxSuccessBodyBytes <= 0 {
		maxSuccessBodyBytes = 1 << 20
	}

	return ProviderResponsePolicy{
		SuccessStatus:       http.StatusOK,
		SuccessContentTypes: []string{"application/json"},
		MaxSuccessBodyBytes: maxSuccessBodyBytes,
		MaxErrorBodyBytes:   collecterr.MaxDetailBytes,
		MaxDrainBytes:       64 << 10,
	}
}

func ReadProviderJSONDocument(
	ctx context.Context,
	resp *http.Response,
	policy ProviderResponsePolicy,
	provider contract.Provider,
) (body []byte, err error) {
	if resp == nil || resp.Body == nil {
		//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
		return nil, collecterr.New(collecterr.Failed, collecterr.ClassProtocol, string(provider)+" response is nil")
	}

	defer func() {
		err = cleanupProviderResponse(ctx, resp.Body, policy.MaxDrainBytes, err)
	}()

	if ctx == nil {
		//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
		return nil, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "provider response context is nil")
	}

	if validationErr := policy.validate(); validationErr != nil {
		return nil, fmt.Errorf("validate: %w", validationErr)
	}

	if resp.StatusCode != policy.SuccessStatus {
		//nolint:wrapcheck // 분류가 끝난 provider 오류를 그대로 돌려줘야 DiagnosticOf가 기록하는 detail이 유지된다.
		return nil, readProviderError(ctx, resp, policy, provider)
	}

	if headerErr := validateSuccessHeaders(resp, policy, provider); headerErr != nil {
		return nil, fmt.Errorf("validate success headers: %w", headerErr)
	}

	body, err = readProviderSuccess(ctx, resp.Body, policy, provider)
	if err != nil {
		return nil, fmt.Errorf("read provider success: %w", err)
	}

	return bytes.Clone(body), nil
}

func cleanupProviderResponse(ctx context.Context, body io.ReadCloser, maxDrainBytes int64, primary error) error {
	if ctx != nil && ctx.Err() == nil && maxDrainBytes > 0 {
		drainErr := drainBounded(ctx, body, maxDrainBytes)
		if drainErr != nil {
			primary = joinResponseError(primary, collecterr.FromContext(fmt.Errorf("drain provider response: %w", drainErr)))
		}
	}

	closeErr := body.Close()
	if closeErr != nil {
		primary = joinResponseError(primary, collecterr.FromContext(fmt.Errorf("close provider response: %w", closeErr)))
	}

	//nolint:wrapcheck // 호출자가 만든 오류를 지나보내는 자리라 여기서 감싸면 접두어가 이중으로 붙는다.
	return primary
}

func (policy ProviderResponsePolicy) validate() error {
	if policy.SuccessStatus <= 0 {
		//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
		return collecterr.New(collecterr.Configuration, collecterr.ClassConfiguration, "provider success status is invalid")
	}

	if len(policy.SuccessContentTypes) == 0 {
		//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
		return collecterr.New(collecterr.Configuration, collecterr.ClassConfiguration, "provider success content types are required")
	}

	if policy.MaxSuccessBodyBytes < 0 || policy.MaxErrorBodyBytes < 0 || policy.MaxDrainBytes < 0 {
		//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
		return collecterr.New(collecterr.Configuration, collecterr.ClassConfiguration, "provider body limits are invalid")
	}

	return nil
}

func readProviderError(ctx context.Context, resp *http.Response, policy ProviderResponsePolicy, provider contract.Provider) error {
	excerpt, _, readErr := readAtMost(ctx, resp.Body, policy.MaxErrorBodyBytes)
	if readErr != nil {
		if err := collecterr.FromContext(fmt.Errorf("read %s error body: %w", provider, readErr)); err != nil {
			return fmt.Errorf("from context: %w", err)
		}

		return nil
	}

	if err := mapProviderStatus(provider, resp.StatusCode, resp.Header.Get("Retry-After"), string(excerpt)); err != nil {
		return fmt.Errorf("map provider status: %w", err)
	}

	return nil
}

func validateSuccessHeaders(resp *http.Response, policy ProviderResponsePolicy, provider contract.Provider) error {
	if remainingContentEncoding(resp) != "" {
		//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
		return collecterr.New(collecterr.Failed, collecterr.ClassProtocol, string(provider)+" content encoding is unsupported")
	}

	if !allowedSuccessContentType(resp.Header.Get("Content-Type"), policy.SuccessContentTypes) {
		//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
		return collecterr.New(collecterr.Failed, collecterr.ClassProtocol, string(provider)+" content type is not JSON")
	}

	return nil
}

func readProviderSuccess(ctx context.Context, body io.Reader, policy ProviderResponsePolicy, provider contract.Provider) ([]byte, error) {
	data, overflow, err := readAtMost(ctx, body, policy.MaxSuccessBodyBytes)
	if err != nil {
		if fromErr := collecterr.FromContext(fmt.Errorf("read %s: %w", provider, err)); fromErr != nil {
			return nil, fmt.Errorf("from context: %w", fromErr)
		}

		return nil, nil
	}

	if overflow {
		//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
		return nil, collecterr.New(collecterr.ResponseTooLarge, collecterr.ClassResourceLimit, string(provider)+" response exceeds body limit")
	}

	if !jsontext.Value(data).IsValid() {
		//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
		return nil, collecterr.New(collecterr.Failed, collecterr.ClassProtocol, string(provider)+" response is not a single JSON document")
	}

	return data, nil
}

func remainingContentEncoding(resp *http.Response) string {
	if resp.Uncompressed {
		return ""
	}

	encoding := strings.TrimSpace(resp.Header.Get("Content-Encoding"))
	if encoding == "" || strings.EqualFold(encoding, "identity") {
		return ""
	}

	return encoding
}

func allowedSuccessContentType(header string, allowed []string) bool {
	mediaType, _, err := mime.ParseMediaType(header)
	if err != nil {
		return false
	}

	for _, candidate := range allowed {
		if strings.EqualFold(mediaType, candidate) {
			return true
		}
	}

	return false
}

func readAtMost(ctx context.Context, body io.Reader, maxBytes int64) (data []byte, overflow bool, err error) {
	if maxBytes < 0 {
		//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
		return nil, false, collecterr.New(collecterr.Failed, collecterr.ClassProtocol, "provider response body limit is invalid")
	}

	data, err = io.ReadAll(&ctxReader{ctx: ctx, r: io.LimitReader(body, maxBytes+1)})
	if err != nil {
		return nil, false, fmt.Errorf("read all: %w", err)
	}

	if int64(len(data)) > maxBytes {
		return data[:maxBytes], true, nil
	}

	return data, false, nil
}

func drainBounded(ctx context.Context, body io.Reader, maxBytes int64) error {
	if maxBytes <= 0 {
		return nil
	}

	if _, err := io.Copy(io.Discard, &ctxReader{ctx: ctx, r: io.LimitReader(body, maxBytes)}); err != nil {
		return fmt.Errorf("copy: %w", err)
	}

	return nil
}

func joinResponseError(primary, cleanup error) error {
	if primary == nil {
		return cleanup
	}

	return errors.Join(primary, cleanup)
}

type ctxReader struct {
	//nolint:containedctx // io.Reader에는 ctx 매개변수가 없어 취소 확인용 ctx를 필드로 들고 갈 수밖에 없다.
	ctx context.Context
	r   io.Reader
}

func (r *ctxReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		//nolint:wrapcheck // io.Reader 소비자가 취소 센티널을 등가 비교할 수 있으므로 그대로 돌려준다.
		return 0, err
	}

	// io.ReadAll과 io.Copy는 종료 조건을 EOF 등가 비교로 판별하므로, 여기서 %w로 감싸면
	// 정상 종료가 실패로 뒤바뀌어 모든 응답 읽기가 실패한다.
	//nolint:wrapcheck // 바로 위 주석대로 EOF 센티널을 그대로 돌려줘야 한다.
	return r.r.Read(p)
}
