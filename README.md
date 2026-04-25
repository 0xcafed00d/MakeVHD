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

## Usage

Run the program with a filename and size in megabytes:

```bash
go run . mydisk.img 64
go run . mydisk.vhd 64
```

To request a standard DOS floppy image, use an `.img` filename with `--floppy`:

```bash
go run . floppy.img --floppy 1440k
go run . floppy.img --floppy 1.44m
go run . floppy.img --floppy 3.5hd
```

The floppy command-line form is accepted by the CLI, but the floppy image creation backend is currently a stub and returns `floppy image creation is not implemented`.

Or build the binary first:

```bash
go build .
./makevhd mydisk.vhd 64
./makevhd floppy.img --floppy 1440k
```

Rules:

- filename must end in `.img` or `.vhd`
- maximum size is `2048 MB`
- `.vhd` files must be at least `3 MB`
- floppy images must use `.img`
- floppy presets are selected with `--floppy <preset>` or `--floppy=<preset>`

Supported floppy presets:

| Preset | Common aliases |
| --- | --- |
| `160k` | `160kb` |
| `180k` | `180kb` |
| `320k` | `320kb` |
| `360k` | `360kb`, `5.25dd`, `5.25-dd` |
| `720k` | `720kb`, `3.5dd`, `3.5-dd` |
| `1200k` | `1200kb`, `1.2m`, `1.2mb`, `5.25hd`, `5.25-hd` |
| `1440k` | `1440kb`, `1.44m`, `1.44mb`, `3.5hd`, `3.5-hd` |
| `2880k` | `2880kb`, `2.88m`, `2.88mb`, `3.5ed`, `3.5-ed` |

## Output Behavior

### `.img`

Creates a raw superfloppy image:

- no partition table
- FAT filesystem starts at sector 0
- useful for direct loop mounting on Linux

### DOS floppy `.img`

Creates a raw FAT12 floppy image using one of the standard DOS floppy layouts.

- no partition table
- output filename must end in `.img`
- size and disk geometry come from the named floppy preset
- common size and media aliases are normalized to the canonical preset

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
- FAT formatting is implemented in Go; no external formatter is required.
- `.img` and `.vhd` are intentionally different formats.
- `.vhd` is a disk image with a partition table.
- `.img` is a filesystem image without a partition table.
