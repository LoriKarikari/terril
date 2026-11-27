# pin: datasource=go depName=github.com/golangci/golangci-lint/v2
golangci := "go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0"
# pin: datasource=go depName=golang.org/x/vuln
govulncheck := "go run golang.org/x/vuln/cmd/govulncheck@v1.9.0"

default:
    @just --list

build:
    go build -o bin/terril ./cmd/terril

# Builds, runs and checks every app in e2e/testdata, or only the ones named. Without names it also builds three apps from the ECR Public mirror of the official images.
[positional-arguments]
test *apps:
    cd e2e && go test -count=1 -timeout 60m -parallel 4 -run '^(TestApps|TestAppsFromOfficialImagesMirror)$' -apps "$(IFS=,; echo "$*")" .

# Rebuilds every app, or the ones named, after a source edit, and fails if the dependency install reruns.
# One app at a time: under parallel builds that share steps, BuildKit can miss a cached step.
[positional-arguments]
cache *apps:
    cd e2e && go test -count=1 -timeout 60m -parallel 1 -run '^TestCache$' -apps "$(IFS=,; echo "$*")" .

# Lists every pinned version next to the latest one upstream, and marks the outdated ones.
pins:
    scripts/pins

fmt:
    {{golangci}} fmt ./...

lint:
    {{golangci}} run ./...
    {{govulncheck}} ./...
    # A rule in the root .gitignore must not drop test app files; a clone only sees committed ones.
    test -z "$(git ls-files --others --ignored --exclude-standard e2e/testdata | git check-ignore -v --stdin | grep '^.gitignore:' | grep -v node_modules)"

# Checks that every commit in the range follows the commit message rules in CONTRIBUTING.md.
commits range="origin/main..HEAD":
    #!/usr/bin/env bash
    set -euo pipefail
    pattern='^((feat|fix|docs|test|refactor|perf|build|ci|chore)(\([a-z0-9-]+\))?!?: [a-z0-9`].*[^.]|Revert ".+")$'
    log=$(git log --no-merges --format='%H%x09%s' "{{range}}")
    bad=0
    while IFS=$'\t' read -r sha subject; do
        [[ -n "$sha" ]] || continue
        if ! [[ "$subject" =~ $pattern ]]; then
            echo "not a conventional commit: ${sha:0:7} $subject" >&2
            bad=1
        elif [[ "$subject" == fix\(ci\)* ]]; then
            echo "a CI change is ci:, never fix(ci): ${sha:0:7} $subject" >&2
            bad=1
        elif [[ "$subject" =~ ^[a-z]+(\([a-z0-9-]+\))?!: ]] && ! git log -1 --format=%B "$sha" | grep -q '^BREAKING CHANGE: '; then
            echo "a ! subject needs a BREAKING CHANGE: footer: ${sha:0:7} $subject" >&2
            bad=1
        fi
    done <<<"$log"
    exit "$bad"
