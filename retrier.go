package retrier

import (
	"errors"
	"time"
)

type Retrieable interface {
	Exec() error
}

type Retrier struct {
	maxAttempts  int
	attempts     int
	waitDuration int
	done         bool
	item         Retrieable
}

func New(retriable Retrieable, maxAttempt, waitDuration int) (*Retrier, error) {
	if maxAttempt < 1 {
		return nil, errors.New("MaxAttempt must be > 0")
	}

	if waitDuration < 1 {
		return nil, errors.New("WaitDuration shall be > 1 msec")
	}

	if retriable == nil {
		return nil, errors.New("Instance is nil?")
	}

	return &Retrier{
		maxAttempts:  maxAttempt,
		waitDuration: waitDuration,
		done:         false,
		item:         retriable,
		attempts:     0,
	}, nil
}

func (r *Retrier) run() {
	duration := time.Duration(r.waitDuration) * time.Millisecond
	t := time.NewTimer(duration)
	for !r.isDone() {
		err := r.doWork()
		if err == nil {
			r.done = true
			continue
		}
		<-t.C
		t.Reset(duration)
	}
}

func (r *Retrier) doWork() error {
	r.attempts++
	return r.item.Exec()
}

func (r *Retrier) isDone() bool {
	return (r.attempts == r.maxAttempts) || r.done
}

func (r *Retrier) Start() {
	go r.run()
}
