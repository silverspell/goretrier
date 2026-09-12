package retrier

import "testing"

type stubRetriable struct{}

func (stubRetriable) Exec() error { return nil }

func TestNew_Validation(t *testing.T) {
	valid := stubRetriable{}

	tests := []struct {
		name         string
		retriable    Retrieable
		maxAttempt   int
		waitDuration int
		wantErr      bool
	}{
		{
			name:         "valid representative values",
			retriable:    valid,
			maxAttempt:   3,
			waitDuration: 1000,
			wantErr:      false,
		},
		{
			name:         "valid lower bound",
			retriable:    valid,
			maxAttempt:   1,
			waitDuration: 1,
			wantErr:      false,
		},
		{
			name:         "maxAttempt zero",
			retriable:    valid,
			maxAttempt:   0,
			waitDuration: 1000,
			wantErr:      true,
		},
		{
			name:         "maxAttempt negative",
			retriable:    valid,
			maxAttempt:   -1,
			waitDuration: 1000,
			wantErr:      true,
		},
		{
			name:         "waitDuration zero",
			retriable:    valid,
			maxAttempt:   3,
			waitDuration: 0,
			wantErr:      true,
		},
		{
			name:         "waitDuration negative",
			retriable:    valid,
			maxAttempt:   3,
			waitDuration: -1,
			wantErr:      true,
		},
		{
			name:         "nil retriable",
			retriable:    nil,
			maxAttempt:   3,
			waitDuration: 1000,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.retriable, tt.maxAttempt, tt.waitDuration)
			if (err != nil) != tt.wantErr {
				t.Fatalf("New() error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

func TestNew_Success(t *testing.T) {
	item := stubRetriable{}

	r, err := New(item, 3, 1000)
	if err != nil {
		t.Fatalf("New() unexpected error = %v", err)
	}
	if r == nil {
		t.Fatal("New() returned nil Retrier")
	}
	if r.maxAttempts != 3 {
		t.Errorf("maxAttempts = %d, want 3", r.maxAttempts)
	}
	if r.waitDuration != 1000 {
		t.Errorf("waitDuration = %d, want 1000", r.waitDuration)
	}
	if r.attempts != 0 {
		t.Errorf("attempts = %d, want 0", r.attempts)
	}
	if r.done {
		t.Error("done = true, want false")
	}
	if r.item == nil {
		t.Error("item = nil, want non-nil")
	}
}

func TestNew_NilRetriable(t *testing.T) {
	_, err := New(nil, 3, 1000)
	if err == nil {
		t.Fatal("New(nil) expected error, got nil")
	}
}
