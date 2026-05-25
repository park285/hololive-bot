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
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/park285/shared-go/pkg/httputil"
)

const (
	DefaultBaseURL = "https://api.chzzk.naver.com"
	OpenAPIBaseURL = "https://openapi.chzzk.naver.com"

	chzzkGetLiveStatusOp     = "chzzk_get_live_status"
	chzzkGetScheduledLivesOp = "chzzk_get_scheduled_lives"
	chzzkGetLivesOp          = "chzzk_get_lives"
	chzzkGetChannelsOp       = "chzzk_get_channels"

	defaultHTTPClientTimeout = 10 * time.Second
)

type Client struct {
	httpClient                *http.Client
	baseURL                   string
	openAPIBaseURL            string
	clientID                  string
	clientSecret              string
	maxLivesPageSize          int
	batchLookupThreshold      int
	maxConcurrentStatusChecks int
	maxResponseBodyBytes      int64
	logger                    *slog.Logger
	circuitOpenUntil          *time.Time
	circuitMu                 sync.RWMutex
	failureCount              int
}

type ClientConfig struct {
	HTTPClient                *http.Client
	BaseURL                   string
	ClientID                  string
	ClientSecret              string
	MaxLivesPageSize          int
	BatchLookupThreshold      int
	MaxConcurrentStatusChecks int
	MaxResponseBodyBytes      int64
	Logger                    *slog.Logger
}

func NewClient(httpClient *http.Client, baseURL string, logger *slog.Logger) *Client {
	d := config.DefaultChzzkOperationalConfig()
	return &Client{
		httpClient:                defaultHTTPClient(httpClient),
		baseURL:                   defaultBaseURL(baseURL),
		openAPIBaseURL:            OpenAPIBaseURL,
		maxLivesPageSize:          d.MaxLivesPageSize,
		batchLookupThreshold:      d.BatchLookupThreshold,
		maxConcurrentStatusChecks: d.MaxConcurrentStatusChecks,
		maxResponseBodyBytes:      config.DefaultMaxResponseBodyBytes,
		logger:                    defaultClientLogger(logger),
	}
}

func NewClientWithConfig(cfg ClientConfig) *Client {
	d := config.DefaultChzzkOperationalConfig()
	mlps := cfg.MaxLivesPageSize
	if mlps == 0 {
		mlps = d.MaxLivesPageSize
	}
	blt := cfg.BatchLookupThreshold
	if blt == 0 {
		blt = d.BatchLookupThreshold
	}
	mcsc := cfg.MaxConcurrentStatusChecks
	if mcsc == 0 {
		mcsc = d.MaxConcurrentStatusChecks
	}
	maxBody := cfg.MaxResponseBodyBytes
	if maxBody == 0 {
		maxBody = config.DefaultMaxResponseBodyBytes
	}
	return &Client{
		httpClient:                defaultHTTPClient(cfg.HTTPClient),
		baseURL:                   defaultBaseURL(cfg.BaseURL),
		openAPIBaseURL:            OpenAPIBaseURL,
		clientID:                  cfg.ClientID,
		clientSecret:              cfg.ClientSecret,
		maxLivesPageSize:          mlps,
		batchLookupThreshold:      blt,
		maxConcurrentStatusChecks: mcsc,
		maxResponseBodyBytes:      maxBody,
		logger:                    defaultClientLogger(cfg.Logger),
	}
}

func defaultClientLogger(logger *slog.Logger) *slog.Logger {
	if logger == nil {
		return slog.Default()
	}

	return logger
}

func defaultHTTPClient(httpClient *http.Client) *http.Client {
	if httpClient != nil {
		return httpClient
	}

	return httputil.NewExternalAPIClient(defaultHTTPClientTimeout)
}

func defaultBaseURL(baseURL string) string {
	if strings.TrimSpace(baseURL) == "" {
		return DefaultBaseURL
	}

	return baseURL
}

func (c *Client) HasOpenAPICredentials() bool {
	return c.clientID != "" && c.clientSecret != ""
}

func escapedChannelPath(channelID string) (string, error) {
	channelID = strings.TrimSpace(channelID)
	if channelID == "" {
		return "", fmt.Errorf("channel id is empty")
	}

	return url.PathEscape(channelID), nil
}
