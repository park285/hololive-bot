package youtubejs

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/parser"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/ratelimiter"
)

func TestClientFetchCommunityDecodesHelperPosts(t *testing.T) {
	t.Parallel()
	published := time.Date(2026, 4, 10, 10, 11, 12, 0, time.FixedZone("KST", 9*3600))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/community" || r.Method != http.MethodPost {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		var req CommunityRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.ChannelID != "UC_TEST" || req.MaxResults != 10 || req.MaxPages != 1 {
			t.Fatalf("request = %#v", req)
		}
		_ = json.NewEncoder(w).Encode(CommunityResult{
			Posts: []*parser.CommunityPost{{
				PostID: "post-1", UpstreamPostID: "post-1", AuthorID: "UC_TEST",
				AuthorName: "Author", ContentText: "hello world",
				PublishedText: published.Format(time.RFC3339), LikeCount: 1200, CommentCount: 7,
			}},
			Pagination: Pagination{PageCount: 1, Exhausted: true, Continuity: "CONTIGUOUS"},
		})
	}))
	t.Cleanup(server.Close)

	client := NewRPC(server.Client(), server.URL, ratelimiter.New(0))
	result, err := client.FetchCommunity(context.Background(), CommunityRequest{
		ChannelID: "UC_TEST", MaxResults: 10, MaxPages: 1,
	})
	if err != nil {
		t.Fatalf("FetchCommunity: %v", err)
	}
	if len(result.Posts) != 1 || result.Posts[0].PostID != "post-1" || !result.Exhausted {
		t.Fatalf("result = %#v", result)
	}
	if result.Posts[0].PublishedAt == nil || !result.Posts[0].PublishedAt.Equal(published) {
		t.Fatalf("PublishedAt = %v, want %v", result.Posts[0].PublishedAt, published)
	}
}

func TestClientFetchFailClosesOnHelperError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"innertube down","error_code":"collection_failed"}`))
	}))
	t.Cleanup(server.Close)

	client := NewRPC(server.Client(), server.URL, nil)
	_, err := client.FetchCommunity(context.Background(), CommunityRequest{ChannelID: "UC_FAIL"})
	if err == nil || !strings.Contains(err.Error(), "innertube down") {
		t.Fatalf("FetchCommunity error = %v, want innertube down", err)
	}
}

func TestClientFetchDoesNotCallHTMLGetCommunityPosts(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/posts") {
			t.Fatal("helper client must not fetch the HTML /posts page")
		}
		_ = json.NewEncoder(w).Encode(CommunityResult{
			Pagination: Pagination{PageCount: 1, Exhausted: true, Continuity: "CONTIGUOUS"},
		})
	}))
	t.Cleanup(server.Close)

	client := NewRPC(server.Client(), server.URL, nil)
	result, err := client.FetchCommunity(context.Background(), CommunityRequest{ChannelID: "UC_EMPTY"})
	if err != nil {
		t.Fatalf("FetchCommunity: %v", err)
	}
	if len(result.Posts) != 0 {
		t.Fatalf("posts = %#v, want empty", result.Posts)
	}
}

func TestStartFailsWithoutNode(t *testing.T) {
	t.Parallel()
	_, _, err := Start(context.Background(), Config{
		NodePath:   "/no/such/node",
		ScriptPath: "/no/such/server.mjs",
		SocketPath: t.TempDir() + "/youtubejs.sock",
	})
	if err == nil {
		t.Fatal("Start must fail when node is missing")
	}
}

func TestClientSetProxyEnabled(t *testing.T) {
	t.Parallel()
	client := NewRPC(http.DefaultClient, "http://youtubejs", nil)
	if client.ProxyEnabled() {
		t.Fatal("proxy must start disabled")
	}
	if !client.SetProxyEnabled(true) || !client.ProxyEnabled() {
		t.Fatal("SetProxyEnabled(true) must stick")
	}
}

func TestHelperProcessEnvOmitsSecrets(t *testing.T) {
	t.Parallel()
	got := helperProcessEnv([]string{
		"PATH=/usr/bin",
		"HOME=/tmp",
		"TZ=Asia/Seoul",
		"POSTGRES_PASSWORD=super-secret",
		"API_SECRET_KEY=api-secret",
		"HOLODEX_API_KEY=holodex-secret",
		"YOUTUBEJS_NODE=/usr/local/bin/node",
		"not-a-pair",
	})
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, "PATH=/usr/bin") || !strings.Contains(joined, "YOUTUBEJS_NODE=/usr/local/bin/node") {
		t.Fatalf("helper env missing required keys: %#v", got)
	}
	for _, forbidden := range []string{"POSTGRES_PASSWORD", "API_SECRET_KEY", "HOLODEX_API_KEY"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("helper env leaked %s: %#v", forbidden, got)
		}
	}
}
