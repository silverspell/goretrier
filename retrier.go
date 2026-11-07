package retrier

import (
	"errors"
	"math"
	"sync"
	"time"
)

// ErrStopped is returned when a Retrier is stopped before its work has either
// succeeded or exhausted all of its attempts. The last error from the retried
// work, if any, can be reached through errors.Is / errors.Unwrap.
var ErrStopped = errors.New("retrier stopped")

type Retrieable interface {
	Exec() error
}

// stoppedError wraps the last error from the retried work while reporting that
// the overall result was cancelled via ErrStopped.
type stoppedError struct {
	lastErr error
}

func (e *stoppedError) Error() string {
	if e.lastErr == nil {
		return ErrStopped.Error()
	}
	return ErrStopped.Error() + ": " + e.lastErr.Error()
}

func (e *stoppedError) Unwrap() error {
	return e.lastErr
}

func (e *stoppedError) Is(target error) bool {
	return target == ErrStopped
}

type Retrier struct {
	mu              sync.Mutex
	maxAttempts     int
	attempts        int
	waitDuration    int
	multiplier      float64
	maxWaitDuration int
	backoff         bool
	done            bool
	err             error
	item            Retrieable
	lastErr         error
	stopCh          chan struct{}
	stopRequested   bool
	stopped         bool
	started         bool
}

// New function returns a new pointer to a Retrier struct.
// Params:
// retriable is an instance that implements the Retriable (a.k.a. has Exec() error function) which must not be nil
// maxAttempt is an integer that states how many retries will be made.
// waitDuration is an integer that states the duration in milliseconds between retries.
// Returns a pointer to a new Retrier instances or Error.
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
		stopCh:       make(chan struct{}),
	}, nil
}

func (r *Retrier) waitInterval() time.Duration {
	return time.Duration(r.waitDuration) * time.Millisecond
}

// NewWithBackoff returns a Retrier that grows the wait between attempts using
// exponential backoff. The wait before attempt n (1-based) is
// waitDuration * multiplier^(n-1) milliseconds, capped at maxWaitDuration.
// There is no jitter. NewWithBackoff returns an error when maxAttempt < 1,
// waitDuration < 1, retriable is nil, multiplier is NaN, infinite or <= 1,
// maxWaitDuration < waitDuration, or either duration cannot be represented as
// a time.Duration in milliseconds.
func NewWithBackoff(retriable Retrieable, maxAttempt, waitDuration int, multiplier float64, maxWaitDuration int) (*Retrier, error) {
	if maxAttempt < 1 {
		return nil, errors.New("MaxAttempt must be > 0")
	}

	if waitDuration < 1 {
		return nil, errors.New("WaitDuration shall be > 1 msec")
	}

	if retriable == nil {
		return nil, errors.New("Instance is nil?")
	}

	if math.IsNaN(multiplier) || math.IsInf(multiplier, 0) || multiplier <= 1 {
		return nil, errors.New("Multiplier must be a finite number > 1")
	}

	if maxWaitDuration < waitDuration {
		return nil, errors.New("MaxWaitDuration shall be >= WaitDuration")
	}

	maxMillis := int64(math.MaxInt64) / int64(time.Millisecond)
	if int64(waitDuration) > maxMillis || int64(maxWaitDuration) > maxMillis {
		return nil, errors.New("WaitDuration and MaxWaitDuration shall be representable as time.Duration")
	}

	return &Retrier{
		maxAttempts:     maxAttempt,
		waitDuration:    waitDuration,
		multiplier:      multiplier,
		maxWaitDuration: maxWaitDuration,
		backoff:         true,
		done:            false,
		item:            retriable,
		attempts:        0,
		stopCh:          make(chan struct{}),
	}, nil
}

func backoffWaitMillis(attempt int, waitDuration int, multiplier float64, maxWaitDuration int) int {
	w := float64(waitDuration)
	for i := 1; i < attempt; i++ {
		w *= multiplier
		if w >= float64(maxWaitDuration) {
			return maxWaitDuration
		}
	}
	return int(w)
}

func (r *Retrier) waitIntervalForAttempt(attempt int) time.Duration {
	if !r.backoff {
		return r.waitInterval()
	}
	ms := backoffWaitMillis(attempt, r.waitDuration, r.multiplier, r.maxWaitDuration)
	return time.Duration(ms) * time.Millisecond
}

func (r *Retrier) run() {
	r.mu.Lock()
	if r.done || r.stopped {
		r.started = true
		r.done = true
		r.mu.Unlock()
		return
	}
	r.started = true
	r.mu.Unlock()

	for {
		r.mu.Lock()
		if r.done || r.stopped {
			r.mu.Unlock()
			return
		}
		if r.stopRequested {
			r.finalizeStoppedLocked()
			r.mu.Unlock()
			return
		}
		if r.attempts >= r.maxAttempts {
			r.done = true
			r.mu.Unlock()
			return
		}
		r.attempts++
		attempt := r.attempts
		r.mu.Unlock()

		err := r.item.Exec()

		r.mu.Lock()
		if err == nil {
			r.err = nil
			r.lastErr = nil
			r.stopped = false
			r.done = true
			r.mu.Unlock()
			return
		}

		r.lastErr = err
		r.err = err

		if attempt >= r.maxAttempts {
			r.done = true
			r.stopped = false
			r.mu.Unlock()
			return
		}

		if r.stopRequested {
			r.finalizeStoppedLocked()
			r.mu.Unlock()
			return
		}
		r.mu.Unlock()

		timer := time.NewTimer(r.waitIntervalForAttempt(attempt))
		select {
		case <-r.stopCh:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			r.mu.Lock()
			r.finalizeStoppedLocked()
			r.mu.Unlock()
			return
		case <-timer.C:
		}
	}
}

func (r *Retrier) finalizeStoppedLocked() {
	r.done = true
	r.stopped = true
	if r.lastErr != nil {
		r.err = &stoppedError{lastErr: r.lastErr}
		return
	}
	r.err = ErrStopped
}

// Stop requests cancellation of the retry loop. It is safe to call from any
// goroutine, is idempotent and does not block. Once Stop has been called no new
// Exec is started and an in-flight wait is interrupted, but a running Exec is
// not interrupted.
func (r *Retrier) Stop() {
	r.mu.Lock()
	if r.done || r.stopRequested {
		r.mu.Unlock()
		return
	}
	r.stopRequested = true
	if !r.started {
		r.done = true
		r.stopped = true
		r.err = ErrStopped
	}
	r.mu.Unlock()

	close(r.stopCh)
}

// Stopped reports whether the Retrier's result was cancelled by Stop.
func (r *Retrier) Stopped() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stopped
}

// Start is the function that is responsible for starting.
func (r *Retrier) Start(wg *sync.WaitGroup, callback Callback) {
	if wg != nil {
		wg.Add(1)
	}
	go func() {
		r.run()
		if callback != nil {
			callback(r)
		}
		if wg != nil {
			wg.Done()
		}
	}()
}

func (r *Retrier) Err() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err
}

func (r *Retrier) Attempts() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.attempts
}

func (r *Retrier) MaxAttempts() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.maxAttempts
}

type Callback func(*Retrier)
