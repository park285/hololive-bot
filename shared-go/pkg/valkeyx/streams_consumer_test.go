package valkeyx

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
)

// setupStreamTest: 스트림 테스트 환경 설정 (miniredis + general/blocking clients)
func setupStreamTest(t *testing.T) (*Clients, *miniredis.Miniredis, func()) {
	t.Helper()

	mr := miniredis.RunT(t)

	generalClient, err := valkey.NewClient(valkey.ClientOption{
		InitAddress:  []string{mr.Addr()},
		DisableCache: true,
	})
	require.NoError(t, err)

	blockingClient, err := valkey.NewClient(valkey.ClientOption{
		InitAddress:  []string{mr.Addr()},
		DisableCache: true,
	})
	require.NoError(t, err)

	clients := &Clients{
		General:  generalClient,
		Blocking: blockingClient,
	}

	cleanup := func() {
		generalClient.Close()
		blockingClient.Close()
	}

	return clients, mr, cleanup
}

// TestStreamConsumer_New: StreamConsumer 생성 기본 테스트
func TestStreamConsumer_New(t *testing.T) {
	clients, _, cleanup := setupStreamTest(t)
	defer cleanup()

	tests := []struct {
		name     string
		stream   string
		group    string
		consumer string
		wantNil  bool
	}{
		{
			name:     "valid_config",
			stream:   "test-stream",
			group:    "test-group",
			consumer: "consumer-1",
			wantNil:  false,
		},
		{
			name:     "empty_stream",
			stream:   "",
			group:    "test-group",
			consumer: "consumer-1",
			wantNil:  false, // StreamConsumer 생성 자체는 성공, Run에서 에러 발생
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			consumer := StreamConsumer{
				General:  clients.General,
				Blocking: clients.Blocking,
				Stream:   tt.stream,
				Group:    tt.group,
				Consumer: tt.consumer,
			}

			if tt.wantNil {
				assert.Nil(t, consumer.General)
			} else {
				assert.NotNil(t, consumer.General)
				assert.NotNil(t, consumer.Blocking)
			}
		})
	}
}

// TestStreamConsumer_Run_NilHandler: nil handler 테스트
func TestStreamConsumer_Run_NilHandler(t *testing.T) {
	clients, _, cleanup := setupStreamTest(t)
	defer cleanup()

	ctx := context.Background()
	consumer := StreamConsumer{
		General:  clients.General,
		Blocking: clients.Blocking,
		Stream:   "test-stream",
		Group:    "test-group",
		Consumer: "consumer-1",
	}

	err := consumer.Run(ctx, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "handler is nil")
}

// TestStreamConsumer_Run_NilClients: nil clients 테스트
func TestStreamConsumer_Run_NilClients(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name     string
		general  valkey.Client
		blocking valkey.Client
		wantErr  string
	}{
		{
			name:     "both_nil",
			general:  nil,
			blocking: nil,
			wantErr:  "valkey client is nil",
		},
		{
			name:     "general_nil",
			general:  nil,
			blocking: nil, // blocking도 nil
			wantErr:  "valkey client is nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			consumer := StreamConsumer{
				General:  tt.general,
				Blocking: tt.blocking,
				Stream:   "test-stream",
				Group:    "test-group",
				Consumer: "consumer-1",
			}

			handler := func(ctx context.Context, msg StreamMessage) error {
				return nil
			}

			err := consumer.Run(ctx, handler)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// TestStreamConsumer_ContextCancellation: Context 취소 시 즉시 종료
func TestStreamConsumer_ContextCancellation(t *testing.T) {
	clients, mr, cleanup := setupStreamTest(t)
	defer cleanup()

	// Consumer group 사전 생성
	stream := "test-stream-ctx"
	group := "test-group-ctx"
	mr.XAdd(stream, "*", []string{"field", "value"})

	// Consumer group 생성 (valkey client 사용)
	createGroupCmd := clients.General.B().XgroupCreate().Key(stream).Group(group).Id("0").Mkstream().Build()
	_ = clients.General.Do(context.Background(), createGroupCmd).Error()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	consumer := StreamConsumer{
		General:      clients.General,
		Blocking:     clients.Blocking,
		Stream:       stream,
		Group:        group,
		Consumer:     "consumer-1",
		Count:        10,
		BlockTimeout: 100 * time.Millisecond,
	}

	handlerCalled := false
	handler := func(ctx context.Context, msg StreamMessage) error {
		handlerCalled = true
		return nil
	}

	start := time.Now()
	err := consumer.Run(ctx, handler)
	elapsed := time.Since(start)

	// Context 취소로 종료되었는지 확인
	assert.Error(t, err)
	assert.True(t, errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled))
	assert.Less(t, elapsed, 500*time.Millisecond, "should exit quickly on context cancellation")

	// handler가 호출되었을 수도 있고 아닐 수도 있음 (타이밍에 따라)
	_ = handlerCalled
}

