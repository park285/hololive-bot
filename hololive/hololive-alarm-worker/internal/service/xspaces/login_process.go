package xspaces

import (
	"bytes"
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"io"
	"os/exec"
	"time"

	sessions "github.com/kapu/hololive-shared/pkg/service/xspaces"
)

// LoginResult는 로그인 helper의 고정 결과입니다. Cookies는 후보 저장에만 사용합니다.
type LoginResult struct {
	Cookies *sessions.Cookies `json:"cookies,omitempty"`
	Error   string            `json:"error,omitempty"`
}

// ProcessLogin은 전용 컨테이너에서 최대 120초 동안 브라우저 로그인을 수행합니다.
// Fatal은 하위 프로세스 잔존 가능성이 있어 호출 서비스 전체를 종료해야 함을 뜻합니다.
func ProcessLogin(ctx context.Context, config *LoginConfig) (result LoginResult, fatal bool) {
	raw, err := jsonv2.Marshal(struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}{config.Username, config.Password})
	if err != nil {
		return LoginResult{Error: "browser_failed"}, false
	}
	defer clear(raw)

	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)

	defer cancel()

	command := exec.CommandContext(ctx, "/usr/bin/node", "/app/xspaces-login/src/login.mjs")

	command.Env = []string{"LANG=C.UTF-8", "HOME=/tmp", "PATH=/usr/bin:/bin", "PLAYWRIGHT_BROWSERS_PATH=/ms-playwright"}
	command.Stdin = bytes.NewReader(raw)
	command.Stderr = io.Discard

	output := &boundedOutput{}

	command.Stdout = output
	command.WaitDelay = time.Second

	runErr := command.Run()

	defer clear(output.Bytes())

	if ctx.Err() != nil || errors.Is(runErr, exec.ErrWaitDelay) {
		return LoginResult{Error: "outcome_unknown"}, true
	}

	if err = jsonv2.Unmarshal(output.Bytes(), &result, jsonv2.RejectUnknownMembers(true)); err != nil {
		return LoginResult{Error: "outcome_unknown"}, true
	}

	if result.Cookies != nil && result.Error == "" && runErr == nil && result.Cookies.Validate() == nil {
		return result, false
	}

	if result.Cookies == nil {
		switch result.Error {
		case "additional_authentication", "login_rejected", "browser_failed", "outcome_unknown":
			return result, runErr != nil
		}
	}

	return LoginResult{Error: "outcome_unknown"}, true
}
