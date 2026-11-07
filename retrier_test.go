package retrier

import (
	"errors"
	"math"
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

const testTimeout = 2 * time.Second

type signalTask struct {
	mu     sync.Mutex
	errs   []error
	calls  int
	called chan struct{}
}

func newSignalTask(errs ...error) *signalTask {
	return &signalTask{
		errs:   errs,
		called: make(chan struct{}, 64),
	}
}

func (s *signalTask) Exec() error {
	s.mu.Lock()
	idx := s.calls
	s.calls++
	s.mu.Unlock()

	select {
	case s.called <- struct{}{}:
	default:
	}

	if idx < len(s.errs) {
		return s.errs[idx]
	}
	if len(s.errs) > 0 {
		return s.errs[len(s.errs)-1]
	}
	return nil
}

func (s *signalTask) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *signalTask) waitForCall(t *testing.T) {
	t.Helper()
	select {
	case <-s.called:
	case <-time.After(testTimeout):
		t.Fatal("timed out waiting for Exec call")
	}
}

type blockingTask struct {
	mu      sync.Mutex
	calls   int
	started chan struct{}
	release chan error
}

func newBlockingTask() *blockingTask {
	return &blockingTask{
		started: make(chan struct{}, 16),
		release: make(chan error),
	}
}

func (b *blockingTask) Exec() error {
	b.mu.Lock()
	b.calls++
	b.mu.Unlock()

	b.started <- struct{}{}
	return <-b.release
}

func (b *blockingTask) callCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.calls
}

func (b *blockingTask) waitStarted(t *testing.T) {
	t.Helper()
	select {
	case <-b.started:
	case <-time.After(testTimeout):
		t.Fatal("timed out waiting for Exec to start")
	}
}

type funcTask struct {
	mu    sync.Mutex
	calls int
	fn    func(int) error
}

func (f *funcTask) Exec() error {
	f.mu.Lock()
	idx := f.calls
	f.calls++
	f.mu.Unlock()
	return f.fn(idx)
}

func (f *funcTask) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func waitGroupTimeout(t *testing.T, wg *sync.WaitGroup) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(testTimeout):
		t.Fatal("timed out waiting for WaitGroup")
	}
}

func TestStopBeforeStart(t *testing.T) {
	task := newSignalTask()

	r, err := New(task, 3, 10)
	if err != nil {
		t.Fatalf("New() returned unexpected error: %v", err)
	}

	r.Stop()
	r.Stop()

	if got := task.callCount(); got != 0 {
		t.Errorf("Exec called %d times, want 0", got)
	}
	if got := r.Attempts(); got != 0 {
		t.Errorf("Attempts() = %d, want 0", got)
	}
	if !errors.Is(r.Err(), ErrStopped) {
		t.Errorf("Err() = %v, want ErrStopped", r.Err())
	}
	if !r.Stopped() {
		t.Error("Stopped() = false, want true")
	}

	callbacks := 0
	wg := &sync.WaitGroup{}
	r.Start(wg, func(*Retrier) { callbacks++ })
	waitGroupTimeout(t, wg)

	if callbacks != 1 {
		t.Errorf("callback invoked %d times, want 1", callbacks)
	}
	if got := task.callCount(); got != 0 {
		t.Errorf("Exec called %d times after Start, want 0", got)
	}
	if got := r.Attempts(); got != 0 {
		t.Errorf("Attempts() = %d after Start, want 0", got)
	}
	if !errors.Is(r.Err(), ErrStopped) {
		t.Errorf("Err() = %v after Start, want ErrStopped", r.Err())
	}
	if !r.Stopped() {
		t.Error("Stopped() = false after Start, want true")
	}
}

