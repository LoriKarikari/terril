#!/usr/bin/env bash
set -uo pipefail
cd "$(dirname "$0")/.."

go build -o bin/wolkpack ./cmd/wolkpack || exit 1
results=e2e/results.tsv
printf 'example\tstatus\tprovider\tdisk_mb\tpull_mb\tdive_eff\tdive_waste_mb\tuser\tdetail\n' >"$results"
failed=0

record() {
  printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' "$@" >>"$results"
  [ "$2" = PASS ] || failed=1
  echo "$2 $1 $9"
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
      record "$name" FAIL - - - - - - "build succeeded but should have failed"
    elif grep -qF "$want" "$log"; then
      record "$name" PASS - - - - - - "failed as expected"
    else
      record "$name" FAIL - - - - - - "wrong error, see $log"
    fi
    continue
  fi

  env_args=() stale_args=() values=() secrets=()
  if [ -f "e2e/testdata/$name/build-env" ]; then
    while read -r line; do
      kv=${line#secret }
      env_args+=(--env "$kv")
      stale_args+=(--env "${kv%%=*}=stale-${kv%%=*}")
      values+=("${kv#*=}")
      [ "$kv" != "$line" ] && secrets+=("${kv#*=}")
    done <"e2e/testdata/$name/build-env"
  fi

  mkdir -p "$dir/host-artifacts/node_modules" "$dir/host-artifacts/__pycache__"
  touch "$dir/host-artifacts/node_modules/HOST_MARKER" "$dir/host-artifacts/__pycache__/HOST_MARKER"
  status=0
  if [ ${#env_args[@]} -gt 0 ]; then
    bin/wolkpack build "${stale_args[@]}" -t "$image" "$dir" >"$log" 2>&1 || status=$?
  fi
  [ $status -eq 0 ] && { bin/wolkpack build "${env_args[@]}" -t "$image" "$dir" >>"$log" 2>&1 || status=$?; }
  rm -rf "$dir/host-artifacts"
  if [ $status -ne 0 ]; then
    record "$name" FAIL - - - - - - "build failed, see $log"
    continue
  fi
  cid=$(docker create "$image")
  rootfs=$(mktemp)
  docker export "$cid" >"$rootfs"
  docker rm "$cid" >/dev/null
  files=$(tar -tf "$rootfs")
  disk=$(stat -c %s "$rootfs" | awk '{printf "%.1f", $1/1e6}')
  rm -f "$rootfs"
  pull=$(docker save "$image" | wc -c | awk '{printf "%.1f", $1/1e6}')
  read -r eff waste < <(e2e/dive.sh "$image")
  if grep -q HOST_MARKER <<<"$files"; then
    record "$name" FAIL - - - - - - "host node_modules or __pycache__ copied into the image"
    continue
  fi
  shipped=""
  if [ -f "e2e/testdata/$name/absent" ]; then
    while read -r pkg; do
      grep -qE "node_modules/(\.pnpm/|\.bun/)?$pkg[@/]|\.yarn/cache/$pkg-npm-|site-packages/$pkg/" <<<"$files" && shipped="$shipped $pkg"
    done <"e2e/testdata/$name/absent"
  fi
  if [ -n "$shipped" ]; then
    record "$name" FAIL - - - - - - "image ships packages it shouldn't:$shipped"
    continue
  fi
  meta=$(docker history --no-trunc "$image"; docker image inspect "$image")
  leaked=""
  for v in "${values[@]}"; do grep -qF -- "$v" <<<"$meta" && leaked="$leaked '$v'"; done
  if [ ${#secrets[@]} -gt 0 ]; then
    cid=$(docker create "$image")
    content=$(docker export "$cid" | tar -xO 2>/dev/null | tr -d '\0')
    docker rm "$cid" >/dev/null
    for v in "${secrets[@]}"; do grep -qF -- "$v" <<<"$content" && leaked="$leaked '$v'"; done
  fi
  if [ -n "$leaked" ]; then
    record "$name" FAIL - - - - - - "build env value in the image:$leaked"
    continue
  fi
  provider=$(bin/wolkpack plan "$dir" | sed -n 's/^# wolkpack: //p')
  user=$(docker image inspect -f '{{.Config.User}}' "$image")
  if [ -z "$user" ] || [ "${user%%:*}" = 0 ] || [ "${user%%:*}" = root ]; then
    record "$name" FAIL "$provider" "$disk" "$pull" "$eff" "$waste" "${user:-root}" "image runs as root"
    continue
  fi

  cid=$(docker run -d -p 127.0.0.1::8080 "$image")
  port=$(docker port "$cid" 8080 | head -1 | cut -d: -f2)
  want=$(cat "e2e/testdata/$name/expect")
  path=$(cat "e2e/testdata/$name/path" 2>/dev/null || echo /)
  body=""
  for _ in $(seq 1 30); do
    body=$(curl -fsS "http://127.0.0.1:$port$path" 2>/dev/null) && [[ "$body" == *"$want"* ]] && break
    sleep 1
  done
  docker logs "$cid" >>"$log" 2>&1
  docker rm -f "$cid" >/dev/null
  if [[ "$body" == *"$want"* ]]; then
    record "$name" PASS "$provider" "$disk" "$pull" "$eff" "$waste" "$user" "got: $(echo "$body" | tr -s "\n\t" "  " | head -c 60)"
  else
    record "$name" FAIL "$provider" "$disk" "$pull" "$eff" "$waste" "$user" "wanted '$want', got '$(echo "$body" | tr -s "\n\t" "  " | head -c 60)'"
  fi
done

column -s$'\t' -t "$results"
awk -F'\t' 'NR>1 && $5!="-" {p+=$5; d+=$4; n++} END {printf "total: %d images, pull %.1f MB, disk %.1f MB\n", n, p, d}' "$results"
exit $failed
