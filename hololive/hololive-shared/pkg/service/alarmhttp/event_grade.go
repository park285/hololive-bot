package alarmhttp

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"

	contract "github.com/kapu/hololive-shared/pkg/contracts/alarm"
)

func (c *Client) RegisterEventGrade(ctx context.Context, req contract.EventGradeRequest) (contract.EventGradeResponse, error) {
	var zero contract.EventGradeResponse

	if !req.Valid() {
		return zero, errors.New("invalid event grade request")
	}

	body, err := encodeJSONRequestBody(contract.EventGradePath, req)
	if err != nil {
		return zero, fmt.Errorf("encode event grade request: %w", err)
	}

	resp, err := c.doRequest(ctx, http.MethodPost, contract.EventGradePath, body, true)
	if err != nil {
		return zero, errors.New("event grade registration unavailable")
	}
	defer c.closeResponseBody(resp, contract.EventGradePath)

	const limit = 2048

	raw, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))

	var result contract.Envelope[contract.EventGradeResponse]

	if err != nil || len(raw) > limit || jsonv2.Unmarshal(raw, &result, jsonv2.RejectUnknownMembers(true)) != nil ||
		!result.Success || !result.Data.Valid(req.Region) {
		return zero, errors.New("invalid event grade response")
	}

	return result.Data, nil
}
