package load

import (
	"strings"
	"testing"
	"time"
)

// 값이 없거나 공백뿐이면 기본값이고, 파싱 오류가 없으면 Err는 nil이다.
func TestStrictEnvUsesDefaultsForUnsetOrBlank(t *testing.T) {
	t.Setenv("STRICT_ENV_TEST_INT", "  ")

	var env StrictEnv

	if got := env.Int("STRICT_ENV_TEST_INT", 7); got != 7 {
		t.Fatalf("Int() = %d, want default 7", got)
	}

	if got := env.Seconds("STRICT_ENV_TEST_UNSET_SECONDS", 90*time.Second); got != 90*time.Second {
		t.Fatalf("Seconds() = %s, want default 90s", got)
	}

	if got := env.Millis("STRICT_ENV_TEST_UNSET_MILLIS", 1500*time.Millisecond); got != 1500*time.Millisecond {
		t.Fatalf("Millis() = %s, want default 1.5s", got)
	}

	if err := env.Err(); err != nil {
		t.Fatalf("Err() = %v, want nil", err)
	}
}

// 잘못된 키를 모두 모아 한 번에 보이고, 오류 문구에 입력 값은 넣지 않는다.
func TestStrictEnvCollectsEveryInvalidKeyWithoutValues(t *testing.T) {
	const secretLike = "s3cr3t-value"

	t.Setenv("STRICT_ENV_TEST_INT", secretLike)
	t.Setenv("STRICT_ENV_TEST_BOOL", secretLike)
	t.Setenv("STRICT_ENV_TEST_FLOAT", secretLike)
	t.Setenv("STRICT_ENV_TEST_SECONDS", "9223372036854775807")

	var env StrictEnv

	env.Int("STRICT_ENV_TEST_INT", 1)
	env.Bool("STRICT_ENV_TEST_BOOL", true)
	env.Float("STRICT_ENV_TEST_FLOAT", 0.5)
	env.Seconds("STRICT_ENV_TEST_SECONDS", time.Second)

	err := env.Err()
	if err == nil {
		t.Fatal("Err() = nil, want every invalid key reported")
	}

	for _, key := range []string{"STRICT_ENV_TEST_INT", "STRICT_ENV_TEST_BOOL", "STRICT_ENV_TEST_FLOAT", "STRICT_ENV_TEST_SECONDS"} {
		if !strings.Contains(err.Error(), key) {
			t.Fatalf("Err() = %v, want it to name %s", err, key)
		}
	}

	if strings.Contains(err.Error(), secretLike) {
		t.Fatalf("Err() = %v, must not echo the raw env value", err)
	}
}
