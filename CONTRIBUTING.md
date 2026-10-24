# Contributing

## Build

```sh
go build -o bin/wolkpack ./cmd/wolkpack
```

## Test

wolkpack has one kind of test. `e2e/run.sh` builds every app in `e2e/testdata/` with wolkpack, starts it, and checks its HTTP response. You need Docker.

Run every app:

```sh
e2e/run.sh
```

Run only some apps:

```sh
e2e/run.sh python-flask node-pnpm
```

The script prints a table and writes it to `e2e/results.tsv`, with each image's size and user. Build logs go to `e2e/logs/`. A test fails if the image runs as root.

## Add a test app

Make a folder in `e2e/testdata/` with a small app that listens on `$PORT`. Add an `expect` file with text the response must contain.

A test app can also have these files:

- `expect-error` holds a message. The build must fail with it, and the app isn't started.
- `path` holds the URL path to request. The default is `/`.
- `app` names a subfolder to build, for monorepos.
- `absent` lists package names that must not be in the image.

Before each build, the script adds a `node_modules` and a `__pycache__` folder to the app. The test fails if either ends up in the image.

Commit lockfiles, and leave out `node_modules`, `.venv` and build output.

## Pull requests

Add a test app that fails before your change and passes after it. CI runs `go vet`, `gofmt` and `e2e/run.sh` on every pull request that changes code. `main` only accepts squash merges, and the `e2e` check must pass.
