# Contributing to terril

Thanks for wanting to help. terril takes no pull requests. The maintainer writes every change, so the way to help is a good issue. This page says which issues lead somewhere and what to put in them.

## What's welcome, and what isn't

### Open an issue

- **Bug reports.** terril builds an image that's wrong, too big, or doesn't start. Include a small app that shows it.
- **Package managers and frameworks.** Support for one that a language already almost handles, such as a new lockfile or a framework's start command.
- **Docs fixes.** Something is unclear, outdated or wrong.
- **Small improvements.** A clearer error, a smaller image or a faster build.
- **New languages and contract changes.** A new language means a new provider and long-term upkeep. A change to CLI flags, `terril.toml` keys, or how terril detects an app affects every user. Describe the apps you want to build and why.

### Usually declined

- **A custom plan format or BuildKit frontend.** The plan is a Dockerfile on purpose, so anyone can read it and build it without terril.
- **Guessing.** Detection that picks a language or start command when two are equally likely. terril fails and asks instead.

## How the maintainer ships

The maintainer pushes straight to `main` after `just commits`, `just lint`, `just test` and `just cache` pass. No checks run on GitHub. Every change comes with a test app in `e2e/testdata/` that fails without it.

### Commit messages

Commits on `main` use `<type>(<scope>): <summary>`, in lowercase and the imperative mood, with no period at the end.

- **Types.** `feat`, `fix`, `docs`, `test`, `refactor`, `perf`, `build`, `ci`, and `chore`.
- **CI changes.** Use `ci`, even for a fix. `feat`, `fix` and `perf` are for changes users of terril notice, and they go into the release notes.
- **Scopes.** Optional. Use the language or area the change touches, such as `node`, `python`, `go`, `static`, `config`, or `plan`.
- **Breaking changes.** Add `!` after the type or scope, and explain the break in a `BREAKING CHANGE:` footer. Anything that changes CLI flags or `terril.toml` in an incompatible way counts.

For example:

```
fix(node): prune Yarn workspace builds
feat(python): start Litestar apps with uvicorn
docs: explain how terril picks the start command
feat(config)!: rename packages to apt-packages
```

## Testing

You don't need to run the tests to open an issue, but a failing test app is the best reproduction you can give. terril has one kind of test. `e2e/e2e_test.go` builds every app in `e2e/testdata/` with the terril CLI, starts it, and checks its HTTP response. You need Docker and [dive](https://github.com/wagoodman/dive).

Run every app, or only some:

```sh
just test
just test python-flask node-pnpm
```

The run prints a table and writes it to `e2e/results.tsv`, with each image's size on disk, its compressed pull size, dive's efficiency and wasted bytes, and its user. A test fails if the image runs as root, or if dive's efficiency score is below 95%, which means files are duplicated or deleted across layers. Build logs go to `e2e/logs/`.

Apps build four at a time. Each test removes its images when it ends, even when it fails, and the run trims Docker's build cache to 10 GB at the end. Pass `-keep` to `go test` to keep the images.

### Add a test app

Make a folder in `e2e/testdata/` with a small app that listens on `$PORT`. Add an `expect` file with text the response must contain.

A test app can also have these files:

- `expect-error` holds a message. The build must fail with it, and the app isn't started.
- `expect-warning` holds a warning the build must print.
- `today` holds a date like `2026-11-01`. terril builds the app as if it were that day, through the `TERRIL_TODAY` variable. Use it to test end-of-life warnings and refusals, which change with the calendar.
- `path` holds the URL path to request. The default is `/`.
- `app` names a subfolder to build, for monorepos.
- `absent` lists package names that must not be in the image.
- `absent-files` lists paths that must not be in the image, such as `srv/.env`.
- `run-env` holds `KEY=VALUE` lines that the test passes to `docker run -e`, for values a platform sets at runtime, such as secrets.
- `build-env` holds `KEY=VALUE` lines that the test passes with `--env`. It builds once with other values first, so a cached build fails the test. A line that starts with `secret ` must not appear anywhere in the image. Add `build-env` to the app's `.dockerignore`. No `--env` value may appear in the image's history or config.

Before each build, the test adds a `node_modules` and a `__pycache__` folder to the app. The test fails if either ends up in the image.

Commit lockfiles, and leave out `node_modules`, `.venv` and build output.

`just cache` builds each app, adds a source file, and rebuilds. It fails if the rebuild reruns the dependency install, and writes the rebuild time to `e2e/cache.tsv`. It builds one app at a time, because parallel builds that share steps can miss Docker's cache.

## Releases

The maintainer releases by pushing a `v*` tag on `main`. Before 1.0, a release with a `feat:` commit bumps the minor version, and one with only fixes bumps the patch version. GoReleaser then builds `terril` for Linux and macOS on amd64 and arm64, attaches the archives and `checksums.txt` to the release, and writes its notes from the `feat:`, `fix:` and `perf:` commits since the last tag.

## Using AI tools

AI-assisted issues are fine. They meet the same bar as any other issue:

- **Show it's real.** Include the actual `terril` output, build log, or container error that proves the problem exists.
- **Include how to reproduce it.** The exact commands you ran, not a description of them.
- **Review it yourself first.** You're responsible for every word you submit.

Agents should follow `AI_CONTRIBUTING.md`, which walks through each step.

## Bug reports

Open an issue with the bug report form. It asks for the terril commit, the command you ran, the output of `terril plan`, and what you expected.

## Security

Don't open a public issue for a security problem. Report it privately through GitHub's private vulnerability reporting on this repository, with the terril commit and steps to reproduce.

## License

terril uses the Apache-2.0 license.
