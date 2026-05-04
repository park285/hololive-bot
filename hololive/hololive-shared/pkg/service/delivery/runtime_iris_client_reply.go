package delivery

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/park285/iris-client-go/iris"
	"golang.org/x/net/http2"
)

const irisReplyPath = "/reply"

type irisTextReplyRequest struct {
	Type string `json:"type"`
	Room string `json:"room"`
	Data string `json:"data"`
}

func (c *RuntimeIrisClient) SendMessageAccepted(ctx context.Context, room, message string, opts ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	if len(opts) > 0 {
		if err := c.SendMessage(ctx, room, message, opts...); err != nil {
			return nil, err
		}
		return nil, nil
	}
	return c.sendMessageAccepted(ctx, room, message)
}

func (c *RuntimeIrisClient) sendMessageAccepted(ctx context.Context, room, message string) (*iris.ReplyAcceptedResponse, error) {
	if c == nil {
		return nil, fmt.Errorf("runtime iris client: client is nil")
	}
	if c.botToken == "" {
		return nil, fmt.Errorf("runtime iris client: bot token is empty")
	}

	c.mu.Lock()
	baseURL, err := c.resolveBaseURLLocked()
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}

	body, err := json.Marshal(irisTextReplyRequest{
		Type: "text",
		Room: room,
		Data: message,
	})
	if err != nil {
		return nil, fmt.Errorf("encode iris text reply: %w", err)
	}

	req, err := newSignedIrisRequest(ctx, baseURL, c.botToken, http.MethodPost, irisReplyPath, body)
	if err != nil {
		return nil, err
	}

	resp, err := newIrisReplyHTTPClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("send iris text reply: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read iris text reply response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("iris text reply returned %d: %s", resp.StatusCode, string(respBody))
	}

	var accepted iris.ReplyAcceptedResponse
	if len(bytes.TrimSpace(respBody)) > 0 {
		if err := json.Unmarshal(respBody, &accepted); err != nil {
			return nil, fmt.Errorf("decode iris text reply response: %w", err)
		}
	}

	return &accepted, nil
}

func newSignedIrisRequest(ctx context.Context, baseURL, secret, method, path string, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build iris text reply request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	timestamp := fmt.Sprintf("%d", time.Now().UnixMilli())
	nonce, err := randomHex(16)
	if err != nil {
		return nil, fmt.Errorf("build iris text reply nonce: %w", err)
	}
	bodyHashBytes := sha256.Sum256(body)
	bodyHash := hex.EncodeToString(bodyHashBytes[:])

	canonical := fmt.Sprintf("%s\n%s\n%s\n%s\n%s", method, path, timestamp, nonce, bodyHash)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(canonical))

	req.Header.Set("X-Iris-Timestamp", timestamp)
	req.Header.Set("X-Iris-Nonce", nonce)
	req.Header.Set("X-Iris-Signature", hex.EncodeToString(mac.Sum(nil)))
	req.Header.Set("X-Iris-Body-Sha256", bodyHash)

	return req, nil
}

func randomHex(bytesLen int) (string, error) {
	raw := make([]byte, bytesLen)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func newIrisReplyHTTPClient() *http.Client {
	dialer := &net.Dialer{
		Timeout:   3 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	return &http.Client{
		Timeout: constants.RequestTimeout.BotCommand,
		Transport: &http2.Transport{
			AllowHTTP:       true,
			IdleConnTimeout: 90 * time.Second,
			DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
				return dialer.DialContext(ctx, network, addr)
			},
		},
	}
}