func TestStopDuringWait(t *testing.T) {
	task := newSignalTask(errAlways)

	r, err := New(task, 5, 5000)
	if err != nil {
		t.Fatalf("New() returned unexpected error: %v", err)
	}

	wg := &sync.WaitGroup{}
	r.Start(wg, nil)
	task.waitForCall(t)
	r.Stop()
	waitGroupTimeout(t, wg)

	if got := task.callCount(); got != 1 {
		t.Errorf("Exec called %d times, want 1", got)
	}
	if got := r.Attempts(); got != 1 {
		t.Errorf("Attempts() = %d, want 1", got)
	}
	err = r.Err()
	if !errors.Is(err, ErrStopped) {
		t.Errorf("Err() = %v, want errors.Is ErrStopped", err)
	}
	if !errors.Is(err, errAlways) {
		t.Errorf("Err() = %v, want errors.Is last error", err)
	}
	if !r.Stopped() {
		t.Error("Stopped() = false, want true")
	}
}

func TestStopDuringNonFinalExecFailure(t *testing.T) {
	task := newBlockingTask()

	r, err := New(task, 3, 5000)
	if err != nil {
		t.Fatalf("New() returned unexpected error: %v", err)
	}

	wg := &sync.WaitGroup{}
	r.Start(wg, nil)
	task.waitStarted(t)
	r.Stop()
	task.release <- errAlways
	waitGroupTimeout(t, wg)

	if got := task.callCount(); got != 1 {
		t.Errorf("Exec called %d times, want 1", got)
	}
	if got := r.Attempts(); got != 1 {
		t.Errorf("Attempts() = %d, want 1", got)
	}
	err = r.Err()
	if !errors.Is(err, ErrStopped) {
		t.Errorf("Err() = %v, want errors.Is ErrStopped", err)
	}
	if !errors.Is(err, errAlways) {
		t.Errorf("Err() = %v, want errors.Is last error", err)
	}
	if !r.Stopped() {
		t.Error("Stopped() = false, want true")
	}
}

func TestStopDuringFinalExecFailure(t *testing.T) {
	task := newBlockingTask()

	r, err := New(task, 1, 5000)
	if err != nil {
		t.Fatalf("New() returned unexpected error: %v", err)
	}

	wg := &sync.WaitGroup{}
	r.Start(wg, nil)
	task.waitStarted(t)
	r.Stop()
	task.release <- errAlways
	waitGroupTimeout(t, wg)

	if got := task.callCount(); got != 1 {
		t.Errorf("Exec called %d times, want 1", got)
	}
	if got := r.Attempts(); got != 1 {
		t.Errorf("Attempts() = %d, want 1", got)
	}
	if got := r.Err(); got != errAlways {
		t.Errorf("Err() = %v, want raw last error %v", got, errAlways)
	}
	if errors.Is(r.Err(), ErrStopped) {
		t.Error("errors.Is(Err(), ErrStopped) = true, want false")
	}
	if r.Stopped() {
		t.Error("Stopped() = true, want false")
	}
}

func TestStopDuringExecSuccess(t *testing.T) {
	task := newBlockingTask()

	r, err := New(task, 3, 5000)
	if err != nil {
		t.Fatalf("New() returned unexpected error: %v", err)
	}

	wg := &sync.WaitGroup{}
	r.Start(wg, nil)
	task.waitStarted(t)
	r.Stop()
	task.release <- nil
	waitGroupTimeout(t, wg)

	if got := task.callCount(); got != 1 {
		t.Errorf("Exec called %d times, want 1", got)
	}
	if got := r.Attempts(); got != 1 {
		t.Errorf("Attempts() = %d, want 1", got)
	}
	if got := r.Err(); got != nil {
		t.Errorf("Err() = %v, want nil", got)
	}
	if r.Stopped() {
		t.Error("Stopped() = true, want false")
	}
}

