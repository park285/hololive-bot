package youtubejs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/parser"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/ratelimiter"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
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
		if req.ProtocolVersion != ProtocolVersion || req.ChannelID != "UC_TEST" || req.MaxResults != 10 || req.MaxPages != 1 {
			t.Fatalf("request = %#v", req)
		}
		if strings.Contains(string(raw), "proxy_url") || strings.Contains(string(raw), "max_aggregate_bytes") {
			t.Fatalf("collection request contains removed fields: %s", raw)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(CommunityResult{
			ProtocolVersion: ProtocolVersion,
			Posts: []*parser.CommunityPost{{
				PostID: "post-1", UpstreamPostID: "post-1", AuthorID: "UC_TEST",
				AuthorName: "Author", ContentText: "hello world",
				PublishedText: published.Format(time.RFC3339), LikeCount: 1200, CommentCount: 7,
			}},
			PageCount:         1,
			Exhausted:         true,
			Continuity:        "CONTIGUOUS",
			TerminationReason: TerminationExhausted,
		}); err != nil {
			t.Errorf("encode community result: %v", err)
		}
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
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		if _, err := w.Write([]byte(`{"protocol_version":1,"error":{"code":"collection_failed","class":"TRANSIENT","retry":{"kind":"default"},"message":"innertube down"}}`)); err != nil {
			t.Errorf("write helper error: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	client := NewRPC(server.Client(), server.URL, nil)
	_, err := client.FetchCommunity(context.Background(), CommunityRequest{ChannelID: "UC_FAIL"})
	if err == nil || !strings.Contains(err.Error(), "innertube down") {
		t.Fatalf("FetchCommunity error = %v, want innertube down", err)
	}
}

func TestClientFetchPreservesHelperErrorClass(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		if _, err := w.Write([]byte(`{"protocol_version":1,"error":{"code":"collection_failed","class":"TRANSIENT","retry":{"kind":"default"},"message":"innertube down"}}`)); err != nil {
			t.Errorf("write helper error: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	client := NewRPC(server.Client(), server.URL, nil)
	_, err := client.FetchCommunity(context.Background(), CommunityRequest{ChannelID: "UC_FAIL"})
	if err == nil || collecterr.CodeOf(err) != collecterr.Failed || collecterr.ClassOf(err) != collecterr.ClassTransient {
		t.Fatalf("FetchCommunity code/class = %q/%q, error = %v", collecterr.CodeOf(err), collecterr.ClassOf(err), err)
	}
}

func TestClientFetchDoesNotCallHTMLGetCommunityPosts(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/posts") {
			t.Fatal("helper client must not fetch the HTML /posts page")
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(CommunityResult{
			ProtocolVersion:   ProtocolVersion,
			PageCount:         1,
			Exhausted:         true,
			Continuity:        "CONTIGUOUS",
			TerminationReason: TerminationExhausted,
		}); err != nil {
			t.Errorf("encode empty community result: %v", err)
		}
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
	_, _, err := Start(context.Background(), &Config{
		NodePath:       "/no/such/node",
		ScriptPath:     "/no/such/server.mjs",
		RuntimeBaseDir: t.TempDir(),
	})
	if err == nil {
		t.Fatal("Start must fail when node is missing")
	}
}

func TestHelperStartingClassifiesOnlyTransientSocketErrors(t *testing.T) {
	t.Parallel()
	for _, err := range []error{
		fmt.Errorf("dial socket: %w", os.ErrNotExist),
		fmt.Errorf("dial socket: %w", syscall.ECONNREFUSED),
	} {
		if !helperStarting(err) {
			t.Fatalf("helperStarting(%v) = false, want true", err)
		}
	}
	for _, err := range []error{context.DeadlineExceeded, syscall.EACCES, errors.New("invalid response")} {
		if helperStarting(err) {
			t.Fatalf("helperStarting(%v) = true, want false", err)
		}
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
		"YOUTUBEJS_SOCKET=/tmp/foreign.sock",
		"not-a-pair",
	})
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, "PATH=/usr/bin") || !strings.Contains(joined, "YOUTUBEJS_NODE=/usr/local/bin/node") {
		t.Fatalf("helper env missing required keys: %#v", got)
	}
	for _, forbidden := range []string{"POSTGRES_PASSWORD", "API_SECRET_KEY", "HOLODEX_API_KEY", "YOUTUBEJS_SOCKET"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("helper env leaked %s: %#v", forbidden, got)
		}
	}
}

func TestHelperExitStateBecomesObservable(t *testing.T) {
	waited := make(chan struct{})
	helper := &Helper{waited: waited}
	if helper.Exited() {
		t.Fatal("helper must be running before wait completion")
	}
	waitErr := errors.New("fixture exit")
	helper.waitErr = waitErr
	close(waited)
	if !helper.Exited() || !errors.Is(helper.ExitError(), waitErr) {
		t.Fatalf("helper exit state = exited:%t err:%v", helper.Exited(), helper.ExitError())
	}
	select {
	case <-helper.Done():
	default:
		t.Fatal("helper done channel must close after exit")
	}
}

func TestHLP001(t *testing.T) {
	base := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	first, _, err := Start(ctx, liveHelperConfig(t, base))
	if err != nil {
		t.Fatalf("first Start: %v", err)
	}
	second, _, err := Start(ctx, liveHelperConfig(t, base))
	if err != nil {
		t.Fatalf("second Start: %v", err)
	}
	if first.runtimeDir == second.runtimeDir || first.socketPath == second.socketPath {
		t.Fatalf("helpers shared runtime paths: %s %s", first.runtimeDir, second.runtimeDir)
	}
	assertPrivateRuntime(t, first)
	assertPrivateRuntime(t, second)
	if err := first.Close(ctx); err != nil {
		t.Fatalf("close first: %v", err)
	}
	if err := second.Close(ctx); err != nil {
		t.Fatalf("close second: %v", err)
	}
	assertNoResidue(t, base)
}

func TestHLP002(t *testing.T) {
	base := t.TempDir()
	regular := filepath.Join(base, "regular")
	if err := os.WriteFile(regular, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := Start(context.Background(), &Config{
		NodePath: nodePath(t), ScriptPath: helperScriptPath(t), RuntimeBaseDir: regular,
	})
	if err == nil {
		t.Fatal("Start must fail when runtime base dir is a regular file")
	}
	if _, statErr := os.Lstat(regular); statErr != nil {
		t.Fatalf("regular file was removed: %v", statErr)
	}

	target := filepath.Join(base, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	_, _, err = Start(context.Background(), &Config{
		NodePath: nodePath(t), ScriptPath: helperScriptPath(t), RuntimeBaseDir: link,
	})
	if err == nil {
		t.Fatal("Start must fail when runtime base dir is a symlink")
	}
	info, statErr := os.Lstat(link)
	if statErr != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("symlink was removed or replaced: %v %#v", statErr, info)
	}

	decoy := filepath.Join(base, "youtubejs-community.sock")
	if err := os.WriteFile(decoy, []byte("foreign"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	helper, _, err := Start(ctx, liveHelperConfig(t, base))
	if err != nil {
		t.Fatalf("Start with decoy: %v", err)
	}
	if _, statErr := os.Lstat(decoy); statErr != nil {
		t.Fatalf("decoy path was unlinked: %v", statErr)
	}
	if err := helper.Close(ctx); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestHLP003(t *testing.T) {
	base := t.TempDir()
	cfg := fixtureConfig(t, base, "insecure")
	_, _, err := Start(context.Background(), cfg)
	if err == nil {
		t.Fatal("Start must fail when socket mode is insecure")
	}
	if !strings.Contains(err.Error(), "group or other") {
		t.Fatalf("error = %v, want socket permission failure", err)
	}
	assertNoResidue(t, base)
}

func TestHLP004(t *testing.T) {
	base := t.TempDir()
	long := filepath.Join(base, strings.Repeat("a", 80))
	if err := os.Mkdir(long, 0o700); err != nil {
		t.Fatal(err)
	}
	_, _, err := Start(context.Background(), &Config{
		NodePath: nodePath(t), ScriptPath: helperScriptPath(t), RuntimeBaseDir: long, MaxInflight: 4,
	})
	if err == nil {
		t.Fatal("Start must fail when socket path exceeds 100 bytes")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("error = %v, want path length failure", err)
	}
	assertNoResidue(t, long)
}

func TestHLP005(t *testing.T) {
	base := t.TempDir()
	_, _, err := Start(context.Background(), fixtureConfig(t, base, "reject-bootstrap"))
	if err == nil {
		t.Fatal("Start must fail on protocol mismatch")
	}
	if collecterr.CodeOf(err) != collecterr.HelperProtocolMismatch {
		t.Fatalf("error = %v, want protocol mismatch", err)
	}
	assertNoResidue(t, base)
}

func TestHLP006(t *testing.T) {
	base := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cfg := liveHelperConfig(t, base)
	cfg.RequestBodyLimit = 32 << 10
	cfg.ResponseBodyLimit = 256 << 10
	cfg.MaxInflight = 3
	helper, _, err := Start(ctx, cfg)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		if err := helper.Close(context.Background()); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	if helper.bootstrap.State != StateReady || helper.bootstrap.RequestBodyBytes != 32<<10 ||
		helper.bootstrap.ResponseBodyBytes != 256<<10 || helper.bootstrap.MaxInflight != 3 || helper.bootstrap.ProxyEnabled {
		t.Fatalf("bootstrap = %#v", helper.bootstrap)
	}
	if err := helper.Healthy(ctx); err != nil {
		t.Fatalf("Healthy: %v", err)
	}
}

func TestHLP007(t *testing.T) {
	base := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cfg := liveHelperConfig(t, base)
	helper, _, err := Start(ctx, cfg)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		if err := helper.Close(context.Background()); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	status, body, _, err := helper.postBootstrap(ctx, matchingBootstrap(cfg))
	if err != nil || status != http.StatusOK || body.State != StateReady || body.MaxInflight != cfg.MaxInflight {
		t.Fatalf("replay status=%d body=%#v err=%v", status, body, err)
	}
	if err := helper.Healthy(ctx); err != nil {
		t.Fatalf("Healthy after replay: %v", err)
	}
}

func TestHLP008(t *testing.T) {
	base := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cfg := liveHelperConfig(t, base)
	helper, _, err := Start(ctx, cfg)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		if err := helper.Close(context.Background()); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	conflict := matchingBootstrap(cfg)
	conflict.Limits.MaxInflight = cfg.MaxInflight + 1
	status, _, raw, err := helper.postBootstrap(ctx, conflict)
	if err != nil || status != http.StatusConflict {
		t.Fatalf("conflict status=%d raw=%s err=%v", status, raw, err)
	}
	if helper.Healthy(ctx) != nil {
		t.Fatal("original READY config must stay healthy")
	}
	if helper.bootstrap.MaxInflight != cfg.MaxInflight {
		t.Fatalf("max inflight changed to %d", helper.bootstrap.MaxInflight)
	}
}

func TestHLP009(t *testing.T) {
	base := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	helper, _, err := Start(ctx, liveHelperConfig(t, base))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	runtimeDir := helper.runtimeDir
	if err := helper.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if helper.ForcedKillCount() != 0 {
		t.Fatalf("SIGKILL count = %d, want 0", helper.ForcedKillCount())
	}
	if !helper.Exited() {
		t.Fatal("child must be reaped")
	}
	if _, err := os.Lstat(runtimeDir); !os.IsNotExist(err) {
		t.Fatalf("runtime dir still exists: %v", err)
	}
	assertNoResidue(t, base)
}

func TestHLP010(t *testing.T) {
	base := t.TempDir()
	cfg := fixtureConfig(t, base, "ignore-term")
	cfg.ShutdownTimeout = 200 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	helper, _, err := Start(ctx, cfg)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := helper.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if helper.ForcedKillCount() != 1 {
		t.Fatalf("SIGKILL count = %d, want 1", helper.ForcedKillCount())
	}
	if !helper.Exited() {
		t.Fatal("child must be reaped after SIGKILL")
	}
	assertNoResidue(t, base)
}

func TestWaitErr(t *testing.T) {
	base := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	helper, _, err := Start(ctx, fixtureConfig(t, base, "exit-error-on-term"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := helper.Close(ctx); err == nil || !strings.Contains(err.Error(), "exit status 7") {
		t.Fatalf("Close error = %v, want child exit status", err)
	}
	if helper.ForcedKillCount() != 0 {
		t.Fatalf("SIGKILL count = %d, want 0", helper.ForcedKillCount())
	}
	assertNoResidue(t, base)
}

func TestHLP011(t *testing.T) {
	base := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	helper, _, err := Start(ctx, liveHelperConfig(t, base))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	errs := make([]error, 3)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = helper.Close(ctx)
		}(i)
	}
	wg.Wait()
	for _, closeErr := range errs {
		if closeErr != nil {
			t.Fatalf("Close error: %v", closeErr)
		}
	}
	if helper.ForcedKillCount() != 0 {
		t.Fatalf("duplicate Close forced extra SIGKILL: %d", helper.ForcedKillCount())
	}
	assertNoResidue(t, base)
}

func TestHLP012(t *testing.T) {
	t.Run("missing-bin", testHLP012MissingBin)
	t.Run("child-exit", testHLP012ChildExit)
	t.Run("insecure-socket", testHLP012InsecureSocket)
	t.Run("no-child", testHLP012NoChild)
	t.Run("spawn-fail", testHLP012SpawnFail)
}

func testHLP012MissingBin(t *testing.T) {
	base := t.TempDir()
	_, _, err := Start(context.Background(), &Config{
		NodePath: "/no/such/node", ScriptPath: "/no/such/server.mjs", RuntimeBaseDir: base,
	})
	if err == nil {
		t.Fatal("expected start failure")
	}
	assertNoResidue(t, base)
}

func testHLP012ChildExit(t *testing.T) {
	base := t.TempDir()
	_, _, err := Start(context.Background(), fixtureConfig(t, base, "exit-now"))
	if err == nil {
		t.Fatal("expected start failure")
	}
	if !strings.Contains(err.Error(), "exited before ready") {
		t.Fatalf("error = %v, want exited before ready", err)
	}
	assertNoResidue(t, base)
}

func testHLP012InsecureSocket(t *testing.T) {
	base := t.TempDir()
	_, _, err := Start(context.Background(), fixtureConfig(t, base, "insecure"))
	if err == nil {
		t.Fatal("expected start failure")
	}
	assertNoResidue(t, base)
}

func testHLP012NoChild(t *testing.T) {
	base := t.TempDir()
	dir, socketPath, err := createRuntimeDir(base)
	if err != nil {
		t.Fatal(err)
	}
	helper := newHelper(dir, socketPath, &Config{ShutdownTimeout: 3 * time.Second})
	started := time.Now()
	if err := helper.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if elapsed := time.Since(started); elapsed >= time.Second {
		t.Fatalf("Close without child waited %s", elapsed)
	}
	if errors.Is(helper.closeErr, ErrCleanupTimedOut) {
		t.Fatal("Close without child returned CLEANUP_TIMED_OUT")
	}
	if _, err := os.Lstat(dir); !os.IsNotExist(err) {
		t.Fatalf("runtime dir remains: %v", err)
	}
}

func testHLP012SpawnFail(t *testing.T) {
	base := t.TempDir()
	node := filepath.Join(base, "not-node")
	if err := os.WriteFile(node, []byte("#!not-a-binary\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	_, _, err := Start(context.Background(), &Config{
		NodePath:        node,
		ScriptPath:      helperScriptPath(t),
		RuntimeBaseDir:  base,
		ShutdownTimeout: 3 * time.Second,
		MaxInflight:     4,
	})
	if err == nil {
		t.Fatal("expected spawn failure")
	}
	if errors.Is(err, ErrCleanupTimedOut) {
		t.Fatalf("spawn fail returned CLEANUP_TIMED_OUT: %v", err)
	}
	if elapsed := time.Since(started); elapsed >= time.Second {
		t.Fatalf("spawn-fail cleanup waited %s", elapsed)
	}
	assertNoResidue(t, base)
}

func TestBootstrapErrorOmitsProxyUserinfo(t *testing.T) {
	const secret = "super-secret"
	base := t.TempDir()
	cfg := fixtureConfig(t, base, "leak-proxy")
	cfg.Proxy = ProxyConfig{Enabled: true, URL: "http://user:" + secret + "@127.0.0.1:9"}
	_, _, err := Start(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected bootstrap failure")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("Go error leaked proxy userinfo: %v", err)
	}
	assertNoResidue(t, base)
}

func liveHelperConfig(t *testing.T, base string) *Config {
	t.Helper()
	return &Config{
		NodePath:          nodePath(t),
		ScriptPath:        helperScriptPath(t),
		RuntimeBaseDir:    base,
		StartupTimeout:    15 * time.Second,
		RequestTimeout:    5 * time.Second,
		HealthTimeout:     time.Second,
		ShutdownTimeout:   3 * time.Second,
		RequestBodyLimit:  DefaultRequestBodyLimit,
		ResponseBodyLimit: DefaultResponseBodyLimit,
		MaxInflight:       4,
	}
}

func fixtureConfig(t *testing.T, base, mode string) *Config {
	t.Helper()
	cfg := liveHelperConfig(t, base)
	cfg.ScriptPath = fixtureScriptPath(t)
	cfg.extraArgs = []string{"--mode", mode}
	return cfg
}

func matchingBootstrap(cfg *Config) BootstrapRequest {
	return BootstrapRequest{
		ProtocolVersion: ProtocolVersion,
		Proxy:           BootstrapProxy{Enabled: cfg.Proxy.Enabled, URL: cfg.Proxy.URL},
		Limits: BootstrapLimits{
			RequestBodyBytes:  cfg.RequestBodyLimit,
			ResponseBodyBytes: cfg.ResponseBodyLimit,
			MaxInflight:       cfg.MaxInflight,
		},
	}
}

func assertPrivateRuntime(t *testing.T, helper *Helper) {
	t.Helper()
	info, err := os.Lstat(helper.runtimeDir)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Fatalf("runtime dir mode = %v", info.Mode())
	}
	socket, err := os.Lstat(helper.socketPath)
	if err != nil {
		t.Fatal(err)
	}
	if socket.Mode().Type() != os.ModeSocket || socket.Mode().Perm()&0o077 != 0 {
		t.Fatalf("socket mode = %v", socket.Mode())
	}
}

func assertNoResidue(t *testing.T, base string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(base, helperRuntimeDirPrefix+"*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("leaked helper runtime dirs: %v", matches)
	}
}

func helperScriptPath(t *testing.T) string {
	t.Helper()
	return absSibling(t, filepath.Join("..", "..", "..", "youtubejs", "src", "server.mjs"))
}

func fixtureScriptPath(t *testing.T) string {
	t.Helper()
	return absSibling(t, filepath.Join("testdata", "fixture-helper.mjs"))
}

func absSibling(t *testing.T, rel string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate helper_test.go")
	}
	path, err := filepath.Abs(filepath.Join(filepath.Dir(file), rel))
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func nodePath(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("node")
	if err != nil {
		t.Fatal(err)
	}
	return path
}
