package docker

import "testing"

const testBusinessContainerName = "hololive-api"

const (
	testAdminContainerName = "admin-dashboard"
	testStartAction        = "start"
	testStopAction         = "stop"
	testRestartAction      = "restart"
)

func TestParseHealth(t *testing.T) {
	if got := parseHealth("Up 2 hours (healthy)"); got == nil || *got != "healthy" {
		t.Fatalf("unexpected health: %v", got)
	}

	if got := parseHealth("Exited (0) 2 hours ago"); got != nil {
		t.Fatalf("unexpected health: %v", got)
	}
}

func TestIsManaged(t *testing.T) {
	client := &Client{}
	if !client.IsManaged(testBusinessContainerName) {
		t.Fatal("hololive container should be managed")
	}

	if client.IsManaged("hololive-api-init") {
		t.Fatal("init container should be excluded")
	}

	if client.IsManaged("random") {
		t.Fatal("random container should not be managed")
	}
}

func TestStopBlocked(t *testing.T) {
	client := &Client{}
	cases := map[string]bool{
		"valkey-cache":            true,
		"holo-postgres":           true,
		testAdminContainerName:    true,
		"deunhealth":              true,
		testBusinessContainerName: false,
		"docker-proxy":            false,
	}

	for name, want := range cases {
		if got := client.stopBlocked(name); got != want {
			t.Fatalf("stopBlocked(%q) = %v, want %v", name, got, want)
		}
	}
}
