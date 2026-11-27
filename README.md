# terril

Build a container image from your app, without writing a Dockerfile.

```sh
terril build ./my-app
docker run -p 8080:8080 my-app
```

## Highlights

- Builds Node, Python, Ruby, PHP, .NET, Rust and Go apps, and static sites.
- Writes a plain Dockerfile that builds anywhere Docker runs.
- Shows you the Dockerfile, so you can take it over anytime.
- Makes small images with only what your app needs to run.
- Runs your app as a non-root user.
- Tells you what to fix instead of building an image that won't start.
- Refuses language versions that no longer get security fixes.

## Install

Download the archive for your system from the [releases page](https://github.com/LoriKarikari/terril/releases), unpack it, and put `terril` on your `PATH`. `checksums.txt` lists each archive's SHA-256. terril runs on Linux and macOS, on amd64 and arm64, and needs Docker 23 or later.

Or install from source with Go 1.27 or later:

```sh
go install github.com/LoriKarikari/terril/cmd/terril@latest
```

## Usage

Build an image. It's tagged with the folder's name, here `my-app`:

```sh
terril build ./my-app
```

Pick your own tag:

```sh
terril build -t api:dev ./my-app
```

See the Dockerfile without building it:

```sh
terril plan ./my-app
```

To take over, save it and build it yourself:

```sh
terril plan ./my-app > Dockerfile
```

A folder name that docker can't use as a tag, such as `My App`, becomes `my-app`. Flags go before the folder. When `docker build` fails, terril exits with docker's exit code. `terril help` prints the usage.

The image listens on `$PORT`, which defaults to 8080.

### Build-time variables

Some builds need values that aren't in the repo, such as `VITE_API_URL` or a token for a private registry. Pass each one with `--env`:

```sh
terril build --env VITE_API_URL=https://api.example.com ./my-app
NPM_TOKEN=... terril build --env NPM_TOKEN ./my-app
```

Without `=VALUE`, terril takes the value from your environment. The build can read these values, but they aren't stored in the image. When a value changes, terril rebuilds the steps that read it. Set the variables your running app needs when you start the container, for example with `docker run -e`.

### Monorepos

Point terril at one app in a JavaScript or uv workspace:

```sh
terril build apps/web
```

terril finds the workspace root, builds the packages the app depends on, and then the app. The image holds only that app and its production dependencies.

### Config

To change the defaults, add a `terril.toml` next to the app:

```toml
provider = "python"            # pick a language when the repo has two
start = "python serve.py"      # start command, run through sh
packages = ["libpq5"]          # apt packages for the build and the image
build-packages = ["libpq-dev"] # apt packages for the build only
```

A typo in a key fails the build. A `web:` line in a `Procfile` also sets the start command.

## Supported apps

| Language | Package managers | Frameworks |
|---|---|---|
| Node | npm, pnpm, Yarn, Bun | Next.js, Nuxt, Astro, React Router, or any app with a `start` script |
| Python | pip, uv, Poetry | Django, FastAPI, Flask |
| Ruby | Bundler | Rails, or any Rack app with puma |
| PHP | Composer | Laravel, or any app with `public/index.php` or `index.php` |
| .NET | NuGet | ASP.NET Core and console apps |
| Rust | Cargo | any binary crate, including workspaces |
| Go | Go modules | any, including apps that use cgo |
| Static sites | none, or any Node build | plain HTML, Vite apps without a server |

For Node, Python, PHP, .NET, Rust, Go and static sites, the runtime image is distroless, so it has no shell or package manager. Those apps get a Debian image instead when their start command needs a shell or `terril.toml` adds apt packages. Ruby apps run on the official `ruby` slim image.

When terril finds two answers and nothing says which is right, it fails and names both. That covers lockfiles of two Node package managers, a `packageManager` field that disagrees with the lockfile, both `rust-toolchain` and `rust-toolchain.toml`, and a Python app that defines a Flask and a FastAPI app. Set `start` in `terril.toml` to pick the Python one. A Python app with both `main.py` and `app.py` runs `main.py`, and a Node app with both Next.js and Nuxt builds Next.js. `start` overrides both.

A folder with only an `index.html`, or a Vite app with no `start` script, is served as a static site by Caddy. Vite apps get single-page-app routing, so `/dashboard` serves `index.html`.

If a Python, Ruby, PHP or Go app has a `package.json` with a `build` script, Node runs that script first. The final image doesn't include Node. For Django apps that set `STATIC_ROOT`, terril then runs `collectstatic`. Rails apps get `assets:precompile`. Rails needs `SECRET_KEY_BASE` or `RAILS_MASTER_KEY` when the container starts, and terril never puts either in the image.

PHP apps run on [FrankenPHP](https://frankenphp.dev)'s static build, which has PHP 8.5 and 80 extensions, such as intl, gd, pdo_pgsql and redis. The same binary runs Composer, so an app that needs another extension fails at `composer install` and names it. Laravel apps need `APP_KEY` when the container starts, and log to stderr.

For .NET, terril publishes the project at the app root, or the only web project in the folder, such as an API next to its test projects. .NET apps run on Microsoft's chiseled images, with ICU for culture formatting unless the project sets `InvariantGlobalization`. A `global.json` that pins an SDK version picks that exact SDK. A project that lists several `TargetFrameworks` ships the newest.

For Rust, terril builds the only binary in the crate or workspace, or the one `default-run` names. Dependencies compile in their own cached step with [cargo-chef](https://github.com/LukeMathWalker/cargo-chef), so editing your code doesn't recompile them. rustup installs the Rust that `rust-toolchain.toml` or `rust-toolchain` names, including nightlies, with the targets and components it lists.

terril builds only the Node, Python, Ruby and .NET versions that still get security fixes, by the dates on [endoflife.date](https://endoflife.date). From six months before a version's end of life, the build prints a warning with the date. Two months after it, terril refuses to build that version and names the ones it builds. A Node version like `22.11.0` builds on the newest Node 22. Go apps build with the newest Go, because the `go` line in `go.mod` is a minimum.

## Acknowledgements

terril is inspired by [Nixpacks](https://github.com/railwayapp/nixpacks), [Railpack](https://github.com/railwayapp/railpack) and [Cloud Native Buildpacks](https://buildpacks.io).

## License

Apache-2.0. See `LICENSE`.
