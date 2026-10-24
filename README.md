# wolkpack

wolkpack turns an app directory into a container image. Point it at a Node, Python or Go app. It needs no Dockerfile and usually no config.

```sh
wolkpack build ./my-app
docker run -p 8080:8080 my-app
```

## How it works

1. wolkpack reads the app's manifests, such as `package.json`, `requirements.txt`, `pyproject.toml` and `go.mod`, to find the language, package manager, version and start command.
2. It writes a multi-stage Dockerfile. `wolkpack plan` prints it.
3. It runs `docker build` with that Dockerfile.

The image runs as UID 1000 and listens on `$PORT`, which defaults to 8080. Go apps, and Node apps that start with a plain `node` command, run on distroless images.

## Install

You need Go 1.27 and Docker 23 or later.

```sh
git clone git@github.com:LoriKarikari/wolkpack.git
cd wolkpack
go build -o bin/wolkpack ./cmd/wolkpack
```

## Use

```sh
wolkpack plan  ./my-app            # print the Dockerfile
wolkpack build ./my-app            # build the image, tagged my-app
wolkpack build -t api:dev ./my-app # build with your own tag
```

## Supported apps

| Language | Package managers | Start command, first match wins |
|---|---|---|
| Node | npm, pnpm, Yarn, Bun | `scripts.start`, then `main`, `server.js`, `index.js`, `server.ts`, `index.ts` |
| Python | pip, uv, Poetry | Django or Flask with gunicorn, FastAPI with uvicorn, then `main.py`, `app.py`, `wsgi.py` |
| Go | Go modules | the binary from `main.go` or the only `cmd/<name>` |

A `web:` line in a `Procfile` replaces the start command.

If a Python or Go app has a `package.json` with a `build` script, Node builds the frontend first. Node isn't in the final image. Django apps that set `STATIC_ROOT` get `collectstatic` after that build.

## Monorepos

Point wolkpack at one app in a JavaScript workspace:

```sh
wolkpack build apps/web
```

wolkpack finds the workspace root, builds the workspace packages the app depends on, then builds the app. It uses turbo or pnpm when the repo has them. The image keeps only the app's production dependencies.

## Config

When the defaults don't fit, add a `wolkpack.toml` next to the app:

```toml
provider = "python"            # pick a language when the repo has two
start = "python serve.py"      # start command, run through sh
packages = ["libpq5"]          # apt packages for the build and the image
build-packages = ["libpq-dev"] # apt packages for the build only
```

An unknown key fails the build.

## Acknowledgements

wolkpack is inspired by [Nixpacks](https://github.com/railwayapp/nixpacks), [Railpack](https://github.com/railwayapp/railpack) and [Cloud Native Buildpacks](https://buildpacks.io).

## License

Apache-2.0. See `LICENSE`.
