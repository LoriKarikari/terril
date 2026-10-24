# wolkpack

wolkpack builds a container image from a Node, Python or Go app.

```sh
wolkpack build ./my-app
docker run -p 8080:8080 my-app
```

## How it works

wolkpack reads the app's manifest and lockfile to work out how to install, build and start it. It turns that into a Dockerfile and builds it with Docker.

The app builds in one image and runs in a separate, smaller one that contains only what it needs at runtime. That image runs as a non-root user and listens on `$PORT`, which defaults to 8080.

## Install

You need Go 1.27 and Docker 23 or later.

```sh
git clone git@github.com:LoriKarikari/wolkpack.git
cd wolkpack
go build -o bin/wolkpack ./cmd/wolkpack
```

## Use

Print the Dockerfile wolkpack would build:

```sh
wolkpack plan ./my-app
```

Build the image. It gets the folder's name as its tag, here `my-app`:

```sh
wolkpack build ./my-app
```

Build the image with your own tag:

```sh
wolkpack build -t api:dev ./my-app
```

## Supported apps

| Language | Package managers | Frameworks |
|---|---|---|
| Node | npm, pnpm, Yarn, Bun | any, started with the `start` script |
| Python | pip, uv, Poetry | Django, FastAPI, Flask |
| Go | Go modules | any |
| Static sites | none, or any Node build | plain HTML, Vite apps without a server |

A `web:` line in a `Procfile` replaces the start command.

A folder with only an `index.html`, or a Vite app with no `start` script, is served as a static site by Caddy. Vite apps get single-page-app routing, so `/dashboard` serves `index.html`.

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
