package retrier_test

import (
	"errors"
	"sync"
	"testing"

	retrier "github.com/silverspell/goretrier"
)

type fakeTask struct {
	calls     int
	failUntil int
}

func (f *fakeTask) Exec() error {
	f.calls++
	if f.calls <= f.failUntil {
		return errors.New("transient")
	}
	return nil
}

func TestRetrier_SuccessOnFirstAttempt(t *testing.T) {
	task := &fakeTask{failUntil: 0}
	r, err := retrier.New(task, 3, 1)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var wg sync.WaitGroup
	r.Start(&wg, nil)
	wg.Wait()

	if task.calls != 1 {
		t.Fatalf("Exec() called %d times, want 1", task.calls)
	}
	if r.Attempts() != 1 {
		t.Fatalf("Attempts() = %d, want 1", r.Attempts())
	}
	if r.Err() != nil {
		t.Fatalf("Err() = %v, want nil", r.Err())
	}
}

func TestRetrier_StopsAfterFirstSuccess(t *testing.T) {
	task := &fakeTask{failUntil: 2}
	r, err := retrier.New(task, 5, 1)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var wg sync.WaitGroup
	r.Start(&wg, nil)
	wg.Wait()

	if task.calls != 3 {
		t.Fatalf("Exec() called %d times, want 3", task.calls)
	}
	if r.Attempts() != 3 {
		t.Fatalf("Attempts() = %d, want 3", r.Attempts())
	}
	if r.Err() != nil {
		t.Fatalf("Err() = %v, want nil", r.Err())
	}
}

func TestRetrier_PermanentFailureRespectsMaxAttempts(t *testing.T) {
	task := &fakeTask{failUntil: 100}
	r, err := retrier.New(task, 3, 1)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var wg sync.WaitGroup
	r.Start(&wg, nil)
	wg.Wait()

	if task.calls != 3 {
		t.Fatalf("Exec() called %d times, want 3", task.calls)
	}
	if r.Attempts() != 3 {
		t.Fatalf("Attempts() = %d, want 3", r.Attempts())
	}
	if r.Err() == nil {
		t.Fatal("Err() = nil, want non-nil")
	}
}
