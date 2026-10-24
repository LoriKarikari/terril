#!/usr/bin/env bash
set -uo pipefail
cd "$(dirname "$0")/.."

go build -o bin/wolkpack ./cmd/wolkpack || exit 1
results=e2e/results.tsv
printf 'example\tstatus\tprovider\timage_mb\tuser\tdetail\n' >"$results"
failed=0

record() {
  printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$@" >>"$results"
  [ "$2" = PASS ] || failed=1
  echo "$2 $1 $6"
}

names=("$@")
[ ${#names[@]} -eq 0 ] && names=($(ls e2e/testdata))

for name in "${names[@]}"; do
  dir=e2e/testdata/$name
  [ -f "$dir/app" ] && dir=$dir/$(cat "$dir/app")
  image=wolkpack-e2e-$name
  log=e2e/logs/$name.log
  mkdir -p e2e/logs

  if [ -f "$dir/expect-error" ]; then
    want=$(cat "e2e/testdata/$name/expect-error")
    if bin/wolkpack build -t "$image" "$dir" >"$log" 2>&1; then
      record "$name" FAIL - - - "build succeeded but should have failed"
    elif grep -qF "$want" "$log"; then
      record "$name" PASS - - - "failed as expected"
    else
      record "$name" FAIL - - - "wrong error, see $log"
    fi
    continue
  fi

  mkdir -p "$dir/host-artifacts/node_modules" "$dir/host-artifacts/__pycache__"
  touch "$dir/host-artifacts/node_modules/HOST_MARKER" "$dir/host-artifacts/__pycache__/HOST_MARKER"
  bin/wolkpack build -t "$image" "$dir" >"$log" 2>&1
  status=$?
  rm -rf "$dir/host-artifacts"
  if [ $status -ne 0 ]; then
    record "$name" FAIL - - - "build failed, see $log"
    continue
  fi
  cid=$(docker create "$image")
  files=$(docker export "$cid" | tar -t)
  docker rm "$cid" >/dev/null
  if grep -q HOST_MARKER <<<"$files"; then
    record "$name" FAIL - - - "host node_modules or __pycache__ copied into the image"
    continue
  fi
  shipped=""
  if [ -f "e2e/testdata/$name/absent" ]; then
    while read -r pkg; do
      grep -qE "node_modules/(\.pnpm/)?$pkg[@/]" <<<"$files" && shipped="$shipped $pkg"
    done <"e2e/testdata/$name/absent"
  fi
  if [ -n "$shipped" ]; then
    record "$name" FAIL - - - "image ships packages it shouldn't:$shipped"
    continue
  fi
  provider=$(bin/wolkpack plan "$dir" | sed -n 's/^# wolkpack: //p')
  size=$(docker image inspect -f '{{.Size}}' "$image" | awk '{printf "%d", $1/1000000}')
  user=$(docker image inspect -f '{{.Config.User}}' "$image")
  if [ -z "$user" ] || [ "${user%%:*}" = 0 ] || [ "${user%%:*}" = root ]; then
    record "$name" FAIL "$provider" "$size" "${user:-root}" "image runs as root"
    continue
  fi

  cid=$(docker run -d -p 127.0.0.1::8080 "$image")
  port=$(docker port "$cid" 8080 | head -1 | cut -d: -f2)
  want=$(cat "e2e/testdata/$name/expect")
  body=""
  for _ in $(seq 1 30); do
    body=$(curl -fsS "http://127.0.0.1:$port/" 2>/dev/null) && [[ "$body" == *"$want"* ]] && break
    sleep 1
  done
  docker logs "$cid" >>"$log" 2>&1
  docker rm -f "$cid" >/dev/null
  if [[ "$body" == *"$want"* ]]; then
    record "$name" PASS "$provider" "$size" "$user" "got: $(echo "$body" | head -c 60)"
  else
    record "$name" FAIL "$provider" "$size" "$user" "wanted '$want', got '$(echo "$body" | head -c 60)'"
  fi
done

column -s$'\t' -t "$results"
exit $failed