func TestStopAfterCompletionIsNoop(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		task := newSignalTask()

		r, err := New(task, 3, 10)
		if err != nil {
			t.Fatalf("New() returned unexpected error: %v", err)
		}

		callbacks := 0
		wg := &sync.WaitGroup{}
		r.Start(wg, func(*Retrier) { callbacks++ })
		waitGroupTimeout(t, wg)

		errBefore := r.Err()
		stoppedBefore := r.Stopped()
		attemptsBefore := r.Attempts()
		callsBefore := task.callCount()

		r.Stop()
		r.Stop()

		if r.Err() != errBefore {
			t.Errorf("Err() changed after Stop: %v -> %v", errBefore, r.Err())
		}
		if r.Stopped() != stoppedBefore {
			t.Errorf("Stopped() changed after Stop: %v -> %v", stoppedBefore, r.Stopped())
		}
		if got := r.Attempts(); got != attemptsBefore {
			t.Errorf("Attempts() = %d after Stop, want %d", got, attemptsBefore)
		}
		if got := task.callCount(); got != callsBefore {
			t.Errorf("Exec called %d times after Stop, want %d", got, callsBefore)
		}
		if callbacks != 1 {
			t.Errorf("callback invoked %d times, want 1", callbacks)
		}
	})

	t.Run("exhausted failure", func(t *testing.T) {
		task := newSignalTask(errAlways)

		r, err := New(task, 2, 10)
		if err != nil {
			t.Fatalf("New() returned unexpected error: %v", err)
		}

		callbacks := 0
		wg := &sync.WaitGroup{}
		r.Start(wg, func(*Retrier) { callbacks++ })
		waitGroupTimeout(t, wg)

		errBefore := r.Err()
		if !errors.Is(errBefore, errAlways) {
			t.Fatalf("Err() = %v, want last error before Stop", errBefore)
		}
		if r.Stopped() {
			t.Fatal("Stopped() = true before Stop, want false")
		}
		attemptsBefore := r.Attempts()
		callsBefore := task.callCount()

		r.Stop()
		r.Stop()

		if !errors.Is(r.Err(), errAlways) {
			t.Errorf("Err() = %v after Stop, want last error", r.Err())
		}
		if errors.Is(r.Err(), ErrStopped) {
			t.Error("errors.Is(Err(), ErrStopped) = true after completed Stop, want false")
		}
		if r.Stopped() {
			t.Error("Stopped() = true after completed Stop, want false")
		}
		if got := r.Attempts(); got != attemptsBefore {
			t.Errorf("Attempts() = %d after Stop, want %d", got, attemptsBefore)
		}
		if got := task.callCount(); got != callsBefore {
			t.Errorf("Exec called %d times after Stop, want %d", got, callsBefore)
		}
		if callbacks != 1 {
			t.Errorf("callback invoked %d times, want 1", callbacks)
		}
	})
}

func TestDoubleStopIsSafe(t *testing.T) {
	r, err := New(newSignalTask(errAlways), 5, 5000)
	if err != nil {
		t.Fatalf("New() returned unexpected error: %v", err)
	}

	var stoppers sync.WaitGroup
	for i := 0; i < 16; i++ {
		stoppers.Add(1)
		go func() {
			defer stoppers.Done()
			r.Stop()
		}()
	}
	waitGroupTimeout(t, &stoppers)

	if got := r.Attempts(); got != 0 {
		t.Errorf("Attempts() = %d, want 0", got)
	}
	if !errors.Is(r.Err(), ErrStopped) {
		t.Errorf("Err() = %v, want ErrStopped", r.Err())
	}
	if !r.Stopped() {
		t.Error("Stopped() = false, want true")
	}
}

func TestStopFromInsideExec(t *testing.T) {
	var r *Retrier
	task := &funcTask{fn: func(int) error {
		r.Stop()
		return errAlways
	}}

	var err error
	r, err = New(task, 3, 5000)
	if err != nil {
		t.Fatalf("New() returned unexpected error: %v", err)
	}

	wg := &sync.WaitGroup{}
	r.Start(wg, nil)
	waitGroupTimeout(t, wg)

	if got := task.callCount(); got != 1 {
		t.Errorf("Exec called %d times, want 1", got)
	}
	err = r.Err()
	if !errors.Is(err, ErrStopped) {
		t.Errorf("Err() = %v, want errors.Is ErrStopped", err)
	}
	if !errors.Is(err, errAlways) {
		t.Errorf("Err() = %v, want errors.Is last error", err)
	}
	if !r.Stopped() {
		t.Error("Stopped() = false, want true")
	}
}

