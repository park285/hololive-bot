package youtubejs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"

	"github.com/kapu/hololive-shared/pkg/httpbody"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
)

const helperControlBodyLimit int64 = 8 << 10

func (h *Helper) Healthy(ctx context.Context) error {
	if h == nil || h.health == nil {
		return collecterr.New(collecterr.Configuration, collecterr.ClassConfiguration, "youtube.js helper is not configured")
	}
	if h.Exited() {
		return collecterr.New(collecterr.Failed, collecterr.ClassTransient, "youtube.js helper exited")
	}
	healthCtx, cancel := withOptionalTimeout(ctx, h.healthTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(healthCtx, http.MethodGet, h.endpoint+"/health", http.NoBody)
	if err != nil {
		return fmt.Errorf("youtube.js helper health request: %w", err)
	}
	status, payload, err := doControlRequest(h.health, req, "youtube.js helper health")
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return collecterr.New(collecterr.HelperProtocolMismatch, collecterr.ClassProtocol, "youtube.js helper is not ready")
	}
	var body HealthResponse
	if err := strictDecode(payload, &body); err != nil {
		return collecterr.Wrap(collecterr.HelperProtocolMismatch, collecterr.ClassProtocol, fmt.Errorf("decode youtube.js helper health: %w", err))
	}
	return validateHealthResponse(body, h.maxInflight)
}

func (h *Helper) bootstrapReady(ctx context.Context, cfg *Config) error {
	status, body, raw, err := h.postBootstrap(ctx, BootstrapRequest{
		ProtocolVersion: ProtocolVersion,
		Proxy: BootstrapProxy{
			Enabled: cfg.Proxy.Enabled,
			URL:     cfg.Proxy.URL,
		},
		Limits: BootstrapLimits{
			RequestBodyBytes:  cfg.RequestBodyLimit,
			ResponseBodyBytes: cfg.ResponseBodyLimit,
			MaxInflight:       cfg.MaxInflight,
		},
	})
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return bootstrapStatusError(status, raw)
	}
	if err := validateBootstrapResponse(body, cfg); err != nil {
		return err
	}
	h.bootstrap = body
	h.maxInflight = body.MaxInflight
	return nil
}

func (h *Helper) postBootstrap(ctx context.Context, request BootstrapRequest) (
	status int,
	body BootstrapResponse,
	raw []byte,
	resultErr error,
) {
	if h == nil || h.rpc == nil || h.rpc.http == nil {
		return 0, BootstrapResponse{}, nil, collecterr.New(collecterr.Configuration, collecterr.ClassConfiguration, "youtube.js helper is not configured")
	}
	req, err := h.newBootstrapRequest(ctx, request)
	if err != nil {
		return 0, BootstrapResponse{}, nil, err
	}
	status, raw, err = doControlRequest(h.rpc.http, req, "youtube.js helper bootstrap")
	if err != nil {
		return status, BootstrapResponse{}, nil, err
	}
	body, err = decodeBootstrapResponse(status, raw)
	if err != nil {
		return status, BootstrapResponse{}, raw, err
	}
	return status, body, raw, nil
}

func doControlRequest(client *http.Client, req *http.Request, action string) (
	status int,
	payload []byte,
	resultErr error,
) {
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, errors.Join(collecterr.FromContext(fmt.Errorf("%s: %w", action, err)), closeHTTPResponse(resp))
	}
	if resp == nil || resp.Body == nil {
		return 0, nil, collecterr.New(collecterr.Failed, collecterr.ClassProtocol, action+" response is nil")
	}
	payload, err = readControlBody(resp)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, payload, nil
}

