package eventgrade

import (
	"bytes"
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	observation "github.com/kapu/hololive-shared/pkg/contracts/eventgrade"
)

const (
	protocolVersion = 1
	maxBootstrap    = 16384
	maxResponse     = 131072
)

type bootstrap struct {
	Version int        `json:"version"`
	Secret  gameConfig `json:"secret"`
}

type gameConfig struct {
	AppVersion  string `json:"app_version"`
	APISecret   string `json:"api_secret"`
	Credentials struct {
		JP string `json:"jp"`
		US string `json:"us"`
		AS string `json:"as"`
	} `json:"credentials"`
}

func secretText(s string, limit int) bool {
	if len(s) == 0 || len(s) > limit {
		return false
	}

	for _, c := range s {
		if c < '!' || c > '~' {
			return false
		}
	}

	return true
}

func readPrivateConfig(path string) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0) //nolint:gosec // 절대 경로·일반 파일·권한·symlink 거부를 아래에서 검증한다.
	if err != nil {
		return nil, errors.New("event grade secret file unavailable")
	}

	defer func() { _ = f.Close() }()

	stat, err := f.Stat()
	if err != nil || !stat.Mode().IsRegular() || (stat.Mode().Perm() != 0o400 && stat.Mode().Perm() != 0o440) || stat.Size() > maxBootstrap {
		return nil, errors.New("event grade secret must be a private read-only regular file")
	}

	raw, err := io.ReadAll(io.LimitReader(f, maxBootstrap+1))
	if err != nil || len(raw) > maxBootstrap {
		clear(raw)

		return nil, errors.New("invalid event grade secret document")
	}

	return raw, nil
}

func readGameConfig(path string) (gameConfig, error) {
	raw, err := readPrivateConfig(path)
	if err != nil {
		return gameConfig{}, err
	}
	defer clear(raw)

	var conf gameConfig

	if err := jsonv2.Unmarshal(raw, &conf, jsonv2.RejectUnknownMembers(true)); err != nil {
		return gameConfig{}, errors.New("invalid event grade secret document")
	}

	c := conf.Credentials
	if !secretText(conf.AppVersion, 128) || !secretText(conf.APISecret, 512) || !secretText(c.JP, 2048) || !secretText(c.US, 2048) || !secretText(c.AS, 2048) || c.JP == c.US || c.JP == c.AS || c.US == c.AS {
		return gameConfig{}, errors.New("invalid event grade credentials")
	}

	return conf, nil
}

type Helper struct {
	dir      string
	cmd      *exec.Cmd
	client   *http.Client
	done     chan struct{}
	close    sync.Once
	closeErr error
	waitErr  error
}

func OpenHelper(ctx context.Context, nodePath, scriptPath, secretPath string) (*Helper, error) {
	conf, err := readGameConfig(secretPath)
	if err != nil {
		return nil, err
	}

	dir, err := os.MkdirTemp("", "grade-")
	if err != nil {
		return nil, errors.New("create event grade runtime directory")
	}

	socket := filepath.Join(dir, "rpc.sock")
	cmd := &exec.Cmd{
		Path: nodePath,
		Args: []string{nodePath, "--max-old-space-size=64", scriptPath, socket},
		Env:  []string{"TZ=UTC"},
	}

	dial := net.Dialer{Timeout: time.Second}
	transport := &http.Transport{MaxConnsPerHost: 1, MaxResponseHeaderBytes: 2048, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) { return dial.DialContext(ctx, "unix", socket) }}
	h := &Helper{dir: dir, cmd: cmd, done: make(chan struct{}), client: &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}

	if err := cmd.Start(); err != nil {
		return nil, errors.Join(errors.New("start event grade helper"), os.RemoveAll(dir))
	}

	go func() {
		h.waitErr = cmd.Wait()
		close(h.done)
	}()

	if err := h.bootstrap(ctx, socket, conf); err != nil {
		return nil, errors.Join(err, h.Close())
	}

	return h, nil
}

func (h *Helper) bootstrap(ctx context.Context, socket string, conf gameConfig) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	ticker := time.NewTicker(20 * time.Millisecond)

	defer ticker.Stop()

	for {
		if st, err := os.Lstat(socket); err == nil {
			if st.Mode()&os.ModeSocket == 0 || st.Mode().Perm() != 0o600 {
				return errors.New("invalid event grade socket")
			}

			break
		} else if !errors.Is(err, os.ErrNotExist) {
			return errors.New("inspect event grade socket")
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for event grade socket: %w", ctx.Err())
		case <-h.done:
			return errors.New("event grade helper exited during startup")
		case <-ticker.C:
		}
	}

	var status struct {
		Version int    `json:"version"`
		Status  string `json:"status"`
	}

	if err := h.call(ctx, "/v1/bootstrap", bootstrap{Version: protocolVersion, Secret: conf}, &status); err != nil {
		return err
	}

	if status.Version != protocolVersion {
		return errors.New("event grade helper protocol mismatch")
	}

	if status.Status != "ready" {
		return errors.New("event grade helper not ready")
	}

	return nil
}

