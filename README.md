# makevhd

`makevhd` is a small Go command-line tool for creating FAT-formatted disk image files.

It supports two output formats:

- `.img`: a raw "superfloppy" image with the FAT filesystem written directly at sector 0
- `.vhd`: a fixed VHD image with:
  - a raw disk image
  - an MBR
  - one FAT partition
  - a fixed VHD footer appended at the end

The code is split into:

- [main.go](main.go): CLI entrypoint
- [disktools/diskTools.go](disktools/diskTools.go): reusable library code

## Requirements

- Go 1.22+
- `mkfs.fat` or `mkfs.vfat` available on the system path

On Debian/Ubuntu, `mkfs.fat` is usually provided by:

```bash
sudo apt install dosfstools
```

## Usage

Run the program with a filename and size in megabytes:

```bash
go run . mydisk.img 64
go run . mydisk.vhd 64
```

Or build the binary first:

```bash
go build .
./makevhd mydisk.vhd 64
```

Rules:

- filename must end in `.img` or `.vhd`
- maximum size is `2048 MB`
- `.vhd` files must be at least `3 MB`

## Output Behavior

### `.img`

Creates a raw superfloppy image:

- no partition table
- FAT filesystem starts at sector 0
- useful for direct loop mounting on Linux

### `.vhd`

Creates a fixed VHD:

- one MBR partition starting at LBA `2048`
- FAT filesystem is written inside that partition
- fixed VHD footer is appended to the end of the file
- can be mounted by Windows as a VHD

## Scripts

### `build.sh`

Builds binaries into `dist/` for:

- Linux AMD64 as `dist/makevhd-linux-amd64`
- Linux ARM 32-bit as `dist/makevhd-linux-armv7`
- Linux ARM 64-bit as `dist/makevhd-linux-arm64`
- Windows AMD64 as `dist/makevhd-windows-amd64.exe`

Run:

```bash
./build.sh
```

The build copies both mount helper scripts into `dist/`:

- `mount-image.sh`
- `mount-image.ps1`

### `build.cmd`

Builds the same artifact set on Windows:

- `dist\makevhd-linux-amd64`
- `dist\makevhd-linux-armv7`
- `dist\makevhd-linux-arm64`
- `dist\makevhd-windows-amd64.exe`

Run from `cmd.exe`:

```bat
build.cmd
```

### `mount-image.sh`

Mounts images produced by this project on Linux.

Run:

```bash
sudo ./mount-image.sh ./disk.img /mnt/disk
sudo ./mount-image.sh ./disk.vhd /mnt/disk
```

Behavior:

- `.img`: mounted directly with `mount -o loop`
- `.vhd`: attached with `losetup --partscan` and mounts partition `1`

The script prints the unmount command after a successful mount.

### `mount-image.ps1`

Mounts `.vhd` images produced by this project on Windows using PowerShell and the built-in Storage module.

Run from an elevated PowerShell session:

```powershell
.\mount-image.ps1 .\disk.vhd C:\mnt\disk
```

Behavior:

- `.vhd`: attached with `Mount-DiskImage` and exposed at the requested NTFS folder mount point
- `.img`: not supported natively on Windows and rejected by the script

The script prints the dismount command after a successful mount.

## Testing

Run the test suite with:

```bash
go test ./...
```

## Notes

- FAT type is selected automatically from the image size.
- `.img` and `.vhd` are intentionally different formats.
- `.vhd` is a disk image with a partition table.
- `.img` is a filesystem image without a partition table.
