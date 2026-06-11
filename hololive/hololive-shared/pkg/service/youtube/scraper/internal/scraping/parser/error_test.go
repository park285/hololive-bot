package parser

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParserDriftError_ErrorWithCause(t *testing.T) {
	err := NewParserDriftError("recent_videos", "decode", errors.New("boom"))
	assert.Equal(t, "recent_videos parser drift at decode: boom", err.Error())
}

func TestParserDriftError_ErrorWithoutCause(t *testing.T) {
	err := NewParserDriftError("stats", "extract", nil)
	assert.Equal(t, "stats parser drift at extract", err.Error())
}

func TestParserDriftError_NilReceiverError(t *testing.T) {
	var err *ParserDriftError
	assert.Equal(t, "youtube parser drift", err.Error())
}

func TestParserDriftError_NilReceiverUnwrap(t *testing.T) {
	var err *ParserDriftError
	assert.Nil(t, err.Unwrap())
}

func TestParserDriftError_IsParserDrift(t *testing.T) {
	cause := errors.New("root cause")
	err := NewParserDriftError("upcoming", "parse", cause)
	assert.True(t, IsParserDriftError(err))
	assert.True(t, errors.Is(err, ErrParserDrift))
	assert.True(t, errors.Is(err, cause))
}

func TestParserDriftError_UnwrapJoinsSentinelAndCause(t *testing.T) {
	cause := errors.New("xml error")
	err := NewParserDriftError("rss", "unmarshal", cause)
	var drift *ParserDriftError
	require.True(t, errors.As(err, &drift))
	assert.Equal(t, "rss", drift.Operation)
	assert.Equal(t, "unmarshal", drift.Stage)
	assert.Equal(t, cause, drift.Cause)
}

func TestIsParserDriftError_NonDriftError(t *testing.T) {
	assert.False(t, IsParserDriftError(errors.New("unrelated")))
	assert.False(t, IsParserDriftError(nil))
}

func TestParserDriftError_UnwrapWithNilCause(t *testing.T) {
	err := NewParserDriftError("op", "stage", nil)
	assert.True(t, errors.Is(err, ErrParserDrift))
}
