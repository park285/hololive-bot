package valkeyx

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
)

func TestNewClient_Success(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	cfg := Config{
		Addr:         mr.Addr(),
		DisableCache: true,
		DB:           0,
		DialTimeout:  5 * time.Second,
	}

	client, err := NewClient(cfg)
	require.NoError(t, err)
	require.NotNil(t, client)
	defer Close(client)

	err = Ping(context.Background(), client)
	assert.NoError(t, err)
}

func TestNewClient_EmptyAddr(t *testing.T) {
	cfg := Config{
		Addr:         "",
		SocketPath:   "",
		DisableCache: true,
	}

	client, err := NewClient(cfg)
	assert.Error(t, err)
	assert.Nil(t, client)
	assert.Contains(t, err.Error(), "valkey addr is empty and socket path not set")
}

func TestIsNil(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	cfg := Config{
		Addr:         mr.Addr(),
		DisableCache: true,
	}
	client, err := NewClient(cfg)
	require.NoError(t, err)
	defer Close(client)

	_, exists, err := GetBytes(context.Background(), client, "nonexistent_key")
	assert.NoError(t, err)
	assert.False(t, exists)
}

func TestGetBytes_Success(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	mr.Set("testkey", "testvalue")

	cfg := Config{
		Addr:         mr.Addr(),
		DisableCache: true,
	}
	client, err := NewClient(cfg)
	require.NoError(t, err)
	defer Close(client)

	value, exists, err := GetBytes(context.Background(), client, "testkey")
	assert.NoError(t, err)
	assert.True(t, exists)
	assert.Equal(t, []byte("testvalue"), value)
}

func TestGetBytes_NilClient(t *testing.T) {
	var client valkey.Client
	_, _, err := GetBytes(context.Background(), client, "anykey")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "valkey client is nil")
}

func TestSetStringEX_Success(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	cfg := Config{
		Addr:         mr.Addr(),
		DisableCache: true,
	}
	client, err := NewClient(cfg)
	require.NoError(t, err)
	defer Close(client)

	err = SetStringEX(context.Background(), client, "testkey", "testvalue", 10*time.Second)
	assert.NoError(t, err)

	assert.True(t, mr.Exists("testkey"))
	val, _ := mr.Get("testkey")
	assert.Equal(t, "testvalue", val)
}

func TestDeleteKeys_Success(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	mr.Set("{test}key1", "value1")
	mr.Set("{test}key2", "value2")

	cfg := Config{
		Addr:         mr.Addr(),
		DisableCache: true,
	}
	client, err := NewClient(cfg)
	require.NoError(t, err)
	defer Close(client)

	err = DeleteKeys(context.Background(), client, "{test}key1", "{test}key2")
	assert.NoError(t, err)

	assert.False(t, mr.Exists("{test}key1"))
	assert.False(t, mr.Exists("{test}key2"))
}

func TestDeleteKeys_EmptyKeys(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	cfg := Config{
		Addr:         mr.Addr(),
		DisableCache: true,
	}
	client, err := NewClient(cfg)
	require.NoError(t, err)
	defer Close(client)

	err = DeleteKeys(context.Background(), client)
	assert.NoError(t, err)
}

func TestClose_NilSafety(t *testing.T) {
	var client valkey.Client
	assert.NotPanics(t, func() {
		Close(client)
	})
}