func TestStopFromInsideCallback(t *testing.T) {
	task := newSignalTask()

	r, err := New(task, 3, 5000)
	if err != nil {
		t.Fatalf("New() returned unexpected error: %v", err)
	}

	callbackDone := make(chan struct{})
	wg := &sync.WaitGroup{}
	r.Start(wg, func(rr *Retrier) {
		rr.Stop()
		close(callbackDone)
	})
	waitGroupTimeout(t, wg)

	select {
	case <-callbackDone:
	case <-time.After(testTimeout):
		t.Fatal("callback did not finish after calling Stop")
	}

	if got := task.callCount(); got != 1 {
		t.Errorf("Exec called %d times, want 1", got)
	}
	if got := r.Err(); got != nil {
		t.Errorf("Err() = %v, want nil", got)
	}
	if r.Stopped() {
		t.Error("Stopped() = true, want false")
	}
}

func TestStartNilArguments(t *testing.T) {
	t.Run("nil waitgroup and callback", func(t *testing.T) {
		executed := make(chan struct{})
		task := &funcTask{fn: func(int) error {
			close(executed)
			return nil
		}}

		r, err := New(task, 1, 10)
		if err != nil {
			t.Fatalf("New() returned unexpected error: %v", err)
		}

		r.Start(nil, nil)

		select {
		case <-executed:
		case <-time.After(testTimeout):
			t.Fatal("Exec was not called")
		}
	})

	t.Run("nil callback", func(t *testing.T) {
		task := newSignalTask()

		r, err := New(task, 1, 10)
		if err != nil {
			t.Fatalf("New() returned unexpected error: %v", err)
		}

		wg := &sync.WaitGroup{}
		r.Start(wg, nil)
		waitGroupTimeout(t, wg)

		if got := task.callCount(); got != 1 {
			t.Errorf("Exec called %d times, want 1", got)
		}
	})

	t.Run("nil waitgroup", func(t *testing.T) {
		task := newSignalTask()

		r, err := New(task, 1, 10)
		if err != nil {
			t.Fatalf("New() returned unexpected error: %v", err)
		}

		callbackDone := make(chan *Retrier, 1)
		r.Start(nil, func(rr *Retrier) {
			callbackDone <- rr
		})

		select {
		case got := <-callbackDone:
			if got != r {
				t.Error("callback received a different Retrier")
			}
		case <-time.After(testTimeout):
			t.Fatal("callback was not called")
		}
	})
}

func TestConcurrentStopAccessors(t *testing.T) {
	task := newSignalTask(errAlways)

	r, err := New(task, 1000, 5)
	if err != nil {
		t.Fatalf("New() returned unexpected error: %v", err)
	}

	wg := &sync.WaitGroup{}
	r.Start(wg, nil)

	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for j := 0; j < 500; j++ {
				_ = r.Stopped()
				_ = r.Err()
				_ = r.Attempts()
				_ = r.MaxAttempts()
				r.Stop()
			}
		}()
	}
	waitGroupTimeout(t, &workers)
	waitGroupTimeout(t, wg)

	if got := r.Attempts(); got > 1000 {
		t.Errorf("Attempts() = %d, want <= 1000", got)
	}
	if got := task.callCount(); got > 1000 {
		t.Errorf("Exec called %d times, want <= 1000", got)
	}
}

