## GoRetrier - A simple retry module in Golang.

### Installation
```bash
go get github.com/silverspell/goretrier
```

### How-to

Create a struct that implements the Retrieable interface (an Exec() error method). Then create a Retrier with New() and call its Start() method.

```go
package main

import (
	"errors"
	"fmt"
	"sync"

	retrier "github.com/silverspell/goretrier"
)

type MyTask struct {
	myTitle string
}

func (m *MyTask) Exec() error {
	if m.myTitle == "AAA" {
		fmt.Println("Error. myTitle should not be AAA")
		return errors.New("Unknown error")
	}
	fmt.Printf("My Title:%s\n", m.myTitle)
	return nil
}

func main() {
	t := &MyTask{
		myTitle: "BBB",
	}

	t2 := &MyTask{
		myTitle: "AAA",
	}
    
    // t is the Retriable interface, 3 is the maximum attempts, 1000 is the milliseconds delay between attempts.
	r, err := retrier.New(t, 3, 1000)
	if err != nil {
		panic(err)
	}
    // t2 is the Retriable interface, 5 is the maximum attempts, 500 is the milliseconds delay between attempts.
	r2, err := retrier.New(t2, 5, 500)
	if err != nil {
		panic(err)
	}

	wg := &sync.WaitGroup{}

	r.Start(wg, func(r *retrier.Retrier) {
		err := r.Err()
		switch {
		case err == nil:
			fmt.Println("r task is done successfully")
		case errors.Is(err, retrier.ErrStopped):
			fmt.Printf("r task was stopped after %d attempts. error: %s\n", r.Attempts(), err)
		default:
			fmt.Printf("r task is failed after %d attempts. error: %s\n", r.Attempts(), err)
		}
	})
	r2.Start(wg, func(r *retrier.Retrier) {
		err := r.Err()
		switch {
		case err == nil:
			fmt.Println("r2 task is done successfully")
		case errors.Is(err, retrier.ErrStopped):
			fmt.Printf("r2 task was stopped after %d attempts. error: %s\n", r.Attempts(), err)
		default:
			fmt.Printf("r2 task is failed after %d attempts. error: %s\n", r.Attempts(), err)
		}
	})
	wg.Wait()
}
```

`Err()` returns the last error from the retried work, or `nil` when the work succeeded. `Attempts()` returns the number of times `Exec()` was called. `MaxAttempts()` returns the configured maximum number of attempts passed to `New`. `Stopped()` reports whether the result was cancelled by `Stop()`.

The retrier waits only between attempts; it performs no wait after the final failed attempt.

All durations passed to `New` and `NewWithBackoff` are in milliseconds.

### Cancellation

Call `Stop()` to cancel a retry loop. `Stop()` is safe to call from any goroutine, is idempotent, and does not block. After `Stop()` no new `Exec()` call is started and an in-flight wait between attempts is interrupted. A running `Exec()` is **not** interrupted; `Stop()` only takes effect between attempts.

`Stopped()` reports whether the retrier's result was cancelled by `Stop()`. When the retrier is cancelled before the work has succeeded or exhausted its attempts, `Err()` is `ErrStopped`. If a work attempt had already failed before the cancellation, `Err()` wraps that last work error: `errors.Is(err, retrier.ErrStopped)` is still `true`, and `errors.Unwrap(err)` (or `errors.Is(err, lastWorkErr)`) reaches the last work error.

`Stop()` does not convert a completed run into a cancellation. If the final attempt already failed, the work succeeded, or the retrier already finished, `Stop()` leaves `Err()` and `Stopped()` unchanged. Calling `Stop()` before `Start()` makes `Err()` return `ErrStopped` and `Stopped()` return `true`; a later `Start()` runs no attempt but still invokes the callback and releases the wait group.

`Start(wg, callback)` calls `wg.Add(1)` when `wg` is non-nil, always invokes `callback` exactly once after the retry loop (including when the run was cancelled), and then always calls `wg.Done()`. Because `Stop()` cannot interrupt a running `Exec()`, `wg.Wait()` waits for the callback and for any in-flight `Exec()` to return, and may therefore block until that `Exec()` finishes.

### Exponential backoff

Use `NewWithBackoff` to grow the wait between attempts exponentially:

```go
func NewWithBackoff(retriable Retrieable, maxAttempt, waitDuration int, multiplier float64, maxWaitDuration int) (*Retrier, error)
```

All duration parameters are in milliseconds. The wait before attempt `n` (1-based) is `waitDuration * multiplier^(n-1)` milliseconds, capped at `maxWaitDuration`. There is no jitter.

`NewWithBackoff` returns an error when `maxAttempt < 1`, `waitDuration < 1`, `retriable` is `nil`, `multiplier` is NaN, infinite, or `<= 1`, `maxWaitDuration < waitDuration`, or either duration cannot be represented as a `time.Duration` in milliseconds.

A `maxWaitDuration` of `30000` (30 seconds) is a recommended upper bound, not an API default and not an absolute cap or clamp. Nothing enforces or clamps the wait to 30 seconds; choose the value that fits your workload.

LoopFlow supports policy-controlled autonomous merge for eligible changes.
