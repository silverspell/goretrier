# AGENTS.md

Guidance for human contributors and LoopFlow/OpenCode agents working on this
repository. For the human contributor workflow (setup, branch, format,
validate, PR), see [contributing.md](./contributing.md).

## Repository structure

- `retrier.go` — the entire public package: `package retrier`.
- `examples/main.go` — a runnable example of the public API.
- `go.mod` — module `github.com/silverspell/goretrier`, Go 1.16.

## Public API (verified from `retrier.go`)

- `type Retrieable interface { Exec() error }` — note the exported name is
  `Retrieable` (not "Retriable").
- `func New(retriable Retrieable, maxAttempt, waitDuration int) (*Retrier, error)`
  — returns an error when `maxAttempt < 1`, `waitDuration < 1`, or `retriable`
  is `nil`.
- `func (r *Retrier) Start(wg *sync.WaitGroup, callback Callback)` — runs
  asynchronously; increments `wg` if non-nil, runs the retry loop, then calls
  `callback` and finally `wg.Done()`.
- `func (r *Retrier) Err() error` — last error from the retried work.
- `func (r *Retrier) Attempts() int` — number of attempts performed.
- `type Callback func(*Retrier)`.

Correct usage (see `examples/main.go`):

```go
r, err := retrier.New(task, 3, 1000)
if err != nil {
    panic(err)
}
wg := &sync.WaitGroup{}
r.Start(wg, func(r *retrier.Retrier) { /* ... */ })
wg.Wait()
```

The `README.md` examples are stale (they call `r.Start()` with no arguments and
rely on `time.Sleep`). Do not treat the README as the source of truth for the
API; verify against `retrier.go` and `examples/main.go`.

## Working rules

- Keep changes small and scoped to the task; do not touch unrelated files.
- Reuse existing patterns and helpers; do not introduce new abstractions.
- Avoid unrelated refactors, formatting-only changes, or dependency changes.
- Do not commit secrets or credentials.
- Report results with real verification evidence (command output), not
  assumptions.
- Do not mandate future architecture or tools that are not present in this
  repository.

## LoopFlow / OpenCode agent rules

These rules apply to automated agents and are separate from the human
contribution flow in [contributing.md](./contributing.md).

- Before starting IMPLEMENTATION or FIX, verify the current task branch and
  confirm it is not a protected branch. Do not make changes on
  `main`, `master`, `develop`, `dev`, `release*`, or `hotfix*`.
- The pipeline prepares the task branch; the agent does not create branches.
- During IMPLEMENTATION/FIX the agent does not commit, push, or open a PR.
  Those steps belong to the SUBMIT stage.
- Keep each task's diff limited to its own scope; unrelated edits make review
  and PR isolation harder.

## Validation

Validation runs inside the devcontainer (see `.loopflow.yml` and
`.devcontainer/devcontainer.json`):

- `go test ./...`
- `go vet ./...`
- `go build ./...`

This repository currently contains no `*_test.go` files, so `go test ./...`
reports `[no test files]`; that is not test coverage or behavioural test
success.

## LoopFlow

### Build and test

The canonical commands are defined in `.loopflow.yml` and run inside the
LoopFlow devcontainer (`.devcontainer/loopflow/devcontainer.json`):

```bash
go test ./...
go vet ./...
go build ./...
```

### Coding conventions

- Go 1.16 module `github.com/silverspell/goretrier`; the public package lives
  in `retrier.go` as `package retrier`.
- Keep exported API names exactly as they exist, including the intentionally
  spelled `Retrieable` interface and `Callback` type.
- Use `gofmt` formatting, small scoped functions, and standard-library
  concurrency primitives (`sync`, `time`) as already used in `retrier.go`.
- Document exported functions with Go doc comments, matching the existing
  `New` and `Start` comments.
- Runnable usage examples belong in `examples/main.go`.

### Invariants

_This section is a draft extracted from the code; violating an invariant needs
human review._

- `New` rejects `maxAttempt < 1`, `waitDuration < 1`, and a `nil` retriable.
- A `Retrier` performs at most `maxAttempts` calls to `Retrieable.Exec()`.
- `Start` increments a non-nil `WaitGroup`, invokes the callback after the
  retry loop, and always calls `wg.Done()`.
- `Err()` returns the last error from the retried work; a `nil` error means the
  work succeeded.

### ADRs

No ADR directory or ADR files are present in this repository.

