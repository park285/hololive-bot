package ingestionlease

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"testing"
	"time"

	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/stretchr/testify/require"
)

func TestJobRunGuardOwnerTokenShapeIsPrefixPidUnixNano(t *testing.T) {
	t.Parallel()

	guard := NewJobRunGuard(nil, JobRunGuardConfig{Namespace: "test", InstanceID: "ap-a"})
	token := guard.newOwnerToken()

	pattern := regexp.MustCompile(`^ap-a:` + fmt.Sprintf("%d", os.Getpid()) + `:[0-9]+$`)
	require.Regexp(t, pattern, token)
}

func TestLeaseOwnerTokenShapeIsRolePidUnixNano(t *testing.T) {
	t.Parallel()

	var captured string
	cache := &cachemocks.Client{
		SetNXFunc: func(_ context.Context, _, value string, _ time.Duration) (bool, error) {
			captured = value
			return true, nil
		},
	}

	lease, err := Acquire(context.Background(), cache, "bot", nil)
	require.NoError(t, err)

	pattern := regexp.MustCompile(`^bot:` + fmt.Sprintf("%d", os.Getpid()) + `:[0-9]+$`)
	require.Regexp(t, pattern, captured)
	require.Equal(t, captured, lease.owner)
}
