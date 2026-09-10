package retrier

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var (
	errFirst  = errors.New("first failure")
	errSecond = errors.New("second failure")
	errThird  = errors.New("third failure")
)

// fakeTask is a test double implementing Retrieable. It returns a
// predetermined sequence of results, one per Exec() call, and counts calls.
// A call beyond the end of the sequence returns a sentinel error so that an
// unexpected extra Exec() can be detected via the call counter.
type fakeTask struct {
	results []error
	calls   int32
}

func (f *fakeTask) Exec() error {
	n := atomic.AddInt32(&f.calls, 1)
	idx := int(n) - 1
	if idx >= len(f.results) {
		return errors.New("unexpected Exec() call")
	}
	return f.results[idx]
}

func (f *fakeTask) callsCount() int32 {
	return atomic.LoadInt32(&f.calls)
}

const testTimeout = 5 * time.Second

// waitWG waits for wg with a bounded timeout so a broken implementation
// cannot hang the test forever.
func waitWG(t *testing.T, wg *sync.WaitGroup) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(testTimeout):
		t.Fatal("WaitGroup.Wait() timed out")
	}
}

func TestNew_Validation(t *testing.T) {
	tests := []struct {
		name         string
		retriable    Retrieable
		maxAttempt   int
		waitDuration int
		wantErr      bool
	}{
		{"nil retriable", nil, 3, 1, true},
		{"zero maxAttempt", &fakeTask{}, 0, 1, true},
		{"negative maxAttempt", &fakeTask{}, -1, 1, true},
		{"zero waitDuration", &fakeTask{}, 3, 0, true},
		{"negative waitDuration", &fakeTask{}, 3, -1, true},
		{"valid input", &fakeTask{}, 3, 1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := New(tt.retriable, tt.maxAttempt, tt.waitDuration)
			if tt.wantErr {
				if err == nil {
					t.Fatal("New() returned nil error, want error")
				}
				if r != nil {
					t.Fatalf("New() returned non-nil *Retrier %v, want nil", r)
				}
				return
			}
			if err != nil {
				t.Fatalf("New() returned error %v, want nil", err)
			}
			if r == nil {
				t.Fatal("New() returned nil *Retrier, want non-nil")
			}
		})
	}
}

func TestRetry_SuccessOnFirstAttempt(t *testing.T) {
	task := &fakeTask{results: []error{nil}}
	r, err := New(task, 3, 1)
	if err != nil {
		t.Fatal(err)
	}
	wg := &sync.WaitGroup{}
	r.Start(wg, nil)
	waitWG(t, wg)

	if got := r.Attempts(); got != 1 {
		t.Fatalf("Attempts() = %d, want 1", got)
	}
	if r.Err() != nil {
		t.Fatalf("Err() = %v, want nil", r.Err())
	}
	if got := task.callsCount(); got != 1 {
		t.Fatalf("Exec() calls = %d, want 1", got)
	}
}

func TestRetry_SuccessAfterFailures(t *testing.T) {
	task := &fakeTask{results: []error{errFirst, errSecond, nil}}
	r, err := New(task, 5, 1)
	if err != nil {
		t.Fatal(err)
	}
	wg := &sync.WaitGroup{}
	r.Start(wg, nil)
	waitWG(t, wg)

	if got := r.Attempts(); got != 3 {
		t.Fatalf("Attempts() = %d, want 3", got)
	}
	if r.Err() != nil {
		t.Fatalf("Err() = %v, want nil", r.Err())
	}
	if got := task.callsCount(); got != 3 {
		t.Fatalf("Exec() calls = %d, want 3 (no extra calls after success)", got)
	}
}

func TestRetry_AllAttemptsFail(t *testing.T) {
	task := &fakeTask{results: []error{errFirst, errSecond, errThird}}
	r, err := New(task, 3, 1)
	if err != nil {
		t.Fatal(err)
	}
	wg := &sync.WaitGroup{}
	r.Start(wg, nil)
	waitWG(t, wg)

	if got := r.Attempts(); got != 3 {
		t.Fatalf("Attempts() = %d, want 3", got)
	}
	if r.Err() != errThird {
		t.Fatalf("Err() = %v, want %v (last error)", r.Err(), errThird)
	}
	if got := task.callsCount(); got != 3 {
		t.Fatalf("Exec() calls = %d, want 3", got)
	}
}

func TestStart_CallbackCalledOnce(t *testing.T) {
	task := &fakeTask{results: []error{nil}}
	r, err := New(task, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	var callbackCalls int32
	callbackDone := make(chan struct{})
	wg := &sync.WaitGroup{}
	r.Start(wg, func(*Retrier) {
		atomic.AddInt32(&callbackCalls, 1)
		close(callbackDone)
	})

	select {
	case <-callbackDone:
	case <-time.After(testTimeout):
		t.Fatal("callback was not called within timeout")
	}
	waitWG(t, wg)

	if got := atomic.LoadInt32(&callbackCalls); got != 1 {
		t.Fatalf("callback called %d times, want 1", got)
	}
}

func TestStart_NilCallback(t *testing.T) {
	task := &fakeTask{results: []error{nil}}
	r, err := New(task, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	wg := &sync.WaitGroup{}
	r.Start(wg, nil)
	waitWG(t, wg)

	if r.Err() != nil {
		t.Fatalf("Err() = %v, want nil", r.Err())
	}
}

func TestStart_NilWaitGroup(t *testing.T) {
	task := &fakeTask{results: []error{nil}}
	r, err := New(task, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	r.Start(nil, func(*Retrier) {
		close(done)
	})

	select {
	case <-done:
	case <-time.After(testTimeout):
		t.Fatal("Start with nil WaitGroup did not complete within timeout")
	}

	if r.Err() != nil {
		t.Fatalf("Err() = %v, want nil", r.Err())
	}
	if got := r.Attempts(); got != 1 {
		t.Fatalf("Attempts() = %d, want 1", got)
	}
}
