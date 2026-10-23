#!/bin/bash
# Release-only orchestration. Business sources and upstream locks stay unchanged.
# Run from a clean Git checkout; output must be outside it for VCS provenance.
set -euo pipefail
version=${1:?Usage: build-release.sh VERSION OUTPUT_DIRECTORY}
output=$(realpath -m "${2:?Missing output directory}")
root=$(git rev-parse --show-toplevel)
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || exit 2
[[ -z "$(git -C "$root" status --porcelain)" ]] || { echo 'Use a clean checkout' >&2; exit 2; }
[[ "$output" != "$root" && "$output" != "$root/"* ]] || { echo 'Output must be outside the source checkout' >&2; exit 2; }
[[ "$(uname -m)" == x86_64 ]] || exit 2
build=$(mktemp -d)
trap 'rm -rf "$build"' EXIT
mkdir -p "$output" "$build/fonts"

download() {
  curl --fail --location --retry 3 --connect-timeout 20 --max-time 300 "$1" -o "$2"
  printf '%s  %s\n' "$3" "$2" | sha256sum --check --status
}
# Node checksum verified against the official release SHASUMS256.txt. fnpack
# and Noto are pinned to these official downloads, including their licenses.
download 'https://nodejs.org/dist/v24.19.0/node-v24.19.0-linux-x64.tar.xz' "$build/node.tar.xz" \
  14b342e71204f811bde6153be8e04b62aef63c236fef92b55f9c83154b409647
download 'https://static2.fnnas.com/fnpack/fnpack-1.2.3-linux-amd64' "$build/fnpack" \
  54b97fa7b70968c4d05c79840f5daeff508957d0bb2062fdb0376d00d9615c93
download 'https://raw.githubusercontent.com/notofonts/noto-cjk/Sans2.004/Sans/OTC/NotoSansCJK-Regular.ttc' "$build/fonts/NotoSansCJK-Regular.ttc" \
  b76b0433203017ca80401b2ee0dd69350349871c4b19d504c34dbdd80541690a
download 'https://raw.githubusercontent.com/notofonts/noto-cjk/Sans2.004/LICENSE' "$build/fonts/LICENSE" \
  6a73f9541c2de74158c0e7cf6b0a58ef774f5a780bf191f2d7ec9cc53efe2bf2
tar --no-same-owner -xJf "$build/node.tar.xz" -C "$build"
chmod +x "$build/fnpack"
export PATH="$build/node-v24.19.0-linux-x64/bin:$PATH"

cd "$root/web"
CI=true corepack pnpm@10.33.0 install --frozen-lockfile
corepack pnpm@10.33.0 build
cd "$root"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64=v1 \
  go build -mod=readonly -trimpath -ldflags='-s -w' -o "$build/miyabi" ./cmd

engine=$(node --input-type=module -e 'import {revision} from "./extensions/mdcz-bridge/contract.mjs"; console.log(revision)')
git init "$build/mdcz"
git -C "$build/mdcz" remote add origin https://github.com/ShotHeadman/mdcz.git
git -C "$build/mdcz" fetch --depth 1 origin "$engine"
git -C "$build/mdcz" checkout --detach FETCH_HEAD
cp -R "$root/extensions/mdcz-bridge" "$build/mdcz/apps/miyabi-bridge"
cd "$build/mdcz"
CI=true ELECTRON_SKIP_BINARY_DOWNLOAD=1 SKIP_INSTALL_SIMPLE_GIT_HOOKS=1 \
  corepack pnpm@10.28.2 --filter @mdcz/server... install --frozen-lockfile
corepack pnpm@10.28.2 --filter @mdcz/server exec vite build --config ../miyabi-bridge/vite.config.ts
node apps/miyabi-bridge/stage.mjs
# Install the standalone lock outside MDCz's workspace; otherwise pnpm prunes
# the workspace and the delivered directory never receives its dependencies.
mv bridge-staging "$build/bridge-runtime"
cd "$build/bridge-runtime"
CI=true corepack pnpm@10.28.2 install --prod --frozen-lockfile
cd "$root"
python3 packaging/fnos/prepare.py --miyabi "$build/miyabi" --bridge "$build/bridge-runtime" \
  --node "$build/node-v24.19.0-linux-x64" --fonts "$build/fonts" \
  --certs /etc/ssl/certs/ca-certificates.crt --version "$version" \
  --os-min-version 1.0.0 --output "$build/fpk-stage"
node packaging/fnos/smoke.mjs "$build/fpk-stage/app"
cd "$build"
"$build/fnpack" build --directory "$build/fpk-stage"
mapfile -t packages < <(find "$build" -maxdepth 2 -name '*.fpk' -type f)
[[ ${#packages[@]} == 1 ]] || { echo 'Expected one FPK' >&2; exit 1; }
cp "${packages[0]}" "$output/miyabi-native_${version}_linux-amd64_preview.fpk"
cp "$build/fpk-stage/app/bundle.json" "$output/miyabi-native_${version}_bundle.json"
cd "$output"
sha256sum "miyabi-native_${version}_linux-amd64_preview.fpk" "miyabi-native_${version}_bundle.json" > SHA256SUMS