func TestBackoffWaitGrowth(t *testing.T) {
	t.Run("multiplier 2", func(t *testing.T) {
		want := []int{100, 200, 400, 800, 1600}
		for i, w := range want {
			attempt := i + 1
			if got := backoffWaitMillis(attempt, 100, 2, 10000); got != w {
				t.Errorf("backoffWaitMillis(%d, 100, 2, 10000) = %d, want %d", attempt, got, w)
			}
		}
	})

	t.Run("multiplier 1.5", func(t *testing.T) {
		want := []int{100, 150, 225}
		for i, w := range want {
			attempt := i + 1
			if got := backoffWaitMillis(attempt, 100, 1.5, 10000); got != w {
				t.Errorf("backoffWaitMillis(%d, 100, 1.5, 10000) = %d, want %d", attempt, got, w)
			}
		}
	})
}

func TestBackoffWaitUpperBound(t *testing.T) {
	const max = 1000
	for _, attempt := range []int{2, 3, 1000} {
		got := backoffWaitMillis(attempt, 100, 10, max)
		if got != max {
			t.Errorf("backoffWaitMillis(%d, 100, 10, %d) = %d, want %d", attempt, max, got, max)
		}
		if got > max {
			t.Errorf("backoffWaitMillis(%d, 100, 10, %d) = %d, want <= %d", attempt, max, got, max)
		}
	}
}

func TestBackoffWaitOverflowSafe(t *testing.T) {
	maxMillis := int(math.MaxInt64 / int64(time.Millisecond))

	cases := []struct {
		name       string
		attempt    int
		multiplier float64
		max        int
	}{
		{"huge multiplier", 1000000, math.MaxFloat64, maxMillis},
		{"multiplier 2", 1000000, 2, maxMillis},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := backoffWaitMillis(tc.attempt, 1, tc.multiplier, tc.max)
			if got < 0 {
				t.Errorf("backoffWaitMillis(...) = %d, want >= 0", got)
			}
			if got > tc.max {
				t.Errorf("backoffWaitMillis(...) = %d, want <= %d", got, tc.max)
			}
			if got != tc.max {
				t.Errorf("backoffWaitMillis(...) = %d, want %d", got, tc.max)
			}
		})
	}
}

func TestNewWithBackoffWaitIntervals(t *testing.T) {
	r, err := NewWithBackoff(&stubTask{}, 5, 100, 2, 1000)
	if err != nil {
		t.Fatalf("NewWithBackoff() returned unexpected error: %v", err)
	}

	want := []time.Duration{100, 200, 400, 800}
	for i, w := range want {
		attempt := i + 1
		if got := r.waitIntervalForAttempt(attempt); got != w*time.Millisecond {
			t.Errorf("waitIntervalForAttempt(%d) = %v, want %v", attempt, got, w*time.Millisecond)
		}
	}
	if got := r.waitIntervalForAttempt(10); got != 1000*time.Millisecond {
		t.Errorf("waitIntervalForAttempt(10) = %v, want %v", got, 1000*time.Millisecond)
	}
}

func TestNewWithBackoffNoWaitAfterFinalAttempt(t *testing.T) {
	task := &stubTask{errs: []error{errAlways}}

	r, err := NewWithBackoff(task, 1, 5000, 2, 5000)
	if err != nil {
		t.Fatalf("NewWithBackoff() returned unexpected error: %v", err)
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

func TestNewWithBackoffRejectsInvalidParams(t *testing.T) {
	maxMillis := int(math.MaxInt64 / int64(time.Millisecond))
	task := &stubTask{}

	cases := []struct {
		name       string
		retriable  Retrieable
		maxAttempt int
		wait       int
		multiplier float64
		maxWait    int
	}{
		{"multiplier 1", task, 3, 100, 1, 1000},
		{"multiplier 0", task, 3, 100, 0, 1000},
		{"multiplier -1", task, 3, 100, -1, 1000},
		{"multiplier NaN", task, 3, 100, math.NaN(), 1000},
		{"multiplier +Inf", task, 3, 100, math.Inf(1), 1000},
		{"multiplier -Inf", task, 3, 100, math.Inf(-1), 1000},
		{"maxWait less than wait", task, 3, 100, 2, 99},
		{"wait exceeds max millis", task, 3, maxMillis + 1, 2, maxMillis + 1},
		{"maxWait exceeds max millis", task, 3, 100, 2, maxMillis + 1},
		{"maxAttempt less than 1", task, 0, 100, 2, 1000},
		{"wait less than 1", task, 3, 0, 2, 1000},
		{"nil retriable", nil, 3, 100, 2, 1000},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, err := NewWithBackoff(tc.retriable, tc.maxAttempt, tc.wait, tc.multiplier, tc.maxWait)
			if err == nil {
				t.Fatalf("NewWithBackoff() error = nil, want non-nil")
			}
			if r != nil {
				t.Errorf("NewWithBackoff() retrier = %v, want nil", r)
			}
		})
	}
}

