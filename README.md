# wolkpack

wolkpack builds a container image from an app directory. It reads the app's manifests, writes a multi-stage Dockerfile, and runs `docker build`. It supports Node, Python and Go.

## Install

You need Go 1.27 and Docker with BuildKit, which Docker 23 and later use by default.

```sh
git clone git@github.com:LoriKarikari/wolkpack.git
cd wolkpack
go build -o bin/wolkpack ./cmd/wolkpack
```

## Use

```sh
wolkpack plan  ./my-app            # print the Dockerfile
wolkpack build ./my-app            # build the image, tagged my-app
wolkpack build -t api:dev ./my-app # build with a tag you choose
```

The image listens on `$PORT`, which defaults to 8080:

```sh
docker run -p 8080:8080 my-app
```

## Supported apps

| Language | Package managers | Start command, first match wins |
|---|---|---|
| Node | npm, pnpm, Yarn 1, Yarn 2 and later (including Plug'n'Play), Bun | `scripts.start`, then `main`, `server.js`, `index.js`, `server.ts`, `index.ts` |
| Python | pip with `requirements.txt` or `pyproject.toml`, uv, Poetry | Django with gunicorn, FastAPI with uvicorn, Flask with gunicorn, then `main.py`, `app.py`, `wsgi.py` |
| Go | Go modules | the binary built from `main.go`, or from the only `cmd/<name>` |

A Django, FastAPI or Flask app must list its server in its dependencies. Without it, the build fails and names the missing package.

For Django apps that set `STATIC_ROOT`, wolkpack runs `collectstatic` during the build, after any Node build. It doesn't run migrations, because several replicas starting at once would run them in parallel.

If a Python or Go app also has a `package.json` with a `build` script, Node builds it in a separate stage, and wolkpack copies the output into the app before the app's own build. Node isn't in the final image.

Bun apps build and run on the `oven/bun` image.

A `web:` line in a `Procfile` replaces the start command for any language.

## Monorepos

Point wolkpack at one app inside a JavaScript workspace:

```sh
wolkpack build apps/web
```

wolkpack looks in parent folders, up to the repo root, for the `pnpm-workspace.yaml` or `package.json` `workspaces` list that includes the app. It builds from that root, so the lockfile and the app's sibling packages are in the build.

It builds the app this way:

- If the root has `turbo.json`, it runs `turbo run build --filter=<app>`.
- With pnpm, it runs `pnpm --filter <app>... run build`.
- Otherwise, it runs the `build` script of each workspace package the app depends on, dependencies first, and then the app's own.

After the build, wolkpack removes dev dependencies and the dependencies of other apps in the workspace. Yarn 1 can't install one workspace member alone, so a Yarn 1 image keeps the other members' production dependencies. The container starts in the app's folder.

A Python or Go app in a subfolder needs no workspace. Point wolkpack at its folder.

## Config

Most apps need no config. When the defaults don't fit, add a `wolkpack.toml` next to the app:

```toml
provider = "python"            # same as --provider; the flag wins
start = "python serve.py"      # runs through sh and replaces the Procfile web: line
packages = ["libpq5"]          # apt packages for every build stage and the runtime
build-packages = ["libpq-dev"] # apt packages for build stages only
```

wolkpack rejects unknown keys, so a misspelled key fails the build. Setting `start` or `packages` moves a Go or Node app from distroless to a Debian image, because distroless has no shell and no apt.

## Tests

`e2e/run.sh` builds every app in `e2e/testdata/`, starts it, and checks its HTTP response against the app's `expect` file. It prints the results and writes them to `e2e/results.tsv`, with each image's size and user. Pass app names to run only those.

A test app can also contain these files:

- `expect-error` holds a message. The build must fail with it.
- `app` names a subfolder to build, for monorepos.
- `absent` lists package names that must not be in the image.

Before each build, the script adds a `node_modules` and a `__pycache__` folder to the app. The test fails if either one ends up in the image.

CI runs `go vet`, `gofmt` and `e2e/run.sh` on every push to `main` and on every pull request.

## Acknowledgements

wolkpack is inspired by [Nixpacks](https://github.com/railwayapp/nixpacks), [Railpack](https://github.com/railwayapp/railpack) and [Cloud Native Buildpacks](https://buildpacks.io).

## License

Apache-2.0. See `LICENSE`.
