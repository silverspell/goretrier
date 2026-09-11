package retrier

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// concurrencyGate is a Retrieable whose Exec blocks until release is closed,
// while tracking how many Exec calls are in flight at once.
type concurrencyGate struct {
	mu          sync.Mutex
	inFlight    int
	maxInFlight int
	release     chan struct{}
	entered     chan struct{}
}

func (g *concurrencyGate) Exec() error {
	g.mu.Lock()
	g.inFlight++
	if g.inFlight > g.maxInFlight {
		g.maxInFlight = g.inFlight
	}
	g.mu.Unlock()

	g.entered <- struct{}{}
	<-g.release

	g.mu.Lock()
	g.inFlight--
	g.mu.Unlock()

	return nil
}

func waitEntered(t *testing.T, ch chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Exec entry")
	}
}

func TestStartSerializesOverlappingCalls(t *testing.T) {
	gate := &concurrencyGate{
		release: make(chan struct{}),
		entered: make(chan struct{}, 8),
	}
	r, err := New(gate, 1, 1)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	wg := &sync.WaitGroup{}

	r.Start(wg, nil)
	waitEntered(t, gate.entered)

	r.Start(wg, nil)

	select {
	case <-gate.entered:
		t.Fatal("second Start entered Exec before the first finished")
	case <-time.After(50 * time.Millisecond):
	}

	close(gate.release)
	wg.Wait()

	if gate.maxInFlight != 1 {
		t.Fatalf("Exec ran concurrently: maxInFlight=%d, want 1", gate.maxInFlight)
	}
}

func TestDifferentInstancesRunConcurrently(t *testing.T) {
	gate := &concurrencyGate{
		release: make(chan struct{}),
		entered: make(chan struct{}, 8),
	}
	r1, err := New(gate, 1, 1)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r2, err := New(gate, 1, 1)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	wg := &sync.WaitGroup{}

	r1.Start(wg, nil)
	r2.Start(wg, nil)

	waitEntered(t, gate.entered)
	waitEntered(t, gate.entered)

	if gate.maxInFlight != 2 {
		t.Fatalf("different instances did not run concurrently: maxInFlight=%d, want 2", gate.maxInFlight)
	}

	close(gate.release)
	wg.Wait()
}

type failingExec struct {
	attempts int32
}

func (f *failingExec) Exec() error {
	atomic.AddInt32(&f.attempts, 1)
	return errors.New("boom")
}

func TestMaxAttemptsResetPerRun(t *testing.T) {
	f := &failingExec{}
	r, err := New(f, 3, 1)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	wg := &sync.WaitGroup{}

	r.Start(wg, nil)
	wg.Wait()
	if got := r.Attempts(); got != 3 {
		t.Fatalf("first run attempts=%d, want 3", got)
	}

	r.Start(wg, nil)
	wg.Wait()
	if got := r.Attempts(); got != 3 {
		t.Fatalf("second run attempts=%d, want 3 (fresh per-run state)", got)
	}

	if total := atomic.LoadInt32(&f.attempts); total != 6 {
		t.Fatalf("total Exec calls=%d, want 6", total)
	}
}