func TestNewWithBackoffBoundaries(t *testing.T) {
	maxMillis := int(math.MaxInt64 / int64(time.Millisecond))
	task := &stubTask{}

	r, err := NewWithBackoff(task, 3, 100, math.Nextafter(1, 2), 100)
	if err != nil {
		t.Fatalf("NewWithBackoff() with multiplier just above 1 returned unexpected error: %v", err)
	}
	if !r.backoff {
		t.Error("backoff = false, want true")
	}
	if r.multiplier != math.Nextafter(1, 2) {
		t.Errorf("multiplier = %v, want %v", r.multiplier, math.Nextafter(1, 2))
	}
	if r.maxWaitDuration != 100 {
		t.Errorf("maxWaitDuration = %d, want 100", r.maxWaitDuration)
	}

	r2, err := NewWithBackoff(task, 3, maxMillis, 2, maxMillis)
	if err != nil {
		t.Fatalf("NewWithBackoff() at max millis boundary returned unexpected error: %v", err)
	}
	if !r2.backoff {
		t.Error("backoff = false, want true")
	}
	if r2.waitDuration != maxMillis {
		t.Errorf("waitDuration = %d, want %d", r2.waitDuration, maxMillis)
	}
	if r2.maxWaitDuration != maxMillis {
		t.Errorf("maxWaitDuration = %d, want %d", r2.maxWaitDuration, maxMillis)
	}
	if got, want := r2.waitIntervalForAttempt(1000000), time.Duration(maxMillis)*time.Millisecond; got != want {
		t.Errorf("waitIntervalForAttempt(1000000) = %v, want %v", got, want)
	}
}

func TestStopDuringBackoffWait(t *testing.T) {
	task := newSignalTask(errAlways)

	r, err := NewWithBackoff(task, 5, 5000, 2, 5000)
	if err != nil {
		t.Fatalf("NewWithBackoff() returned unexpected error: %v", err)
	}

	wg := &sync.WaitGroup{}
	r.Start(wg, nil)
	task.waitForCall(t)
	r.Stop()
	waitGroupTimeout(t, wg)

	if got := task.callCount(); got != 1 {
		t.Errorf("Exec called %d times, want 1", got)
	}
	if got := r.Attempts(); got != 1 {
		t.Errorf("Attempts() = %d, want 1", got)
	}
	err = r.Err()
	if !errors.Is(err, ErrStopped) {
		t.Errorf("Err() = %v, want errors.Is ErrStopped", err)
	}
	if !errors.Is(err, errAlways) {
		t.Errorf("Err() = %v, want errors.Is last error", err)
	}
	if !r.Stopped() {
		t.Error("Stopped() = false, want true")
	}
}

func TestNewFixedWaitUnchanged(t *testing.T) {
	r, err := New(&stubTask{}, 3, 50)
	if err != nil {
		t.Fatalf("New() returned unexpected error: %v", err)
	}
	if r.backoff {
		t.Error("backoff = true, want false for New")
	}
	if got := r.waitIntervalForAttempt(1); got != 50*time.Millisecond {
		t.Errorf("waitIntervalForAttempt(1) = %v, want 50ms", got)
	}
	if got := r.waitIntervalForAttempt(99); got != 50*time.Millisecond {
		t.Errorf("waitIntervalForAttempt(99) = %v, want 50ms", got)
	}
}
