package providerhttp

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"strings"

	sharedhttputil "github.com/park285/shared-go/v2/pkg/httputil"

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

// ReadProviderJSONDocument는 상태와 본문 계약을 검사하고 호출자가 소유하는 JSON 바이트를 반환합니다.
// 성공 여부와 관계없이 본문을 닫으며, 취소되지 않은 응답의 나머지는 MaxDrainBytes까지만 버립니다.
func ReadProviderJSONDocument(
	ctx context.Context,
	resp *http.Response,
	policy ProviderResponsePolicy,
	provider contract.Provider,
) (body []byte, err error) {
	if resp == nil || resp.Body == nil {
		return nil, collecterr.New(collecterr.Failed, collecterr.ClassProtocol, string(provider)+" response is nil")
	}

	defer func() {
		err = cleanupProviderResponse(ctx, resp.Body, policy.MaxDrainBytes, err)
	}()

	if validationErr := policy.validate(); validationErr != nil {
		return nil, fmt.Errorf("validate: %w", validationErr)
	}

	if resp.StatusCode != policy.SuccessStatus {
		return nil, readProviderError(ctx, resp, policy, provider)
	}

	if headerErr := validateSuccessHeaders(resp, policy, provider); headerErr != nil {
		return nil, fmt.Errorf("validate success headers: %w", headerErr)
	}

	body, err = readProviderSuccess(ctx, resp.Body, policy, provider)
	if err != nil {
		return nil, fmt.Errorf("read provider success: %w", err)
	}

	return body, nil
}

func cleanupProviderResponse(ctx context.Context, body io.ReadCloser, maxDrainBytes int64, primary error) error {
	if ctx.Err() == nil && maxDrainBytes > 0 {
		drainErr := drainBounded(ctx, body, maxDrainBytes)
		if drainErr != nil {
			primary = joinResponseError(primary, collecterr.FromContext(fmt.Errorf("drain provider response: %w", drainErr)))
		}
	}

	closeErr := body.Close()
	if closeErr != nil {
		primary = joinResponseError(primary, collecterr.FromContext(fmt.Errorf("close provider response: %w", closeErr)))
	}

	return primary
}

func (policy ProviderResponsePolicy) validate() error {
	if policy.SuccessStatus <= 0 {
		return collecterr.New(collecterr.Configuration, collecterr.ClassConfiguration, "provider success status is invalid")
	}

	if len(policy.SuccessContentTypes) == 0 {
		return collecterr.New(collecterr.Configuration, collecterr.ClassConfiguration, "provider success content types are required")
	}

	if policy.MaxSuccessBodyBytes < 0 || policy.MaxErrorBodyBytes < 0 || policy.MaxDrainBytes < 0 {
		return collecterr.New(collecterr.Configuration, collecterr.ClassConfiguration, "provider body limits are invalid")
	}

	return nil
}

func readProviderError(ctx context.Context, resp *http.Response, policy ProviderResponsePolicy, provider contract.Provider) error {
	excerpt, readErr := readErrorExcerpt(ctx, resp.Body, policy.MaxErrorBodyBytes)
	if readErr != nil {
		return collecterr.FromContext(fmt.Errorf("read %s error body: %w", provider, readErr))
	}

	if err := mapProviderStatus(provider, resp.StatusCode, resp.Header.Get("Retry-After"), string(excerpt)); err != nil {
		return fmt.Errorf("map provider status: %w", err)
	}

	return nil
}

func validateSuccessHeaders(resp *http.Response, policy ProviderResponsePolicy, provider contract.Provider) error {
	if remainingContentEncoding(resp) != "" {
		return collecterr.New(collecterr.Failed, collecterr.ClassProtocol, string(provider)+" content encoding is unsupported")
	}

	if !allowedSuccessContentType(resp.Header.Get("Content-Type"), policy.SuccessContentTypes) {
		return collecterr.New(collecterr.Failed, collecterr.ClassProtocol, string(provider)+" content type is not JSON")
	}

	return nil
}

func readProviderSuccess(ctx context.Context, body io.Reader, policy ProviderResponsePolicy, provider contract.Provider) ([]byte, error) {
	data, err := sharedhttputil.ReadAllLimited(&ctxReader{ctx: ctx, r: body}, policy.MaxSuccessBodyBytes)
	if errors.Is(err, sharedhttputil.ErrResponseBodyTooLarge) {
		return nil, collecterr.New(collecterr.ResponseTooLarge, collecterr.ClassResourceLimit, string(provider)+" response exceeds body limit")
	}

	if err != nil {
		return nil, collecterr.FromContext(fmt.Errorf("read %s: %w", provider, err))
	}

	if !jsontext.Value(data).IsValid() {
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

func readErrorExcerpt(ctx context.Context, body io.Reader, maxBytes int64) ([]byte, error) {
	readLimit := maxBytes
	if readLimit < math.MaxInt64 {
		readLimit++
	}

	data, err := io.ReadAll(&ctxReader{ctx: ctx, r: io.LimitReader(body, readLimit)})
	if err != nil {
		return nil, fmt.Errorf("read error excerpt: %w", err)
	}

	if int64(len(data)) > maxBytes {
		return data[:maxBytes], nil
	}

	return data, nil
}

func drainBounded(ctx context.Context, body io.Reader, maxBytes int64) error {
	if maxBytes <= 0 {
		return nil
	}

	// 공유 drain의 EOF 확인용 추가 읽기도 기존 상한에 포함한다. 실제 close는 호출부가
	// 따로 수행해 drain 오류와 close 오류 각각의 분류 및 원인을 보존한다.
	bounded := io.NopCloser(&ctxReader{ctx: ctx, r: io.LimitReader(body, maxBytes)})
	if err := sharedhttputil.DrainAndClose(bounded, maxBytes); err != nil {
		return fmt.Errorf("drain bounded response: %w", err)
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
		return 0, err
	}

	n, err := r.r.Read(p)
	if err == nil {
		// 바깥 LimitReader가 다음 Read를 생략해도 마지막 읽기 중 발생한 취소를 보존한다.
		err = r.ctx.Err()
	}

	// io.ReadAll과 io.Copy가 EOF를 직접 비교하므로 읽기 오류는 감싸지 않는다.
	return n, err
}
