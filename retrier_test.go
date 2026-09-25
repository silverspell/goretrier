package retrier

import "testing"

type stubTask struct {
	err error
}

func (s stubTask) Exec() error {
	return s.err
}

func TestMaxAttempts(t *testing.T) {
	r, err := New(stubTask{}, 3, 1000)
	if err != nil {
		t.Fatalf("New() returned unexpected error: %v", err)
	}

	if got := r.MaxAttempts(); got != 3 {
		t.Fatalf("MaxAttempts() = %d, want 3", got)
	}
}
