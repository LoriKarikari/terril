# wolkpack

wolkpack is an open-source CLI that builds a container image from a Node, Python or Go app, or a static site. It reads the app's manifests, writes a Dockerfile, and runs `docker build`. `wolkpack plan` prints that Dockerfile.

## Code

- `pack/facts.go` reads and parses every manifest, lockfile and `wolkpack.toml`. Nothing else reads app files.
- `pack/pack.go` picks the providers and their roles. The primary provider owns the runtime image. Node can also build assets for a Python or Go app.
- `pack/node.go`, `pack/python.go`, `pack/golang.go` and `pack/static.go` turn facts into build stages and a runtime.
- `pack/plan.go` renders a plan to a Dockerfile.
- `cmd/wolkpack` is the CLI.

## Commands

- `just build` builds `bin/wolkpack`.
- `just test` runs the end-to-end tests in `e2e/e2e_test.go`. `just test python-flask` runs one app.
- `just lint` runs golangci-lint (with gofmt and goimports) and govulncheck, the same checks CI runs. `just fmt` formats the code.

## Rules

- The CLI flags, the `wolkpack.toml` keys and their errors are a contract. Changing them needs an issue first.
- Parse manifests with real parsers in `pack/facts.go`. Providers read typed facts, never raw file text.
- When two answers are equally likely, fail with an error that names both. Never pick one by list order.
- Images run as UID 1000. A change that makes any image run as root is wrong.
- Images contain only what the app needs at runtime. No dev dependencies, no host `node_modules` or `__pycache__`, no build tools.
- Pin every tool version wolkpack installs, such as uv, Poetry and corepack.
- Every change gets an end-to-end test app in `e2e/testdata/` that fails without the change. Don't write unit tests.
- Write a comment only when the code can't show why it's written that way. Never narrate what the code does.
