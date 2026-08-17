package providerhttp

import (
	"bytes"
	"context"
	"encoding/json"
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
		return nil, collecterr.New(collecterr.Failed, collecterr.ClassProtocol, string(provider)+" response is nil")
	}
	defer func() {
		err = cleanupProviderResponse(ctx, resp.Body, policy.MaxDrainBytes, err)
	}()
	if ctx == nil {
		return nil, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "provider response context is nil")
	}
	if validationErr := policy.validate(); validationErr != nil {
		return nil, validationErr
	}
	if resp.StatusCode != policy.SuccessStatus {
		err = readProviderError(ctx, resp, policy, provider)
		return nil, err
	}
	if headerErr := validateSuccessHeaders(resp, policy, provider); headerErr != nil {
		return nil, headerErr
	}
	body, err = readProviderSuccess(ctx, resp.Body, policy, provider)
	if err != nil {
		return nil, err
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
	excerpt, readErr := readBounded(ctx, resp.Body, policy.MaxErrorBodyBytes)
	if readErr != nil {
		return collecterr.FromContext(fmt.Errorf("read %s error body: %w", provider, readErr))
	}
	return mapProviderStatus(provider, resp.StatusCode, resp.Header.Get("Retry-After"), string(excerpt))
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
	data, overflow, err := readAtMost(ctx, body, policy.MaxSuccessBodyBytes)
	if err != nil {
		return nil, collecterr.FromContext(fmt.Errorf("read %s: %w", provider, err))
	}
	if overflow {
		return nil, collecterr.New(collecterr.ResponseTooLarge, collecterr.ClassResourceLimit, string(provider)+" response exceeds body limit")
	}
	if !json.Valid(data) {
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

func readBounded(ctx context.Context, body io.Reader, maxBytes int64) ([]byte, error) {
	data, _, err := readAtMost(ctx, body, maxBytes)
	return data, err
}

func readAtMost(ctx context.Context, body io.Reader, maxBytes int64) (data []byte, overflow bool, err error) {
	if maxBytes < 0 {
		return nil, false, collecterr.New(collecterr.Failed, collecterr.ClassProtocol, "provider response body limit is invalid")
	}
	data, err = io.ReadAll(&ctxReader{ctx: ctx, r: io.LimitReader(body, maxBytes+1)})
	if err != nil {
		return nil, false, err
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
	_, err := io.Copy(io.Discard, &ctxReader{ctx: ctx, r: io.LimitReader(body, maxBytes)})
	return err
}

func joinResponseError(primary, cleanup error) error {
	if primary == nil {
		return cleanup
	}
	return errors.Join(primary, cleanup)
}

type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *ctxReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}
