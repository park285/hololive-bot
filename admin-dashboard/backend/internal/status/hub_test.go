package status

import (
	"sync"
	"testing"
	"time"
)

func TestHubHasSubscribersGatesIdlePolling(t *testing.T) {
	hub := NewHub(nil)
	if hub.hasSubscribers() {
		t.Fatal("new hub must report no subscribers (idle poll gated)")
	}

	_, _, unsubscribe := hub.Subscribe()
	if !hub.hasSubscribers() {
		t.Fatal("after Subscribe hub must report subscribers")
	}

	unsubscribe()
	if hub.hasSubscribers() {
		t.Fatal("after unsubscribe hub must report no subscribers")
	}
}

func TestSendDropOldestDropsOldestWhenFull(t *testing.T) {
	ch := make(chan SystemStats, 2)
	ch <- SystemStats{ThreadCount: 1}
	ch <- SystemStats{ThreadCount: 2}

	sendDropOldest(ch, &SystemStats{ThreadCount: 3})

	if len(ch) != 2 {
		t.Fatalf("buffer len = %d, want 2 (one dropped, one added)", len(ch))
	}
	first := <-ch
	second := <-ch
	if first.ThreadCount != 2 {
		t.Fatalf("oldest survivor = %d, want 2 (sample 1 dropped)", first.ThreadCount)
	}
	if second.ThreadCount != 3 {
		t.Fatalf("newest = %d, want 3", second.ThreadCount)
	}
}

func TestSubscribePublishUnsubscribeLifecycle(t *testing.T) {
	hub := NewHub(nil)

	history, updates, cancel := hub.Subscribe()
	if len(history) != 0 {
		t.Fatalf("initial history = %d, want 0", len(history))
	}

	hub.Publish(&SystemStats{ThreadCount: 7})
	select {
	case got := <-updates:
		if got.ThreadCount != 7 {
			t.Fatalf("delivered sample = %d, want 7", got.ThreadCount)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber did not receive published sample")
	}

	cancel()
	if _, ok := <-updates; ok {
		t.Fatal("channel still open after cancel, want closed")
	}

	hub.Publish(&SystemStats{ThreadCount: 9})
}

func TestSubscribeIsolatesSubscribers(t *testing.T) {
	hub := NewHub(nil)
	_, a, cancelA := hub.Subscribe()
	defer cancelA()
	_, b, cancelB := hub.Subscribe()
	defer cancelB()

	hub.Publish(&SystemStats{ThreadCount: 5})

	for name, ch := range map[string]chan SystemStats{"a": a, "b": b} {
		select {
		case got := <-ch:
			if got.ThreadCount != 5 {
				t.Fatalf("subscriber %s sample = %d, want 5", name, got.ThreadCount)
			}
		case <-time.After(time.Second):
			t.Fatalf("subscriber %s did not receive sample", name)
		}
	}
}

func TestUnsubscribeIsIdempotent(t *testing.T) {
	hub := NewHub(nil)
	_, updates, unsubscribe := hub.Subscribe()

	unsubscribe()
	unsubscribe()

	if _, ok := <-updates; ok {
		t.Fatal("updates channel still open after unsubscribe")
	}
	if hub.hasSubscribers() {
		t.Fatal("double unsubscribe left a registered subscriber")
	}
}

func TestStopBeforeStartReturnsAndDoesNotDisableLaterStart(t *testing.T) {
	hub := NewHub(nil)

	stopped := make(chan struct{})
	go func() {
		hub.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Stop before Start blocked")
	}

	hub.Start()
	hub.Stop()
}

func TestConcurrentStartAndStopAreIdempotent(t *testing.T) {
	hub := NewHub(nil)

	var startWG sync.WaitGroup
	for range 8 {
		startWG.Add(1)
		go func() {
			defer startWG.Done()
			hub.Start()
		}()
	}
	startWG.Wait()

	var stopWG sync.WaitGroup
	for range 8 {
		stopWG.Add(1)
		go func() {
			defer stopWG.Done()
			hub.Stop()
		}()
	}
	done := make(chan struct{})
	go func() {
		stopWG.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("concurrent Stop calls did not return")
	}
}

func TestStopTerminatesRunLoopAndIsIdempotent(t *testing.T) {
	hub := NewHub(nil)
	hub.Start()

	done := make(chan struct{})
	go func() {
		hub.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Stop did not return; run loop likely still alive")
	}

	stopAgain := make(chan struct{})
	go func() {
		hub.Stop()
		close(stopAgain)
	}()
	select {
	case <-stopAgain:
	case <-time.After(time.Second):
		t.Fatal("second Stop blocked, want idempotent no-op")
	}
}

func TestTickStopsOnStopSignal(t *testing.T) {
	hub := NewHub(nil)
	close(hub.stop)
	if hub.tick(t.Context(), make(chan time.Time)) {
		t.Fatal("tick returned true after stop signal, want false")
	}
}

func TestTickProcessesTimerTickAndPublishes(t *testing.T) {
	withProcFile(t, map[string][]byte{
		"/proc/meminfo":     []byte("MemTotal: 2048 kB\nMemAvailable: 1024 kB\n"),
		"/proc/loadavg":     []byte("0.1 0.2 0.3 1/1 1\n"),
		"/proc/self/status": []byte("Threads:\t3\n"),
		"/proc/stat":        []byte("cpu  1 2 3 4 5 6 7 8 9 10\n"),
	})
	hub := NewHub(nil)
	_, updates, cancel := hub.Subscribe()
	defer cancel()

	ticks := make(chan time.Time, 1)
	ticks <- time.Now()
	if !hub.tick(t.Context(), ticks) {
		t.Fatal("tick returned false on a real tick, want true")
	}
	select {
	case got := <-updates:
		if got.MemoryTotal != 2048*1024 {
			t.Fatalf("published MemoryTotal = %d, want %d", got.MemoryTotal, 2048*1024)
		}
	case <-time.After(time.Second):
		t.Fatal("tick did not publish a sample to the subscriber")
	}
}
