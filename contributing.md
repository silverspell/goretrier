# Contributing

Short contributor guide for the `goretrier` module
(`github.com/silverspell/goretrier`). For repository structure, the public API,
and agent working rules, see [AGENTS.md](./AGENTS.md).

## Setup

```bash
git clone https://github.com/silverspell/goretrier.git
cd goretrier
```

The module requires Go 1.16 or newer. The validation environment is a
devcontainer; see `.devcontainer/devcontainer.json`.

## Making a change

1. Open a task branch off `main`:

   ```bash
   git checkout -b feature/my-change
   ```

2. Make your change using the existing package in `retrier.go`.

3. Format the code:

   ```bash
   gofmt -w retrier.go
   ```

4. Validate:

   ```bash
   go test ./...
   go vet ./...
   go build ./...
   ```

   Alternatively, run the validation commands inside the devcontainer:

   ```bash
   devcontainer up --workspace-folder .
   devcontainer exec --workspace-folder . go test ./...
   devcontainer exec --workspace-folder . go vet ./...
   devcontainer exec --workspace-folder . go build ./...
   ```

5. Commit and open a pull request:

   ```bash
   git add retrier.go
   git commit -m "Describe your change"
   git push -u origin feature/my-change
   ```

   Then open a PR on GitHub.

## Notes

- This repository has no tests yet. `go test ./...` prints `[no test files]`;
  it does not indicate passing behavioural tests or coverage.
- If your change alters the public API (for example `New`, `Start`, `Retrieable`,
  or `Callback`), describe the behaviour change and include the relevant tests
  in your PR.
- Keep changes scoped to the task; avoid unrelated reformatting or dependency
  changes.
