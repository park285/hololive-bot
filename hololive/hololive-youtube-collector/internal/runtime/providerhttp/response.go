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
		return nil, collecterr.New(contract.ErrorCollectionFailed, contract.ClassProtocol, string(provider)+" response is nil")
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
		return collecterr.New(contract.ErrorConfiguration, contract.ClassConfiguration, "provider success status is invalid")
	}

	if len(policy.SuccessContentTypes) == 0 {
		return collecterr.New(contract.ErrorConfiguration, contract.ClassConfiguration, "provider success content types are required")
	}

	if policy.MaxSuccessBodyBytes < 0 || policy.MaxErrorBodyBytes < 0 || policy.MaxDrainBytes < 0 {
		return collecterr.New(contract.ErrorConfiguration, contract.ClassConfiguration, "provider body limits are invalid")
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
		return collecterr.New(contract.ErrorCollectionFailed, contract.ClassProtocol, string(provider)+" content encoding is unsupported")
	}

	if !allowedSuccessContentType(resp.Header.Get("Content-Type"), policy.SuccessContentTypes) {
		return collecterr.New(contract.ErrorCollectionFailed, contract.ClassProtocol, string(provider)+" content type is not JSON")
	}

	return nil
}

func readProviderSuccess(ctx context.Context, body io.Reader, policy ProviderResponsePolicy, provider contract.Provider) ([]byte, error) {
	data, err := sharedhttputil.ReadAllLimited(&ctxReader{ctx: ctx, r: body}, policy.MaxSuccessBodyBytes)
	if errors.Is(err, sharedhttputil.ErrResponseBodyTooLarge) {
		return nil, collecterr.New(contract.ErrorResponseTooLarge, contract.ClassResourceLimit, string(provider)+" response exceeds body limit")
	}

	if err != nil {
		return nil, collecterr.FromContext(fmt.Errorf("read %s: %w", provider, err))
	}

	if !jsontext.Value(data).IsValid() {
		return nil, collecterr.New(contract.ErrorCollectionFailed, contract.ClassProtocol, string(provider)+" response is not a single JSON document")
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
		err = r.ctx.Err()
	}

	return n, err
}