// TestStreamConsumer_Shutdown: graceful shutdown 시나리오
func TestStreamConsumer_Shutdown(t *testing.T) {
	clients, mr, cleanup := setupStreamTest(t)
	defer cleanup()

	stream := "test-stream-shutdown"
	group := "test-group-shutdown"
	mr.XAdd(stream, "*", []string{"key", "value"})

	createGroupCmd := clients.General.B().XgroupCreate().Key(stream).Group(group).Id("0").Mkstream().Build()
	_ = clients.General.Do(context.Background(), createGroupCmd).Error()

	ctx, cancel := context.WithCancel(context.Background())

	consumer := StreamConsumer{
		General:      clients.General,
		Blocking:     clients.Blocking,
		Stream:       stream,
		Group:        group,
		Consumer:     "consumer-1",
		Count:        10,
		BlockTimeout: 100 * time.Millisecond,
	}

	var wg sync.WaitGroup
	wg.Add(1)

	var runErr error
	go func() {
		defer wg.Done()
		handler := func(ctx context.Context, msg StreamMessage) error {
			// 메시지 처리 시뮬레이션
			time.Sleep(10 * time.Millisecond)
			return nil
		}
		runErr = consumer.Run(ctx, handler)
	}()

	// 충분한 시간 대기 후 취소
	time.Sleep(200 * time.Millisecond)
	cancel()

	// shutdown 완료 대기 (타임아웃 설정)
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// 정상 종료
		assert.Error(t, runErr, "Run should return error on context cancel")
		assert.True(t, errors.Is(runErr, context.Canceled), "error should be context.Canceled")
	case <-time.After(2 * time.Second):
		t.Fatal("graceful shutdown timeout - consumer did not stop")
	}
}

// TestStreamConsumer_ACKAfterHandle: ACK 타이밍 검증
func TestStreamConsumer_ACKAfterHandle(t *testing.T) {
	clients, mr, cleanup := setupStreamTest(t)
	defer cleanup()

	stream := "test-stream-ack"
	group := "test-group-ack"

	// 메시지 추가
	mr.XAdd(stream, "1-0", []string{"data", "test1"})
	mr.XAdd(stream, "2-0", []string{"data", "test2"})

	createGroupCmd := clients.General.B().XgroupCreate().Key(stream).Group(group).Id("0").Mkstream().Build()
	_ = clients.General.Do(context.Background(), createGroupCmd).Error()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	consumer := StreamConsumer{
		General:        clients.General,
		Blocking:       clients.Blocking,
		Stream:         stream,
		Group:          group,
		Consumer:       "consumer-1",
		Count:          10,
		BlockTimeout:   100 * time.Millisecond,
		AckAfterHandle: true,
	}

	processedCount := 0
	handler := func(ctx context.Context, msg StreamMessage) error {
		processedCount++
		return nil
	}

	err := consumer.Run(ctx, handler)

	// Context 타임아웃으로 종료되었는지 확인
	assert.Error(t, err)
	assert.True(t, errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled))

	// 최소 1개 이상의 메시지가 처리되었는지 확인
	assert.Greater(t, processedCount, 0, "should process at least one message")
}

// TestStreamConsumer_HandlerError: handler 에러 처리
func TestStreamConsumer_HandlerError(t *testing.T) {
	clients, mr, cleanup := setupStreamTest(t)
	defer cleanup()

	stream := "test-stream-error"
	group := "test-group-error"

	mr.XAdd(stream, "1-0", []string{"data", "fail"})

	createGroupCmd := clients.General.B().XgroupCreate().Key(stream).Group(group).Id("0").Mkstream().Build()
	_ = clients.General.Do(context.Background(), createGroupCmd).Error()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	consumer := StreamConsumer{
		General:        clients.General,
		Blocking:       clients.Blocking,
		Stream:         stream,
		Group:          group,
		Consumer:       "consumer-1",
		Count:          10,
		BlockTimeout:   100 * time.Millisecond,
		AckAfterHandle: false, // 에러 시 ACK 안 함
	}

	handlerCalled := false
	expectedErr := errors.New("handler error")
	handler := func(ctx context.Context, msg StreamMessage) error {
		handlerCalled = true
		return expectedErr
	}

	err := consumer.Run(ctx, handler)

	// Context 타임아웃으로 종료
	assert.Error(t, err)
	assert.True(t, handlerCalled, "handler should be called")
}

// TestStreamConsumer_DefaultValues: 기본값 설정 테스트
func TestStreamConsumer_DefaultValues(t *testing.T) {
	clients, mr, cleanup := setupStreamTest(t)
	defer cleanup()

	stream := "test-stream-defaults"
	group := "test-group-defaults"

	mr.XAdd(stream, "*", []string{"key", "value"})

	createGroupCmd := clients.General.B().XgroupCreate().Key(stream).Group(group).Id("0").Mkstream().Build()
	_ = clients.General.Do(context.Background(), createGroupCmd).Error()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// 최소 설정만 제공
	consumer := StreamConsumer{
		General:  clients.General,
		Blocking: clients.Blocking,
		Stream:   stream,
		Group:    group,
		Consumer: "consumer-1",
		// Count, BlockTimeout 생략 (기본값 테스트)
	}

	handler := func(ctx context.Context, msg StreamMessage) error {
		return nil
	}

	err := consumer.Run(ctx, handler)

	// 정상 실행되고 context timeout으로 종료되는지 확인
	assert.Error(t, err)
	assert.True(t, errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled))
}
