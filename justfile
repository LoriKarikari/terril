golangci := "go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0"

default:
    @just --list

build:
    go build -o bin/wolkpack ./cmd/wolkpack

# Builds, runs and checks every app in e2e/testdata, or only the ones named, then rebuilds it after a
# source edit and fails if the dependency install reruns.
[positional-arguments]
test *apps:
    cd e2e && go test -count=1 -timeout 60m -parallel 4 -run TestApps -apps "$(IFS=,; echo "$*")" .

fmt:
    {{golangci}} fmt ./...

lint:
    {{golangci}} run ./...
    go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
    # A rule in the root .gitignore must not drop test app files; CI only sees committed ones.
    test -z "$(git ls-files --others --ignored --exclude-standard e2e/testdata | git check-ignore -v --stdin | grep '^.gitignore:' | grep -v node_modules)"
