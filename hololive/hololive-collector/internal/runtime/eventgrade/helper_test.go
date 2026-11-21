package eventgrade

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBootstrapFileRejectsMissingExtraRepeatedAndSharedCredentials(t *testing.T) {
	const valid = `{"app_version":"fixture","api_secret":"fixture-secret","credentials":{"jp":"j","us":"u","as":"a"}}`

	for _, tc := range []struct {
		data string
		mode os.FileMode
		ok   bool
	}{
		{valid, 0o400, true},
		{valid, 0o600, false},
		{valid, 0o444, false},
		{strings.Replace(valid, `,"as":"a"`, "", 1), 0o400, false},
		{strings.Replace(valid, `"as":"a"`, `"as":""`, 1), 0o400, false},
		{strings.Replace(valid, `"us":"u"`, `"us":"j"`, 1), 0o400, false},
		{strings.Replace(valid, `"jp":`, `"jp":"j","jp":`, 1), 0o400, false},
		{strings.Replace(valid, `"jp":`, `"url":"forbidden","jp":`, 1), 0o400, false},
	} {
		path := filepath.Join(t.TempDir(), "bundle")
		require.NoError(t, os.WriteFile(path, []byte(tc.data), tc.mode))
		require.NoError(t, os.Chmod(path, tc.mode))

		_, err := readGameConfig(path)

		if tc.ok {
			require.NoError(t, err)
		} else {
			require.Error(t, err)
			require.NotContains(t, err.Error(), "fixture-secret")
		}
	}
}

func TestHelperRejectsProtocolMismatchAndOversizedResponse(t *testing.T) {
	for _, reply := range []string{`{"version":2,"status":"ready"}`, `{"version":1,"status":"ready","unexpected":true}`, `{"version":1,"status":true}`, strings.Repeat("x", maxResponse+1)} {
		dir := t.TempDir()
		socket := filepath.Join(dir, "s")
		ln, err := new(net.ListenConfig).Listen(t.Context(), "unix", socket)
		require.NoError(t, err)
		require.NoError(t, os.Chmod(socket, 0o600))

		srv := http.Server{
			Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")

				if _, err := io.WriteString(w, reply); err != nil {
					return
				}
			}),
			ReadHeaderTimeout: time.Second,
		}
		serveErr := make(chan error, 1)

		go func() { serveErr <- srv.Serve(ln) }()

		dial := net.Dialer{}
		tr := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) { return dial.DialContext(ctx, "unix", socket) }}
		h := &Helper{client: &http.Client{Transport: tr}, done: make(chan struct{})}
		require.Error(t, h.bootstrap(t.Context(), socket, gameConfig{}))
		tr.CloseIdleConnections()
		require.NoError(t, srv.Close())
		require.ErrorIs(t, <-serveErr, http.ErrServerClosed)
	}
}
