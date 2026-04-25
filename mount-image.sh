#!/usr/bin/env bash

set -euo pipefail

usage() {
    cat <<'EOF'
Usage: mount-image.sh <image(.img|.vhd)> <mount-point>

Mounts a makevhd-generated image on Linux.

For .img files:
  - mounts the filesystem directly with loop

For .vhd files:
  - creates a loop device with partition scanning
  - mounts partition 1

Examples:
  sudo ./mount-image.sh ./disk.img /mnt/disk
  sudo ./mount-image.sh ./disk.vhd /mnt/disk
EOF
}

if [[ $# -ne 2 ]]; then
    usage >&2
    exit 1
fi

image_path=$1
mount_point=$2

if [[ $EUID -ne 0 ]]; then
    echo "run this script as root" >&2
    exit 1
fi

if [[ ! -f "$image_path" ]]; then
    echo "image file not found: $image_path" >&2
    exit 1
fi

mkdir -p "$mount_point"

cleanup_on_error() {
    if [[ -n ${loopdev:-} ]]; then
        umount "$mount_point" 2>/dev/null || true
        losetup -d "$loopdev" 2>/dev/null || true
    fi
}

# Older BusyBox losetup lacks the GNU-style --find/--show/--partscan flags,
# so try the newer form first and fall back to -f plus -P.
attach_loop_with_partscan() {
    if loopdev=$(losetup --find --show --partscan "$image_path" 2>/dev/null); then
        printf '%s\n' "$loopdev"
        return 0
    fi

    loopdev=$(losetup -f)
    losetup -P "$loopdev" "$image_path"
    printf '%s\n' "$loopdev"
}

trap cleanup_on_error ERR

case "${image_path##*.}" in
    img|IMG)
        mount -o loop "$image_path" "$mount_point"
        echo "mounted $image_path at $mount_point"
        echo "unmount with: sudo umount \"$mount_point\""
        ;;

    vhd|VHD)
        loopdev=$(attach_loop_with_partscan)
        partdev="${loopdev}p1"

        if [[ ! -b "$partdev" ]]; then
            partx -a "$loopdev" >/dev/null 2>&1 || true
        fi

        for _ in $(seq 1 10); do
            if [[ -b "$partdev" ]]; then
                break
            fi
            sleep 0.2
        done

        if [[ ! -b "$partdev" ]]; then
            echo "partition device not found for $loopdev" >&2
            cleanup_on_error
            exit 1
        fi

        mount "$partdev" "$mount_point"
        echo "mounted $image_path at $mount_point using $partdev"
        echo "unmount with: sudo umount \"$mount_point\" && sudo losetup -d \"$loopdev\""
        ;;

    *)
        echo "unsupported image extension: $image_path" >&2
        exit 1
        ;;
esac

trap - ERR
