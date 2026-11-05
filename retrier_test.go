package retrier

import (
	"errors"
	"sync"
	"testing"
	"time"
)

var errAlways = errors.New("always fails")

type stubTask struct {
	mu    sync.Mutex
	errs  []error
	calls int
	times []time.Time
}

func (s *stubTask) Exec() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	idx := s.calls
	s.calls++
	s.times = append(s.times, time.Now())
	if idx < len(s.errs) {
		return s.errs[idx]
	}
	if len(s.errs) > 0 {
		return s.errs[len(s.errs)-1]
	}
	return nil
}

func (s *stubTask) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *stubTask) timestamps() []time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Time(nil), s.times...)
}

func TestMaxAttempts(t *testing.T) {
	r, err := New(&stubTask{}, 3, 1000)
	if err != nil {
		t.Fatalf("New() returned unexpected error: %v", err)
	}

	if got := r.MaxAttempts(); got != 3 {
		t.Fatalf("MaxAttempts() = %d, want 3", got)
	}
}

func TestNoWaitAfterFinalFailure(t *testing.T) {
	task := &stubTask{errs: []error{errAlways}}

	r, err := New(task, 1, 1000)
	if err != nil {
		t.Fatalf("New() returned unexpected error: %v", err)
	}

	done := make(chan time.Duration, 1)
	start := time.Now()
	wg := &sync.WaitGroup{}
	r.Start(wg, func(*Retrier) {
		done <- time.Since(start)
	})

	select {
	case elapsed := <-done:
		if elapsed >= 500*time.Millisecond {
			t.Fatalf("callback took %v; expected no wait after final failure", elapsed)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("callback not invoked within 500ms; Retrier waited after final failure")
	}

	wg.Wait()

	if got := r.Attempts(); got != 1 {
		t.Errorf("Attempts() = %d, want 1", got)
	}
	if r.Err() == nil {
		t.Error("Err() = nil, want non-nil after failing attempt")
	}
	if got := task.callCount(); got != 1 {
		t.Errorf("Exec called %d times, want 1", got)
	}
}

func TestWaitBetweenAttempts(t *testing.T) {
	const waitMS = 50
	task := &stubTask{errs: []error{errAlways}}

	r, err := New(task, 3, waitMS)
	if err != nil {
		t.Fatalf("New() returned unexpected error: %v", err)
	}

	wg := &sync.WaitGroup{}
	r.Start(wg, nil)
	wg.Wait()

	if got := task.callCount(); got != 3 {
		t.Errorf("Exec called %d times, want 3", got)
	}
	if got := r.Attempts(); got != 3 {
		t.Errorf("Attempts() = %d, want 3", got)
	}
	if r.Err() == nil {
		t.Error("Err() = nil, want non-nil after failing attempts")
	}

	times := task.timestamps()
	minGap := time.Duration(waitMS-10) * time.Millisecond
	for i := 1; i < len(times); i++ {
		if gap := times[i].Sub(times[i-1]); gap < minGap {
			t.Errorf("gap between attempt %d and %d is %v, want >= %v", i, i+1, gap, minGap)
		}
	}
}

func TestStopsAfterSuccess(t *testing.T) {
	const waitMS = 50
	task := &stubTask{errs: []error{errAlways, nil}}

	r, err := New(task, 5, waitMS)
	if err != nil {
		t.Fatalf("New() returned unexpected error: %v", err)
	}

	done := make(chan time.Duration, 1)
	start := time.Now()
	wg := &sync.WaitGroup{}
	r.Start(wg, func(*Retrier) {
		done <- time.Since(start)
	})

	select {
	case elapsed := <-done:
		if elapsed >= 200*time.Millisecond {
			t.Fatalf("callback took %v; expected it to stop after success", elapsed)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("callback not invoked")
	}

	wg.Wait()

	if got := task.callCount(); got != 2 {
		t.Errorf("Exec called %d times, want 2", got)
	}
	if got := r.Attempts(); got != 2 {
		t.Errorf("Attempts() = %d, want 2", got)
	}
	if r.Err() != nil {
		t.Errorf("Err() = %v, want nil after success", r.Err())
	}
}

func TestSuccessOnFirstAttempt(t *testing.T) {
	task := &stubTask{errs: []error{nil}}

	r, err := New(task, 5, 50)
	if err != nil {
		t.Fatalf("New() returned unexpected error: %v", err)
	}

	wg := &sync.WaitGroup{}
	r.Start(wg, nil)
	wg.Wait()

	if got := task.callCount(); got != 1 {
		t.Errorf("Exec called %d times, want 1", got)
	}
	if got := r.Attempts(); got != 1 {
		t.Errorf("Attempts() = %d, want 1", got)
	}
	if r.Err() != nil {
		t.Errorf("Err() = %v, want nil", r.Err())
	}
}
