package retrier

import (
	"errors"
	"sync"
	"testing"
	"time"
)

type alwaysFail struct{}

func (alwaysFail) Exec() error { return errors.New("boom") }

type alwaysSucceed struct{}

func (alwaysSucceed) Exec() error { return nil }

// rendezvous blocks each caller until all callers have arrived, proving that
// independent Retriers are genuinely executing concurrently: if only one ran
// at a time, the first would deadlock and the bounded timeout would fire.
type rendezvous struct {
	mu      sync.Mutex
	entered int
	total   int
	allIn   chan struct{}
}

func newRendezvous(total int) *rendezvous {
	return &rendezvous{total: total, allIn: make(chan struct{})}
}

func (r *rendezvous) Enter() {
	r.mu.Lock()
	r.entered++
	if r.entered == r.total {
		close(r.allIn)
	}
	r.mu.Unlock()
	<-r.allIn
}

// gated rendezvous-gates the first Exec call and then delegates to inner.
type gated struct {
	inner Retrieable
	gate  *rendezvous
	once  sync.Once
}

func (g *gated) Exec() error {
	g.once.Do(g.gate.Enter)
	return g.inner.Exec()
}

// waitDone waits deterministically for a Retrier to complete using a bounded
// safety timeout instead of a fixed sleep.
func waitDone(t *testing.T, r *Retrier) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		r.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("retrier did not complete within bounded timeout")
	}
}

func TestNewValidation(t *testing.T) {
	task := alwaysSucceed{}
	if _, err := New(task, 0, 1); err == nil {
		t.Fatal("expected error for maxAttempt < 1")
	}
	if _, err := New(task, 1, 0); err == nil {
		t.Fatal("expected error for waitDuration < 1")
	}
	if _, err := New(nil, 1, 1); err == nil {
		t.Fatal("expected error for nil retriable")
	}
}

func TestSuccessCompletesDeterministically(t *testing.T) {
	r, err := New(alwaysSucceed{}, 3, 1)
	if err != nil {
		t.Fatal(err)
	}
	r.Start(nil, nil)
	waitDone(t, r)
	if r.Err() != nil {
		t.Fatalf("expected nil error, got %v", r.Err())
	}
	if r.Attempts() != 1 {
		t.Fatalf("expected 1 attempt, got %d", r.Attempts())
	}
}

func TestFailureExhaustsAttempts(t *testing.T) {
	r, err := New(alwaysFail{}, 3, 1)
	if err != nil {
		t.Fatal(err)
	}
	r.Start(nil, nil)
	waitDone(t, r)
	if r.Err() == nil {
		t.Fatal("expected non-nil error")
	}
	if r.Attempts() != 3 {
		t.Fatalf("expected 3 attempts, got %d", r.Attempts())
	}
}

func TestConcurrentRetriersCompleteDeterministically(t *testing.T) {
	gate := newRendezvous(2)

	r1, err := New(&gated{inner: alwaysFail{}, gate: gate}, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := New(&gated{inner: alwaysFail{}, gate: gate}, 2, 1)
	if err != nil {
		t.Fatal(err)
	}

	r1.Start(nil, nil)
	r2.Start(nil, nil)

	waitDone(t, r1)
	waitDone(t, r2)

	if r1.Attempts() != 2 {
		t.Fatalf("r1 expected 2 attempts, got %d", r1.Attempts())
	}
	if r2.Attempts() != 2 {
		t.Fatalf("r2 expected 2 attempts, got %d", r2.Attempts())
	}
	if r1.Err() == nil || r2.Err() == nil {
		t.Fatal("expected non-nil errors for both retriers")
	}
}
