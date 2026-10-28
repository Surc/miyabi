#!/usr/bin/env bash
# Build a separate rating service; never modify the supplied source checkout.
set -euo pipefail

output=$(realpath -m "${1:?usage: build-engine.sh OUTPUT [SOURCE_CHECKOUT]}")
source_repo=${2:-https://github.com/javinizer/javinizer-go.git}
script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
go_bin=${GO_BIN:-go}
revision=fb950d76d7e2eb9416737fa2e9131f91b39c8beb

if [[ $("$go_bin" env GOHOSTOS) != linux || $("$go_bin" env GOHOSTARCH) != amd64 ]]; then
  echo 'Build this service on Linux amd64 (CGO SQLite is required).' >&2
  exit 1
fi
if [[ $("$go_bin" env GOVERSION) != go1.27.3 ]]; then
  echo 'Use the validated Go 1.27.3 toolchain via GO_BIN.' >&2
  exit 1
fi

work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
git clone --quiet --no-hardlinks -- "$source_repo" "$work/source"
git -C "$work/source" checkout --quiet --detach "$revision"
test "$(git -C "$work/source" rev-parse HEAD)" = "$revision"
git -C "$work/source" apply --check "$script_dir/identity.patch"
git -C "$work/source" apply "$script_dir/identity.patch"

mkdir -p -- "$(dirname -- "$output")"
cd -- "$work/source"
CGO_ENABLED=1 GOOS=linux GOARCH=amd64 GOAMD64=v1 "$go_bin" build -mod=readonly -trimpath \
  -ldflags '-X github.com/javinizer/javinizer-go/internal/version.Version=v1.6.5-miyabi-rating.1' \
  -o "$output" ./cmd/javinizer
sha256sum -- "$output"
