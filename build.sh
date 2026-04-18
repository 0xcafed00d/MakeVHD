#!/usr/bin/env bash

set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
dist_dir="$repo_root/dist"

native_goos=$(go env GOHOSTOS)
native_goarch=$(go env GOHOSTARCH)

mkdir -p "$dist_dir"

build_target() {
    local name=$1
    shift

    echo "building $name"
    "$@" go build -trimpath -o "$dist_dir/$name" .
}

cd "$repo_root"

build_target "makevhd" env
build_target "makevhd-linux-armv7" env CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7
build_target "makevhd-linux-arm64" env CGO_ENABLED=0 GOOS=linux GOARCH=arm64

echo
echo "native platform: ${native_goos}/${native_goarch}"
echo "artifacts:"
echo "  $dist_dir/makevhd"
echo "  $dist_dir/makevhd-linux-armv7"
echo "  $dist_dir/makevhd-linux-arm64"