func (h *Helper) call(ctx context.Context, path string, in, out any) error {
	data, err := jsonv2.Marshal(in)
	if err != nil || len(data) > maxBootstrap {
		return errors.New("invalid event grade RPC request")
	}

	defer clear(data)

	//nolint:revive // unix 소켓 전송이라 URL host는 사용되지 않고 https는 helper가 제공하지 않는 TLS를 요구한다.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://event-grade-helper"+path, bytes.NewReader(data))
	if err != nil {
		return errors.New("construct event grade RPC")
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := h.client.Do(req)
	if err != nil {
		return errors.New("event grade helper unavailable")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "application/json" {
		return errors.New("event grade RPC rejected")
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil || len(body) > maxResponse || jsonv2.Unmarshal(body, out, jsonv2.RejectUnknownMembers(true)) != nil {
		return errors.New("invalid event grade RPC response")
	}

	return nil
}

func (h *Helper) Close() error {
	h.close.Do(func() {
		h.client.CloseIdleConnections()

		if err := h.cmd.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
			h.closeErr = errors.New("signal event grade helper")
		}

		timer := time.NewTimer(2 * time.Second)

		defer timer.Stop()

		select {
		case <-h.done:
		case <-timer.C:
			if err := h.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
				h.closeErr = errors.Join(h.closeErr, errors.New("kill event grade helper"))
			}

			<-h.done
		}

		h.closeErr = errors.Join(h.closeErr, os.RemoveAll(h.dir))
	})

	return h.closeErr
}

func (h *Helper) Done() <-chan struct{} {
	return h.done
}

func (h *Helper) ExitError() error {
	select {
	case <-h.done:
		return h.waitErr
	default:
		return nil
	}
}

type catalogResponse struct {
	Version           int                   `json:"version"`
	Region            string                `json:"region"`
	Chapters          []observation.Chapter `json:"chapters"`
	Bindings          []observation.Binding `json:"bindings"`
	Conflicts         []string              `json:"conflicts"`
	QuarantinePending *bool                 `json:"quarantine_pending"`
}

type borderRequest struct {
	Version           int       `json:"version"`
	Operation         string    `json:"operation"`
	Region            string    `json:"region"`
	EventID           string    `json:"event_id"`
	ChapterID         string    `json:"chapter_id"`
	FinalAfter        time.Time `json:"final_after"`
	BindingObservedAt time.Time `json:"binding_observed_at"`
}

type borderResponse struct {
	Version int    `json:"version"`
	Border  border `json:"border"`
}

type border struct {
	Region     string             `json:"region"`
	EventID    string             `json:"event_id"`
	ChapterID  string             `json:"chapter_id"`
	BoardKind  string             `json:"board_kind"`
	StartedAt  time.Time          `json:"started_at"`
	ObservedAt time.Time          `json:"observed_at"`
	Final      *bool              `json:"final"`
	Ranks      []observation.Rank `json:"ranks"`
}

type regionRequest struct {
	Version   int    `json:"version"`
	Operation string `json:"operation"`
	Region    string `json:"region"`
}

type ackRequest struct {
	Version   int      `json:"version"`
	Operation string   `json:"operation"`
	Region    string   `json:"region"`
	Conflicts []string `json:"conflicts"`
}

func (h *Helper) catalog(ctx context.Context, region string) (catalogResponse, error) {
	var out catalogResponse

	err := h.call(ctx, "/v1/collect", regionRequest{Version: protocolVersion, Operation: "catalog", Region: region}, &out)

	return out, err
}

func (h *Helper) border(ctx context.Context, req borderRequest) (borderResponse, error) {
	var out borderResponse

	req.Version, req.Operation = protocolVersion, "border"

	err := h.call(ctx, "/v1/collect", req, &out)

	return out, err
}

func (h *Helper) acknowledge(ctx context.Context, region string, conflicts []string) error {
	var out struct {
		Version int    `json:"version"`
		Status  string `json:"status"`
	}

	if err := h.call(ctx, "/v1/collect", ackRequest{Version: protocolVersion, Operation: "ack", Region: region, Conflicts: conflicts}, &out); err != nil {
		return err
	}

	if out.Version != protocolVersion || out.Status != "acknowledged" {
		return errors.New("event grade quarantine acknowledgement invalid")
	}

	return nil
}
