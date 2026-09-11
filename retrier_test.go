package retrier

import (
	"errors"
	"testing"
	"time"
)

const safetyTimeout = 5 * time.Second

type taskStub struct {
	exec func() error
}

func (t *taskStub) Exec() error {
	if t.exec == nil {
		return nil
	}
	return t.exec()
}

func alwaysSucceed() *taskStub {
	return &taskStub{}
}

func alwaysFail() *taskStub {
	return &taskStub{exec: func() error { return errors.New("boom") }}
}

func waitDone(t *testing.T, r *Retrier) {
	t.Helper()
	select {
	case <-r.Done():
	case <-time.After(safetyTimeout):
		t.Fatal("retrier did not complete within safety timeout")
	}
}

func TestRetrierCompletionDeterministic(t *testing.T) {
	r, err := New(alwaysSucceed(), 3, 1)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	r.Start(nil, nil)
	waitDone(t, r)
	if r.Err() != nil {
		t.Fatalf("Err() = %v, want nil", r.Err())
	}
	if r.Attempts() != 1 {
		t.Fatalf("Attempts() = %d, want 1", r.Attempts())
	}
}

func TestRetrierFailureExhaustsAttempts(t *testing.T) {
	r, err := New(alwaysFail(), 3, 1)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	r.Start(nil, nil)
	waitDone(t, r)
	if r.Err() == nil {
		t.Fatal("Err() = nil, want error")
	}
	if r.Attempts() != 3 {
		t.Fatalf("Attempts() = %d, want 3", r.Attempts())
	}
}

func TestRetrierTwoInstancesConcurrent(t *testing.T) {
	gate := make(chan struct{})
	entered := make(chan string, 2)
	release := make(chan struct{})

	newBlocking := func(name string) *taskStub {
		return &taskStub{exec: func() error {
			entered <- name
			<-gate
			return nil
		}}
	}

	r1, err := New(newBlocking("one"), 3, 1)
	if err != nil {
		t.Fatalf("New(r1) error = %v", err)
	}
	r2, err := New(newBlocking("two"), 3, 1)
	if err != nil {
		t.Fatalf("New(r2) error = %v", err)
	}

	go r1.Start(nil, nil)
	go r2.Start(nil, nil)

	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		select {
		case name := <-entered:
			seen[name] = true
		case <-release:
			t.Fatal("retrier completed before both instances entered Exec")
		}
	}
	if !seen["one"] || !seen["two"] {
		t.Fatalf("both instances must execute concurrently, seen = %v", seen)
	}
	close(gate)

	waitDone(t, r1)
	waitDone(t, r2)
}

func TestDoneChannelClosesOnce(t *testing.T) {
	r, err := New(alwaysSucceed(), 2, 1)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	r.Start(nil, nil)
	waitDone(t, r)
	select {
	case <-r.Done():
	default:
		t.Fatal("Done() channel should remain closed after completion")
	}
}
