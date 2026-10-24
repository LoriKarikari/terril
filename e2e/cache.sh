#!/usr/bin/env bash
set -uo pipefail
cd "$(dirname "$0")/.."

go build -o bin/wolkpack ./cmd/wolkpack || exit 1
out=e2e/cache.tsv
printf 'example\tstatus\trebuild_s\tsteps_rerun\tdeps_rerun\n' >"$out"
failed=0
install_re='install|uv sync|npm ci|go mod download'
# Tool setup, and production prunes that run after the build, aren't the dependency install.
not_install_re='apt-get|install -g|python install|tool install|prune|--prod|--production|--omit=dev'

names=("$@")
[ ${#names[@]} -eq 0 ] && names=($(ls e2e/testdata))

for name in "${names[@]}"; do
  src=e2e/testdata/$name
  [ -f "$src/expect-error" ] || [ -f "$src/build-env" ] && continue
  work=$(mktemp -d)
  cp -r "$src/." "$work/"
  dir=$work
  [ -f "$work/app" ] && dir=$work/$(cat "$work/app")
  log=e2e/logs/$name.cache.log
  mkdir -p e2e/logs

  if ! bin/wolkpack build -t "wolkpack-cache-$name" "$dir" >"$log" 2>&1; then
    printf '%s\tFAIL\t-\t-\tfirst build failed\n' "$name" >>"$out"
    failed=1
    rm -rf "$work"
    continue
  fi
  plan_dir=$src
  [ -f "$src/app" ] && plan_dir=$src/$(cat "$src/app")
  echo "$RANDOM" >"$dir/wolkpack-cache-probe.txt"
  start=$(date +%s.%N)
  bin/wolkpack build -t "wolkpack-cache-$name" "$dir" >"$log" 2>&1
  secs=$(echo "$(date +%s.%N) - $start" | bc | awk '{printf "%.1f", $1}')
  rm -rf "$work"

  # BuildKit prints "#N [stage i/n] RUN ..." for every step, then "#N CACHED" for cached ones.
  rerun=$(awk '
    /^#[0-9]+ \[[^]]+\] RUN / { id = $1; sub(/^#[0-9]+ \[[^]]+\] RUN (--mount=[^ ]+ )*/, ""); cmd[id] = $0 }
    /^#[0-9]+ CACHED/ { cached[$1] = 1 }
    END { for (id in cmd) if (!(id in cached)) print cmd[id] }' "$log" | sort -u)
  steps=$(grep -c . <<<"$rerun")
  # The first dependency install in the plan must come from cache. Later installs, such as a
  # workspace's own packages after the source copy, may rerun.
  first=$(bin/wolkpack plan "$plan_dir" | sed -n 's/^RUN \(--mount=[^ ]* \)*//p' | grep -E "$install_re" | grep -vE "$not_install_re" | head -1)
  deps=""
  [ -n "$first" ] && grep -qxF -- "$first" <<<"$rerun" && deps=$(cut -c1-60 <<<"$first")
  status=PASS
  [ -n "$deps" ] && status=FAIL && failed=1
  printf '%s\t%s\t%s\t%s\t%s\n' "$name" "$status" "$secs" "$steps" "${deps:--}" >>"$out"
  echo "$status $name ${secs}s"
done

column -s$'\t' -t "$out"
exit $failed
