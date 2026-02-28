package notification

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-shared/pkg/domain"
)

const (
	// alarmDispatchQueue: Rust → Go 알림 발송 위임 큐 키
	alarmDispatchQueue = "alarm:dispatch:queue"
	// brpopTimeout: BRPOP 블로킹 대기 시간
	brpopTimeout = 5 * time.Second
	// drainWindow: 첫 메시지 이후 추가 메시지를 drain 하는 시간
	drainWindow = 200 * time.Millisecond
	// maxDrainBatch: 한 번에 drain 할 최대 메시지 수
	maxDrainBatch = 50
)

// supportedVersions: 처리 가능한 봉투 버전 집합. 0은 버전 필드 없는 레거시(v1 취급).
var supportedVersions = map[uint8]struct{}{
	0: {},
	1: {},
}

// AlarmQueueConsumer: Valkey List 큐에서 알림 발송 봉투를 소비하는 컨슈머
type AlarmQueueConsumer struct {
	client valkey.Client
	logger *slog.Logger
}

// NewAlarmQueueConsumer: 큐 컨슈머 생성
func NewAlarmQueueConsumer(client valkey.Client, logger *slog.Logger) *AlarmQueueConsumer {
	return &AlarmQueueConsumer{
		client: client,
		logger: logger,
	}
}

// DrainBatch: BRPOP으로 첫 메시지를 대기한 후 drainWindow 내 추가 메시지를 RPOP으로 수집한다.
// context 취소 시 빈 슬라이스를 반환한다.
func (c *AlarmQueueConsumer) DrainBatch(ctx context.Context) ([]*domain.AlarmQueueEnvelope, error) {
	// BRPOP 블로킹 대기
	cmd := c.client.B().Brpop().Key(alarmDispatchQueue).Timeout(brpopTimeout.Seconds()).Build()
	resp := c.client.Do(ctx, cmd)

	if err := resp.Error(); err != nil {
		// timeout (nil) 또는 context 취소
		if valkey.IsValkeyNil(err) {
			return nil, nil
		}
		if ctx.Err() != nil {
			return nil, fmt.Errorf("BRPOP context canceled: %w", ctx.Err())
		}
		return nil, fmt.Errorf("BRPOP 실패: %w", err)
	}

	// BRPOP 응답은 [key, value] 배열
	arr, err := resp.AsStrSlice()
	if err != nil || len(arr) < 2 {
		return nil, fmt.Errorf("BRPOP 응답 파싱 실패: %w", err)
	}

	envelopes := make([]*domain.AlarmQueueEnvelope, 0, maxDrainBatch)

	first, parseErr := parseEnvelope(arr[1])
	if parseErr != nil {
		c.logParseError("첫 메시지", parseErr)
	} else {
		envelopes = append(envelopes, first)
	}

	// drain window 내 추가 메시지 수집
	deadline := time.Now().Add(drainWindow)
	for len(envelopes) < maxDrainBatch && time.Now().Before(deadline) {
		rpopCmd := c.client.B().Rpop().Key(alarmDispatchQueue).Build()
		rpopResp := c.client.Do(ctx, rpopCmd)

		if rpopErr := rpopResp.Error(); rpopErr != nil {
			break // 큐가 비었거나 에러
		}

		raw, strErr := rpopResp.ToString()
		if strErr != nil {
			break
		}

		env, pErr := parseEnvelope(raw)
		if pErr != nil {
			c.logParseError("drain", pErr)
			continue
		}
		envelopes = append(envelopes, env)
	}

	return envelopes, nil
}

// ReleaseClaimKeys: 발송 실패 시 Rust가 설정한 claim 키를 DEL 하여 재시도를 허용한다.
func (c *AlarmQueueConsumer) ReleaseClaimKeys(ctx context.Context, keys []string) {
	validKeys := filterValidClaimKeys(keys)
	if len(validKeys) == 0 {
		return
	}

	cmd := c.client.B().Del().Key(validKeys...).Build()
	if err := c.client.Do(ctx, cmd).Error(); err != nil {
		c.logger.Warn("claim 키 해제 실패", slog.Any("error", err), slog.Int("key_count", len(validKeys)))
	}
}

// logParseError: 파싱 오류를 버전 불일치 여부에 따라 분기 로깅한다.
func (c *AlarmQueueConsumer) logParseError(stage string, err error) {
	if errors.Is(err, errUnsupportedVersion) {
		// 미지원 버전은 정상 drop — 버전 번호를 error 메시지에서 추출하여 기록
		c.logger.Warn("미지원 봉투 버전, skip", slog.String("stage", stage), slog.Any("error", err))
		return
	}
	c.logger.Warn("큐 메시지 파싱 실패", slog.String("stage", stage), slog.Any("error", err))
}

func filterValidClaimKeys(keys []string) []string {
	if len(keys) == 0 {
		return nil
	}

	valid := make([]string, 0, len(keys))
	for _, key := range keys {
		trimmed := strings.TrimSpace(key)
		if strings.HasPrefix(trimmed, NotifyClaimKeyPrefix) {
			valid = append(valid, trimmed)
		}
	}
	return valid
}

// errUnsupportedVersion: 미지원 버전 봉투를 caller가 식별할 수 있도록 센티넬 에러로 정의
var errUnsupportedVersion = fmt.Errorf("미지원 봉투 버전")

func parseEnvelope(raw string) (*domain.AlarmQueueEnvelope, error) {
	var env domain.AlarmQueueEnvelope
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		return nil, fmt.Errorf("envelope JSON 파싱 실패: %w", err)
	}
	if _, ok := supportedVersions[env.Version]; !ok {
		return nil, fmt.Errorf("version=%d: %w", env.Version, errUnsupportedVersion)
	}
	return &env, nil
}
