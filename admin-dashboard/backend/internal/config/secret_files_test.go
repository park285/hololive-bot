package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func writeSecretForTest(t *testing.T, name, value string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

func TestSecretInputsNeverMaterializeInProcessEnvironment(t *testing.T) {
	path := writeSecretForTest(t, "session-secret", strings.Repeat("s", 32)+"\n")
	t.Setenv("SESSION_SECRET_FILE", path)
	t.Setenv("SESSION_SECRET", "")
	t.Setenv("ADMIN_SECRET_KEY", "")

	in := readInputs()
	if err := in.applySecretFiles(); err != nil {
		t.Fatal(err)
	}

	if in.values["SESSION_SECRET"] != strings.Repeat("s", 32) {
		t.Fatal("file input was not loaded")
	}

	if os.Getenv("SESSION_SECRET") != "" || os.Getenv("ADMIN_SECRET_KEY") != "" {
		t.Fatal("process environment was modified")
	}
}

func TestApplySecretFilesRejectsAmbiguousDirectSecret(t *testing.T) {
	path := writeSecretForTest(t, "session-secret", strings.Repeat("s", 32))
	t.Setenv("SESSION_SECRET_FILE", path)
	t.Setenv("SESSION_SECRET", strings.Repeat("d", 32))

	if err := readInputs().applySecretFiles(); err == nil {
		t.Fatal("direct secret plus *_FILE must fail")
	}
}

func TestReadSecretFileRejectsSymlinkAndEmbeddedNewline(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")

	if err := os.WriteFile(target, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	if _, err := readSecretFile(link); err == nil {
		t.Fatal("symlink secret path must fail")
	}

	multiline := writeSecretForTest(t, "multiline", "first\nsecond")
	if _, err := readSecretFile(multiline); err == nil {
		t.Fatal("embedded newline secret must fail")
	}
}

func TestLoadSecureRequires32ByteSessionSecret(t *testing.T) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("test-password"), minimumAdminPasswordBcryptCost)
	if err != nil {
		t.Fatalf("GenerateFromPassword() error = %v", err)
	}

	t.Setenv("ENV", "test")
	t.Setenv("ADMIN_PASS_HASH", string(passwordHash))
	t.Setenv("SESSION_SECRET", strings.Repeat("x", 31))
	t.Setenv("VALKEY_URL", "valkey-cache:6379")

	if _, err := Load(); err == nil {
		t.Fatal("31-byte SESSION_SECRET must fail")
	}
}

func TestLoadRejectsWeakAdminPasswordBcryptCost(t *testing.T) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("test-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("GenerateFromPassword() error = %v", err)
	}

	t.Setenv("ENV", "test")
	t.Setenv("ADMIN_PASS_HASH", string(passwordHash))
	t.Setenv("SESSION_SECRET", strings.Repeat("x", 32))

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want weak bcrypt cost rejection")
	} else if !strings.Contains(err.Error(), "bcrypt cost must be at least") {
		t.Fatalf("Load() error = %q, want bcrypt cost context", err)
	}
}
