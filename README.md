# wolkpack

wolkpack turns an app directory into a container image. Point it at a Node, Python or Go app and it writes a Dockerfile and builds it.

```sh
go build -o bin/wolkpack ./cmd/wolkpack
bin/wolkpack plan  e2e/testdata/python-node-assets   # print the Dockerfile
bin/wolkpack build e2e/testdata/python-node-assets   # build it with docker
```

## What it does differently

- **The plan is a Dockerfile.** `wolkpack plan` prints a normal multi-stage Dockerfile. Read it, commit it, or edit it and stop using wolkpack. Nothing is hidden in a custom format.
- **Several languages in one app.** A Python or Go app with a `package.json` that has a `build` script gets its frontend built by Node in a separate stage. The built files are copied into the app before its own build runs. Node itself stays out of the final image.
- **Ambiguity is an error, not a guess.** A repo with both `go.mod` and `requirements.txt` fails with a message naming both. `--provider` picks one. Nothing depends on the order of a list.
- **Manifests are parsed, not grepped.** `package.json`, `pyproject.toml`, `requirements.txt` and `go.mod` are parsed once into typed facts. Python dependency names are normalized per PEP 503, so `fastapi-users` doesn't count as `fastapi`.
- **Version ranges work.** `"engines": {"node": ">=22"}` or `requires-python = ">=3.12"` resolve to the default version if it fits, otherwise the newest supported one. If nothing fits, the build fails and says why.
- **Non-root and small by default.** Every image runs as UID 1000. Go apps ship as a static binary on distroless, about 14 MB. Node apps whose start command is a plain `node` call run on distroless Node, about 214 MB. Start scripts that need a shell keep the slim Node image.

## Supported

| Language | Package managers | Start command, in order |
|---|---|---|
| Node | npm, pnpm, Yarn 1, Yarn 2+ (including Plug'n'Play), Bun | `scripts.start`, `main`, `server.js`, `index.js`, `server.ts`, `index.ts` |
| Python | pip (`requirements.txt` or `pyproject.toml`), uv, Poetry | Django via gunicorn, FastAPI via uvicorn, Flask via gunicorn, then `python main.py`, `app.py` or `wsgi.py` |
| Go | modules | the binary from `main.go` or the single `cmd/<name>` |

Django apps with `STATIC_ROOT` set get `collectstatic` at build time, after any Node asset build. Migrations don't run automatically.

Bun apps build and run on the `oven/bun` image instead of `node`.

A `Procfile` `web:` line overrides the start command for any language. The app listens on `$PORT`, which defaults to 8080.

## Config

Most apps need none. When the defaults don't fit, add a `wolkpack.toml` next to the app:

```toml
provider = "python"            # same as --provider; the flag wins
start = "python serve.py"      # runs through sh; replaces the Procfile web: line
packages = ["libpq5"]          # apt packages in every build stage and the runtime
build-packages = ["libpq-dev"] # apt packages in build stages only
```

Unknown keys fail the build, so a typo can't be silently ignored. Setting `start` or `packages` moves Go and Node apps off distroless onto a Debian image, because distroless has no shell and no apt.

## Tests

`e2e/run.sh` builds every app in `e2e/testdata/`, runs it and checks its HTTP response. It prints and writes `e2e/results.tsv` with each image's size and user. An app with an `expect-error` file must fail to build with that message.

## License

Apache-2.0. See `LICENSE`.
