package retrier

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

func enter(flag, max *int32) int32 {
	n := atomic.AddInt32(flag, 1)
	for {
		cur := atomic.LoadInt32(max)
		if n <= cur || atomic.CompareAndSwapInt32(max, cur, n) {
			return n
		}
	}
}

func leave(flag *int32) {
	atomic.AddInt32(flag, -1)
}

type blockingTask struct {
	inFlight    int32
	maxInFlight int32
	execCount   int32
	entered     chan struct{}
	release     chan struct{}
	once        sync.Once
}

func (t *blockingTask) Exec() error {
	enter(&t.inFlight, &t.maxInFlight)
	atomic.AddInt32(&t.execCount, 1)
	t.once.Do(func() {
		close(t.entered)
		<-t.release
	})
	leave(&t.inFlight)
	return nil
}

type barrierTask struct {
	inFlight    *int32
	maxInFlight *int32
	entered     chan struct{}
	release     chan struct{}
	once        sync.Once
}

func (t *barrierTask) Exec() error {
	enter(t.inFlight, t.maxInFlight)
	t.once.Do(func() {
		close(t.entered)
		<-t.release
	})
	leave(t.inFlight)
	return nil
}

type failingTask struct {
	execCount int32
}

func (f *failingTask) Exec() error {
	atomic.AddInt32(&f.execCount, 1)
	return errors.New("fail")
}

type runResult struct {
	attempts int
	err      error
}

func TestSameInstanceStartSerializes(t *testing.T) {
	task := &blockingTask{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	r, err := New(task, 1, 1)
	if err != nil {
		t.Fatal(err)
	}

	wg := &sync.WaitGroup{}
	r.Start(wg, nil)

	<-task.entered
	if got := atomic.LoadInt32(&task.execCount); got != 1 {
		t.Fatalf("first run Exec count = %d, want 1", got)
	}

	r.Start(wg, nil)

	if got := atomic.LoadInt32(&task.execCount); got != 1 {
		t.Fatalf("second run started before first completed: Exec count = %d, want 1", got)
	}
	if got := atomic.LoadInt32(&task.maxInFlight); got != 1 {
		t.Fatalf("Exec ran concurrently on same instance: maxInFlight = %d, want 1", got)
	}

	close(task.release)
	wg.Wait()

	if got := atomic.LoadInt32(&task.execCount); got != 2 {
		t.Fatalf("total Exec count = %d, want 2", got)
	}
	if got := atomic.LoadInt32(&task.maxInFlight); got != 1 {
		t.Fatalf("maxInFlight = %d, want 1", got)
	}
}

func TestSameInstanceFreshState(t *testing.T) {
	const maxAttempts = 3
	task := &failingTask{}
	r, err := New(task, maxAttempts, 1)
	if err != nil {
		t.Fatal(err)
	}

	wg := &sync.WaitGroup{}
	results := make(chan runResult, 2)
	cb := func(r *Retrier) {
		results <- runResult{attempts: r.Attempts(), err: r.Err()}
	}
	r.Start(wg, cb)
	r.Start(wg, cb)
	wg.Wait()
	close(results)

	if got := atomic.LoadInt32(&task.execCount); got != 2*maxAttempts {
		t.Fatalf("total Exec calls = %d, want %d", got, 2*maxAttempts)
	}

	n := 0
	for res := range results {
		n++
		if res.attempts != maxAttempts {
			t.Fatalf("run %d attempts = %d, want %d (state leaked between runs)", n, res.attempts, maxAttempts)
		}
		if res.err == nil {
			t.Fatalf("run %d err = nil, want non-nil", n)
		}
	}
	if n != 2 {
		t.Fatalf("callbacks = %d, want 2", n)
	}
}

func TestDifferentInstancesConcurrent(t *testing.T) {
	var inFlight, maxInFlight int32
	release := make(chan struct{})

	task1 := &barrierTask{
		inFlight:    &inFlight,
		maxInFlight: &maxInFlight,
		entered:     make(chan struct{}),
		release:     release,
	}
	task2 := &barrierTask{
		inFlight:    &inFlight,
		maxInFlight: &maxInFlight,
		entered:     make(chan struct{}),
		release:     release,
	}

	r1, err := New(task1, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := New(task2, 1, 1)
	if err != nil {
		t.Fatal(err)
	}

	wg := &sync.WaitGroup{}
	r1.Start(wg, nil)
	r2.Start(wg, nil)

	<-task1.entered
	<-task2.entered
	if got := atomic.LoadInt32(&maxInFlight); got != 2 {
		t.Fatalf("different instances did not run concurrently: maxInFlight = %d, want 2", got)
	}

	close(release)
	wg.Wait()
}
