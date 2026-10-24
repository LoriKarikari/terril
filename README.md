# wolkpack

wolkpack builds a container image from a Node, Python or Go app. You don't write a Dockerfile, and most apps need no config.

```sh
wolkpack build ./my-app
docker run -p 8080:8080 my-app
```

## How it works

1. wolkpack reads the app's manifest and lockfile to find out how to install, build and start it.
2. It writes a multi-stage Dockerfile. `wolkpack plan` prints it.
3. It runs `docker build` with that Dockerfile.

The image runs as UID 1000 and listens on `$PORT`, which defaults to 8080. Go apps run on a distroless image. So do Node apps whose start command is a plain `node` call.

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
wolkpack build -t api:dev ./my-app # build the image, tagged api:dev
```

## Supported apps

| Language | Package managers | Frameworks |
|---|---|---|
| Node | npm, pnpm, Yarn, Bun | any, started with the `start` script |
| Python | pip, uv, Poetry | Django, FastAPI, Flask |
| Go | Go modules | any |

A `web:` line in a `Procfile` replaces the start command.

If a Python or Go app has a `package.json` with a `build` script, Node runs that script first. The final image doesn't include Node. For Django apps that set `STATIC_ROOT`, wolkpack then runs `collectstatic`.

## Monorepos

Point wolkpack at one app in a JavaScript workspace:

```sh
wolkpack build apps/web
```

wolkpack finds the workspace root. It builds the workspace packages the app depends on, and then the app. It uses turbo or pnpm if the repo has them. The image contains only the app's production dependencies.

## Config

To change the defaults, add a `wolkpack.toml` next to the app:

```toml
provider = "python"            # pick a language when the repo has two
start = "python serve.py"      # start command, run through sh
packages = ["libpq5"]          # apt packages for the build and the image
build-packages = ["libpq-dev"] # apt packages for the build only
```

wolkpack rejects unknown keys, so a typo fails the build.

## Acknowledgements

wolkpack is inspired by [Nixpacks](https://github.com/railwayapp/nixpacks), [Railpack](https://github.com/railwayapp/railpack) and [Cloud Native Buildpacks](https://buildpacks.io). Thank you to their maintainers and contributors.

## License

Apache-2.0. See `LICENSE`.
