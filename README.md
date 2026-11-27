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

Prerequisites:

- Go 1.27 or later
- Docker 23 or later
- Linux or macOS

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

`terril help` lists every flag.

To change the defaults, add a `terril.toml` next to the app:

```toml
provider = "python"            # pick a language when the repo has two
start = "python serve.py"      # start command, run through sh
packages = ["libpq5"]          # apt packages for the build and the image
build-packages = ["libpq-dev"] # apt packages for the build only
```

## Acknowledgements

terril is inspired by [Nixpacks](https://github.com/railwayapp/nixpacks), [Railpack](https://github.com/railwayapp/railpack) and [Cloud Native Buildpacks](https://buildpacks.io).

## License

Apache-2.0. See `LICENSE`.
