package retrier

import (
	"errors"
	"sync"
	"testing"
)

type countingTask struct {
	mu    sync.Mutex
	calls int
	fail  bool
}

func (c *countingTask) Exec() error {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	if c.fail {
		return errors.New("boom")
	}
	return nil
}

func (c *countingTask) Calls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func TestConcurrentRetriersIndependent(t *testing.T) {
	taskA := &countingTask{fail: true}
	taskB := &countingTask{}

	rA, err := New(taskA, 3, 1)
	if err != nil {
		t.Fatal(err)
	}
	rB, err := New(taskB, 3, 1)
	if err != nil {
		t.Fatal(err)
	}

	rA.Start(nil, nil)
	rB.Start(nil, nil)

	<-rA.Done()
	<-rB.Done()

	if rA.Attempts() != 3 {
		t.Fatalf("rA attempts = %d, want 3", rA.Attempts())
	}
	if rA.Err() == nil {
		t.Fatal("rA expected an error after exhausting attempts")
	}
	if rB.Attempts() != 1 {
		t.Fatalf("rB attempts = %d, want 1", rB.Attempts())
	}
	if rB.Err() != nil {
		t.Fatalf("rB unexpected error: %v", rB.Err())
	}

	if taskA.Calls() != 3 {
		t.Fatalf("taskA calls = %d, want 3", taskA.Calls())
	}
	if taskB.Calls() != 1 {
		t.Fatalf("taskB calls = %d, want 1", taskB.Calls())
	}
}
