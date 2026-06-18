package status

import (
	"testing"
	"time"

	"github.com/quic-go/quic-go/http3"
)

func TestEndpointClientsSelectH3ForHTTPS(t *testing.T) {
	clients := endpointClients([]ServiceEndpoint{
		{Name: "h3-svc", URL: "https://hololive-admin-api:30006", HealthPath: "/health"},
		{Name: "tcp-svc", URL: "http://localhost:30190", HealthPath: "/health"},
	}, time.Second)

	if _, isH3 := clients["h3-svc"].client.Transport.(*http3.Transport); !isH3 {
		t.Fatalf("h3-svc transport = %T, want *http3.Transport", clients["h3-svc"].client.Transport)
	}
	if _, isH3 := clients["tcp-svc"].client.Transport.(*http3.Transport); isH3 {
		t.Fatal("tcp-svc must not use http3 transport")
	}
}

func TestHubSubscribeReplaysCappedHistory(t *testing.T) {
	hub := NewHub(nil)
	for i := range historyCap + 5 {
		hub.Publish(&SystemStats{ThreadCount: i})
	}

	history, updates, cancel := hub.Subscribe()
	defer cancel()

	if len(history) != historyCap {
		t.Fatalf("history length = %d, want %d", len(history), historyCap)
	}
	if got := history[len(history)-1].ThreadCount; got != historyCap+4 {
		t.Fatalf("latest replayed sample = %d, want %d", got, historyCap+4)
	}

	hub.Publish(&SystemStats{ThreadCount: 999})
	select {
	case live := <-updates:
		if live.ThreadCount != 999 {
			t.Fatalf("live sample = %d, want 999", live.ThreadCount)
		}
	case <-time.After(time.Second):
		t.Fatal("live update not delivered after replay")
	}
}

func TestSendDropOldestNeverBlocks(t *testing.T) {
	for _, prefill := range []int{0, 1, 4} {
		ch := make(chan SystemStats, 4)
		for range prefill {
			ch <- SystemStats{}
		}
		done := make(chan struct{})
		go func() {
			sendDropOldest(ch, &SystemStats{ThreadCount: 1})
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatalf("sendDropOldest blocked on channel with prefill=%d", prefill)
		}
	}
}

func TestPublishNeverDeadlocksWithRacingConsumer(t *testing.T) {
	hub := NewHub(nil)
	_, updates, cancel := hub.Subscribe()
	defer cancel()

	consumed := make(chan struct{})
	go func() {
		for range updates {
		}
		close(consumed)
	}()

	done := make(chan struct{})
	go func() {
		for range 50000 {
			hub.Publish(&SystemStats{})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Publish deadlocked against a racing consumer")
	}
}

func TestFormatDuration(t *testing.T) {
	if FormatDuration(5*time.Minute) != "5m" {
		t.Fatal("minute formatting failed")
	}
	if FormatDuration(2*time.Hour) != "2h 0m" {
		t.Fatal("hour formatting failed")
	}
}

func TestMemoryStatsFromProc(t *testing.T) {
	old := osReadFile
	defer func() { osReadFile = old }()
	osReadFile = func(name string) ([]byte, error) {
		return []byte("MemTotal: 1000 kB\nMemAvailable: 250 kB\n"), nil
	}
	total, used := memoryStats()
	if total != 1024000 || used != 768000 {
		t.Fatalf("unexpected memory stats: %d %d", total, used)
	}
}
