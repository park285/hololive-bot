package alarm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/park285/shared-go/v2/pkg/httputil"

	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"
	"github.com/kapu/hololive-shared/pkg/domain"
)

// UpdateAlarmAdvanceMinutes는 전송 뒤 오류를 적용 거부로 추정하지 않고 원래 오류와 결과불명을 반환한다.
func (c *Client) UpdateAlarmAdvanceMinutes(ctx context.Context, minutes int) (domain.AdvanceMinutesResult, error) {
	// 직렬화 대기와 전송은 하나의 호출 예산을 사용한다. HTTP client timeout만으로는 대기를 제한할 수 없다.
	ctx, cancel := advanceOperationContext(ctx, c.httpClient.Timeout)
	defer cancel()

	result := domain.AdvanceMinutesResult{RequestedMinutes: minutes, Outcome: domain.ApplyRejected}

	req, err := c.newAdvanceRequest(ctx, minutes)
	if err != nil {
		return result, err
	}

	// 동시 PUT의 늦은 성공 응답이 이후 결과불명의 cache를 과거 값으로 덮지 않도록 순서를 보존한다.
	select {
	case c.advanceRequests <- struct{}{}:
		defer func() { <-c.advanceRequests }()
	case <-ctx.Done():
		return result, fmt.Errorf("update alarm advance minutes: wait to send: %w", ctx.Err())
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		return result, fmt.Errorf("update alarm advance minutes: before send: %w", ctxErr)
	}

	// Do 호출 이후에는 전송하지 않았다는 증거가 없으므로 취소·EOF도 결과불명이다.
	result.Outcome = domain.ApplyUnknown

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.closeResponseBody(resp, contractsalarm.SettingsPath)
		c.setTargetMinutes(nil)

		return result, fmt.Errorf("update alarm advance minutes: send request: %w", err)
	}
	defer c.closeResponseBody(resp, contractsalarm.SettingsPath)

	if responseErr := c.validateResponse(contractsalarm.SettingsPath, resp); responseErr != nil {
		if confirmedAdvanceRejection(responseErr) {
			result.Outcome = domain.ApplyRejected
		} else {
			c.setTargetMinutes(nil)
		}

		return result, fmt.Errorf("update alarm advance minutes: validate response: %w", responseErr)
	}

	data, err := decodeAPIEnvelope[minutesResp](contractsalarm.SettingsPath, resp.Body)
	if err != nil {
		c.setTargetMinutes(nil)

		return result, fmt.Errorf("update alarm advance minutes: decode response: %w", err)
	}

	if !validAdvanceTargets(data.TargetMinutes) {
		c.setTargetMinutes(nil)

		return result, errors.New("update alarm advance minutes: response has no valid target minutes")
	}

	result.Outcome = domain.ApplyConfirmed
	result.TargetMinutes = slices.Clone(data.TargetMinutes)
	c.setTargetMinutes(data.TargetMinutes)

	return result, nil
}

func (c *Client) newAdvanceRequest(ctx context.Context, minutes int) (*http.Request, error) {
	if err := validateAlarmServiceOrigin(c.baseURL); err != nil {
		return nil, fmt.Errorf("update alarm advance minutes: validate origin: %w", err)
	}

	body, err := encodeJSONRequestBody(contractsalarm.SettingsPath, updateAdvanceMinutesReq{Minutes: minutes})
	if err != nil {
		return nil, fmt.Errorf("update alarm advance minutes: encode request: %w", err)
	}

	req, err := c.newRequest(ctx, http.MethodPut, contractsalarm.SettingsPath, body, true)
	if err != nil {
		return nil, fmt.Errorf("update alarm advance minutes: request: %w", err)
	}

	return req, nil
}

func advanceOperationContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout > 0 {
		return context.WithTimeout(ctx, timeout)
	}

	return context.WithCancel(ctx)
}

// 상태 코드만으로 proxy 오류를 worker 거부로 간주하지 않는다. Worker의 인증·입력 오류 코드도 확인한다.
func confirmedAdvanceRejection(err error) bool {
	apiErr, ok := errors.AsType[*httputil.APIError](err)
	if !ok || apiErr.Err != nil {
		return false
	}

	switch apiErr.StatusCode {
	case http.StatusBadRequest:
		return apiErr.Code == "invalid_request_body"
	case http.StatusUnauthorized:
		return apiErr.Code == "unauthorized"
	case http.StatusForbidden:
		return apiErr.Code == "forbidden"
	default:
		return false
	}
}

func validAdvanceTargets(targets []int) bool {
	if len(targets) == 0 {
		return false
	}

	for _, minute := range targets {
		if minute <= 0 {
			return false
		}
	}

	return true
}

func (c *Client) setTargetMinutes(targets []int) {
	c.targetMinutesMu.Lock()
	defer c.targetMinutesMu.Unlock()

	c.targetMinutes = slices.Clone(targets)
}
