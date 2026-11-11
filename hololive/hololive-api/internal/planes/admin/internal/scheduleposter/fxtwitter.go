package scheduleposter

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/park285/shared-go/v2/pkg/httputil"
	"github.com/park285/shared-go/v2/pkg/netguard"
)

const (
	fxTwitterHost  = "api.fxtwitter.com"
	imageHost      = "pbs.twimg.com"
	maxPageBytes   = 2 << 20
	maxSourceBytes = 8 << 20
	userAgent      = "hololive-bot-schedule-poster"
)

type statusPage struct {
	Results []status `json:"results"`
	Cursor  struct {
		Bottom string `json:"bottom"`
	} `json:"cursor"`
}

type status struct {
	ID               string `json:"id"`
	Text             string `json:"text"`
	CreatedTimestamp int64  `json:"created_timestamp"`
	Author           struct {
		ID string `json:"id"`
	} `json:"author"`
	// 해당 없음은 null이고, 미디어 없음은 객체 대신 빈 배열로 온다.
	RepostedBy jsontext.Value `json:"reposted_by"`
	ReplyingTo jsontext.Value `json:"replying_to"`
	Media      jsontext.Value `json:"media"`
}

type statusMedia struct {
	Photos []struct {
		URL string `json:"url"`
	} `json:"photos"`
}

func (s *status) original() bool {
	return isJSONNull(s.RepostedBy) && isJSONNull(s.ReplyingTo)
}

func (s *status) firstPhoto() (string, bool) {
	if len(s.Media) == 0 || s.Media[0] != '{' {
		return "", false
	}

	var media statusMedia

	if err := jsonv2.Unmarshal(s.Media, &media); err != nil || len(media.Photos) == 0 {
		return "", false
	}

	return media.Photos[0].URL, true
}

func isJSONNull(value jsontext.Value) bool {
	return len(value) == 0 || bytes.Equal(value, []byte("null"))
}

type source struct {
	MIME string
	Data []byte
}

type fxClient struct {
	client  *http.Client
	baseURL string
}

func newFXClient() *fxClient {
	policy := netguard.Policy{
		Timeout:      10 * time.Second,
		AllowHost:    func(host string) bool { return host == fxTwitterHost || host == imageHost },
		AllowedPorts: []string{"443"},
		Schemes:      []string{"https"},
	}

	client := netguard.GuardedClient(&http.Client{Timeout: 30 * time.Second}, policy)

	client.CheckRedirect = netguard.RedirectPolicy(netguard.RedirectConfig{Policy: policy, MaxRedirects: 2})

	return &fxClient{client: client, baseURL: "https://" + fxTwitterHost}
}

func (c *fxClient) statuses(ctx context.Context, userID, cursor string) (statusPage, error) {
	endpoint := c.baseURL + "/2/profile/id:" + url.PathEscape(userID) + "/statuses"

	if cursor != "" {
		endpoint += "?" + url.Values{"cursor": {cursor}}.Encode()
	}

	body, _, err := c.get(ctx, endpoint, maxPageBytes)
	if err != nil {
		return statusPage{}, fmt.Errorf("fetch X statuses: %w", err)
	}

	return decodeStatusPage(body)
}

func decodeStatusPage(body []byte) (statusPage, error) {
	var page statusPage

	if err := jsonv2.Unmarshal(body, &page); err != nil {
		return statusPage{}, fmt.Errorf("decode X statuses: %w", err)
	}

	return page, nil
}

func (c *fxClient) image(ctx context.Context, rawURL string) (source, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host != imageHost || parsed.User != nil {
		return source{}, errors.New("poster image URL must be https://" + imageHost)
	}

	body, contentType, err := c.get(ctx, rawURL, maxSourceBytes)
	if err != nil {
		return source{}, fmt.Errorf("fetch poster image: %w", err)
	}

	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || (mediaType != "image/jpeg" && mediaType != "image/png" && mediaType != "image/webp") {
		return source{}, fmt.Errorf("poster image content type %q is not allowed", contentType)
	}

	return source{MIME: mediaType, Data: body}, nil
}

func (c *fxClient) get(ctx context.Context, endpoint string, limit int64) (body []byte, contentType string, err error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return nil, "", fmt.Errorf("create request: %w", err)
	}

	request.Header.Set("User-Agent", userAgent)

	response, err := c.client.Do(request)
	if err != nil {
		return nil, "", fmt.Errorf("send request: %w", err)
	}

	defer response.Body.Close()

	body, err = httputil.ReadAllLimited(response.Body, limit)
	if err != nil {
		return nil, "", fmt.Errorf("read response: %w", err)
	}

	if response.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("unexpected status %d", response.StatusCode)
	}

	return body, strings.TrimSpace(response.Header.Get("Content-Type")), nil
}
