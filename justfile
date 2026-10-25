default:
    @just --list

build:
    go build -o bin/wolkpack ./cmd/wolkpack

# Builds, runs and checks every app in e2e/testdata, or only the ones named.
[positional-arguments]
test *apps:
    cd e2e && go test -count=1 -timeout 60m -parallel 4 -run TestApps -apps "$(IFS=,; echo "$*")" .

# Rebuilds every app, or the ones named, after a source edit, and fails if the dependency install reruns.
# One app at a time: under parallel builds that share steps, BuildKit can miss a cached step.
[positional-arguments]
cache *apps:
    cd e2e && go test -count=1 -timeout 60m -parallel 1 -run TestCache -apps "$(IFS=,; echo "$*")" .

lint:
    go vet ./...
    test -z "$(gofmt -l .)"
    # A rule in the root .gitignore must not drop test app files; CI only sees committed ones.
    test -z "$(git ls-files --others --ignored --exclude-standard e2e/testdata | git check-ignore -v --stdin | grep '^.gitignore:' | grep -v node_modules)"
