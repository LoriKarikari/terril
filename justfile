default:
    @just --list

build:
    go build -o bin/wolkpack ./cmd/wolkpack

# Builds, runs and checks every app in e2e/testdata, or only the ones named.
[positional-arguments]
test *apps:
    e2e/run.sh "$@"

lint:
    go vet ./...
    test -z "$(gofmt -l .)"
    # A rule in the root .gitignore must not drop test app files; CI only sees committed ones.
    test -z "$(git ls-files --others --ignored --exclude-standard e2e/testdata | git check-ignore -v --stdin | grep '^.gitignore:' | grep -v node_modules)"
