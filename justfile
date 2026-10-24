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