func (h *Helper) newBootstrapRequest(ctx context.Context, request BootstrapRequest) (*http.Request, error) {
	payload, err := json.Marshal(request)
	if err != nil {
		return nil, collecterr.Wrap(collecterr.Failed, collecterr.ClassProtocol, fmt.Errorf("marshal youtube.js helper bootstrap: %w", err))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.endpoint+"/v1/bootstrap", bytes.NewReader(payload))
	if err != nil {
		return nil, collecterr.Wrap(collecterr.Failed, collecterr.ClassProtocol, fmt.Errorf("build youtube.js helper bootstrap: %w", err))
	}
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

func decodeBootstrapResponse(status int, raw []byte) (BootstrapResponse, error) {
	var body BootstrapResponse
	if status != http.StatusOK {
		return body, nil
	}
	if err := strictDecode(raw, &body); err != nil {
		return BootstrapResponse{}, collecterr.Wrap(collecterr.HelperProtocolMismatch, collecterr.ClassProtocol, fmt.Errorf("decode youtube.js helper bootstrap: %w", err))
	}
	return body, nil
}

func readControlBody(resp *http.Response) ([]byte, error) {
	if resp == nil || resp.Body == nil {
		return nil, collecterr.New(collecterr.Failed, collecterr.ClassProtocol, "youtube.js helper control response is nil")
	}
	if !jsonContentType(resp.Header.Get("Content-Type")) {
		closeErr := closeHTTPResponse(resp)
		return nil, errors.Join(collecterr.New(collecterr.HelperProtocolMismatch, collecterr.ClassProtocol, "youtube.js helper control content type is invalid"), closeErr)
	}
	payload, readErr := httpbody.ReadAllAndDrain(resp.Body, helperControlBodyLimit)
	closeErr := resp.Body.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		if errors.Is(err, httpbody.ErrTooLarge) {
			return nil, errors.Join(collecterr.New(collecterr.ResponseTooLarge, collecterr.ClassResourceLimit, "youtube.js helper control response exceeds body limit"), err)
		}
		return nil, collecterr.FromContext(fmt.Errorf("read youtube.js helper control: %w", err))
	}
	return payload, nil
}

func bootstrapStatusError(status int, raw []byte) error {
	err := helperStatusError(status, raw)
	if collecterr.CodeOf(err) == collecterr.HelperProtocolMismatch {
		return err
	}
	return collecterr.Wrap(collecterr.HelperProtocolMismatch, collecterr.ClassProtocol, fmt.Errorf("youtube.js helper bootstrap status %d: %w", status, err))
}

func jsonContentType(value string) bool {
	media, _, err := mime.ParseMediaType(value)
	return err == nil && media == "application/json"
}

func validateHealthResponse(body HealthResponse, maxInflight int) error {
	if body.ProtocolVersion != ProtocolVersion || body.State != StateReady {
		return collecterr.New(collecterr.HelperProtocolMismatch, collecterr.ClassProtocol, "youtube.js helper health state is invalid")
	}
	if body.MaxInflight < 1 || body.Inflight < 0 || body.Inflight > body.MaxInflight {
		return collecterr.New(collecterr.HelperProtocolMismatch, collecterr.ClassProtocol, "youtube.js helper inflight bounds are invalid")
	}
	if maxInflight > 0 && body.MaxInflight != maxInflight {
		return collecterr.New(collecterr.HelperProtocolMismatch, collecterr.ClassProtocol, "youtube.js helper inflight limit mismatch")
	}
	return nil
}

func validateBootstrapResponse(body BootstrapResponse, cfg *Config) error {
	if body.ProtocolVersion != ProtocolVersion || body.State != StateReady {
		return collecterr.New(collecterr.HelperProtocolMismatch, collecterr.ClassProtocol, "youtube.js helper bootstrap state is invalid")
	}
	if body.RequestBodyBytes != cfg.RequestBodyLimit || body.ResponseBodyBytes != cfg.ResponseBodyLimit || body.MaxInflight != cfg.MaxInflight {
		return collecterr.New(collecterr.HelperProtocolMismatch, collecterr.ClassProtocol, "youtube.js helper bootstrap limits mismatch")
	}
	if body.ProxyEnabled != cfg.Proxy.Enabled {
		return collecterr.New(collecterr.HelperProtocolMismatch, collecterr.ClassProtocol, "youtube.js helper bootstrap proxy mismatch")
	}
	return nil
}
