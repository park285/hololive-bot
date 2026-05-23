// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package chzzk

import (
	"context"
	"fmt"
	"net/http"

	json "github.com/park285/hololive-bot/shared-go/pkg/json"

	apperrors "github.com/kapu/hololive-shared/pkg/apperrors"
)

func (c *Client) GetLiveStatus(ctx context.Context, channelID string) (*LiveStatusContent, error) {
	if err := c.rejectIfCircuitOpen(); err != nil {
		return nil, err
	}

	escapedChannelID, err := escapedChannelPath(channelID)
	if err != nil {
		return nil, err
	}

	path := fmt.Sprintf("/polling/v2/channels/%s/live-status", escapedChannelID)
	reqURL := c.baseURL + path

	req, err := c.newRequest(ctx, "GET", reqURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	var liveStatusResp LiveStatusResponse
	if err := c.executeRequest(chzzkGetLiveStatusOp, req, "failed to read response body", func(body []byte) error {
		if err := json.Unmarshal(body, &liveStatusResp); err != nil {
			return fmt.Errorf("failed to unmarshal response: %w", err)
		}

		return nil
	}); err != nil {
		return nil, err
	}

	if liveStatusResp.Code != http.StatusOK {
		c.handleStatusCodeError(liveStatusResp.Code)

		return nil, &apperrors.APIError{
			Operation:  chzzkGetLiveStatusOp,
			StatusCode: liveStatusResp.Code,
			Err: fmt.Errorf(
				"chzzk api code=%d message=%s",
				liveStatusResp.Code,
				liveStatusResp.Message,
			),
		}
	}

	if liveStatusResp.Content == nil {
		return nil, fmt.Errorf("chzzk live status content is nil")
	}

	return liveStatusResp.Content, nil
}

func (c *Client) GetScheduledLives(ctx context.Context, channelID string) ([]ScheduledLive, error) {
	if err := c.rejectIfCircuitOpen(); err != nil {
		return nil, err
	}

	escapedChannelID, err := escapedChannelPath(channelID)
	if err != nil {
		return nil, err
	}

	path := fmt.Sprintf("/service/v1/channels/%s/scheduled-lives", escapedChannelID)
	reqURL := c.baseURL + path

	req, err := c.newRequest(ctx, "GET", reqURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	var scheduledResp ScheduledLivesResponse
	if err := c.executeRequest(chzzkGetScheduledLivesOp, req, "failed to read response body", func(body []byte) error {
		if err := json.Unmarshal(body, &scheduledResp); err != nil {
			return fmt.Errorf("failed to unmarshal response: %w", err)
		}

		return nil
	}); err != nil {
		return nil, err
	}

	if scheduledResp.Code != http.StatusOK {
		c.handleStatusCodeError(scheduledResp.Code)

		return nil, &apperrors.APIError{
			Operation:  chzzkGetScheduledLivesOp,
			StatusCode: scheduledResp.Code,
			Err: fmt.Errorf(
				"chzzk api code=%d message=%s",
				scheduledResp.Code,
				scheduledResp.Message,
			),
		}
	}

	if scheduledResp.Content == nil {
		return []ScheduledLive{}, nil
	}

	return scheduledResp.Content.ScheduledLives, nil
}
