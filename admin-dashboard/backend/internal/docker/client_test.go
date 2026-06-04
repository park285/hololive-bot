package docker

import "testing"

func TestParseHealth(t *testing.T) {
	if got := parseHealth("Up 2 hours (healthy)"); got == nil || *got != "healthy" {
		t.Fatalf("unexpected health: %v", got)
	}
	if got := parseHealth("Exited (0) 2 hours ago"); got != nil {
		t.Fatalf("unexpected health: %v", got)
	}
}

func TestIsManaged(t *testing.T) {
	client := &Client{managedPrefixes: []string{"hololive", "admin"}, excludeSuffixes: []string{"-init"}}
	if !client.IsManaged("hololive-admin-api") {
		t.Fatal("hololive container should be managed")
	}
	if client.IsManaged("hololive-admin-api-init") {
		t.Fatal("init container should be excluded")
	}
	if client.IsManaged("random") {
		t.Fatal("random container should not be managed")
	}
}
