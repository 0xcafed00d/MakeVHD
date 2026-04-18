package disktools

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCreateImage(t *testing.T) {
	imagePath := filepath.Join(t.TempDir(), "disk.img")

	if err := CreateImage(imagePath, 8); err != nil {
		t.Fatalf("CreateImage returned error: %v", err)
	}

	info, err := os.Stat(imagePath)
	if err != nil {
		t.Fatalf("Stat returned error: %v", err)
	}

	const wantSize = int64(8 * bytesPerMB)
	if info.Size() != wantSize {
		t.Fatalf("image size = %d, want %d", info.Size(), wantSize)
	}
}

func TestCreateImageRejectsInvalidSize(t *testing.T) {
	imagePath := filepath.Join(t.TempDir(), "disk.img")

	if err := CreateImage(imagePath, 0); err == nil {
		t.Fatal("CreateImage returned nil error for invalid size")
	}
}

func TestCreateImageRejectsInvalidExtension(t *testing.T) {
	imagePath := filepath.Join(t.TempDir(), "disk.raw")

	if err := CreateImage(imagePath, 8); err == nil {
		t.Fatal("CreateImage returned nil error for invalid filename extension")
	}
}

func TestCreateImageRejectsVHDBelowMinimumSize(t *testing.T) {
	imagePath := filepath.Join(t.TempDir(), "disk.vhd")

	if err := CreateImage(imagePath, minVHDSizeMB-1); err == nil {
		t.Fatal("CreateImage returned nil error for VHD below minimum size")
	}
}

func TestCreateImageAcceptsMaximumSize(t *testing.T) {
	imagePath := filepath.Join(t.TempDir(), "disk.img")

	if err := CreateImage(imagePath, maxImageSizeMB); err != nil {
		t.Fatalf("CreateImage returned error at max size: %v", err)
	}

	info, err := os.Stat(imagePath)
	if err != nil {
		t.Fatalf("Stat returned error: %v", err)
	}

	const wantSize = int64(maxImageSizeMB * bytesPerMB)
	if info.Size() != wantSize {
		t.Fatalf("image size = %d, want %d", info.Size(), wantSize)
	}
}

func TestCreateImageRejectsSizeAboveMaximum(t *testing.T) {
	imagePath := filepath.Join(t.TempDir(), "disk.img")

	if err := CreateImage(imagePath, maxImageSizeMB+1); err == nil {
		t.Fatal("CreateImage returned nil error for size above maximum")
	}
}

func TestFormatImageWritesFAT12BootSector(t *testing.T) {
	requireMkfsFAT(t)

	imagePath := filepath.Join(t.TempDir(), "fat12.vhd")
	if err := CreateImage(imagePath, 8); err != nil {
		t.Fatalf("CreateImage returned error: %v", err)
	}

	if err := writeMBR(imagePath, 8); err != nil {
		t.Fatalf("writeMBR returned error: %v", err)
	}

	if err := FormatImage(imagePath, 8); err != nil {
		t.Fatalf("FormatImage returned error: %v", err)
	}

	layout, err := layoutForVHD(8)
	if err != nil {
		t.Fatalf("layoutForVHD returned error: %v", err)
	}

	assertFilesystemTypeAtLBA(t, imagePath, int64(layout.startLBA), 54, "FAT12   ")
	assertBootSignatureAtLBA(t, imagePath, int64(layout.startLBA))
	assertHiddenSectors(t, imagePath, int64(layout.startLBA), layout.startLBA)
}

func TestFormatImageWritesFAT16BootSector(t *testing.T) {
	requireMkfsFAT(t)

	imagePath := filepath.Join(t.TempDir(), "fat16.vhd")
	if err := CreateImage(imagePath, 64); err != nil {
		t.Fatalf("CreateImage returned error: %v", err)
	}

	if err := writeMBR(imagePath, 64); err != nil {
		t.Fatalf("writeMBR returned error: %v", err)
	}

	if err := FormatImage(imagePath, 64); err != nil {
		t.Fatalf("FormatImage returned error: %v", err)
	}

	layout, err := layoutForVHD(64)
	if err != nil {
		t.Fatalf("layoutForVHD returned error: %v", err)
	}

	assertFilesystemTypeAtLBA(t, imagePath, int64(layout.startLBA), 54, "FAT16   ")
	assertBootSignatureAtLBA(t, imagePath, int64(layout.startLBA))
	assertHiddenSectors(t, imagePath, int64(layout.startLBA), layout.startLBA)
}

func TestFormatImageWritesFAT32BootSector(t *testing.T) {
	requireMkfsFAT(t)

	imagePath := filepath.Join(t.TempDir(), "fat32.vhd")
	if err := CreateImage(imagePath, 514); err != nil {
		t.Fatalf("CreateImage returned error: %v", err)
	}

	if err := writeMBR(imagePath, 514); err != nil {
		t.Fatalf("writeMBR returned error: %v", err)
	}

	if err := FormatImage(imagePath, 514); err != nil {
		t.Fatalf("FormatImage returned error: %v", err)
	}

	layout, err := layoutForVHD(514)
	if err != nil {
		t.Fatalf("layoutForVHD returned error: %v", err)
	}

	assertFilesystemTypeAtLBA(t, imagePath, int64(layout.startLBA), 82, "FAT32   ")
	assertBootSignatureAtLBA(t, imagePath, int64(layout.startLBA))
	assertHiddenSectors(t, imagePath, int64(layout.startLBA), layout.startLBA)
}

func TestFormatImageFallsBackWhenMkfsOffsetUnsupported(t *testing.T) {
	requireMkfsFAT(t)

	imagePath := filepath.Join(t.TempDir(), "fallback.vhd")
	if err := CreateImage(imagePath, 8); err != nil {
		t.Fatalf("CreateImage returned error: %v", err)
	}

	if err := writeMBR(imagePath, 8); err != nil {
		t.Fatalf("writeMBR returned error: %v", err)
	}

	layout, err := layoutForVHD(8)
	if err != nil {
		t.Fatalf("layoutForVHD returned error: %v", err)
	}

	mkfsPath, err := findMkfsFAT()
	if err != nil {
		t.Fatalf("findMkfsFAT returned error: %v", err)
	}

	fakeMkfsPath := filepath.Join(t.TempDir(), "mkfs-fat-fallback.sh")
	script := "#!/usr/bin/env bash\n" +
		"for arg in \"$@\"; do\n" +
		"    if [[ \"$arg\" == \"--offset\" || \"$arg\" == --offset=* ]]; then\n" +
		"        echo \"/usr/sbin/mkfs.fat: unrecognized option '--offset'\" >&2\n" +
		"        echo \"Unknown option: ?\" >&2\n" +
		"        exit 1\n" +
		"    fi\n" +
		"done\n" +
		"exec \"" + mkfsPath + "\" \"$@\"\n"
	if err := os.WriteFile(fakeMkfsPath, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	if err := formatPartitionedVHD(imagePath, fakeMkfsPath, layout); err != nil {
		t.Fatalf("formatPartitionedVHD returned error: %v", err)
	}

	assertFilesystemTypeAtLBA(t, imagePath, int64(layout.startLBA), 54, "FAT12   ")
	assertBootSignatureAtLBA(t, imagePath, int64(layout.startLBA))
	assertHiddenSectors(t, imagePath, int64(layout.startLBA), layout.startLBA)
}

func TestFormatImageFallbackCopiesOnlyMetadata(t *testing.T) {
	requireMkfsFAT(t)

	imagePath := filepath.Join(t.TempDir(), "metadata-only.vhd")
	if err := CreateImage(imagePath, 8); err != nil {
		t.Fatalf("CreateImage returned error: %v", err)
	}

	if err := writeMBR(imagePath, 8); err != nil {
		t.Fatalf("writeMBR returned error: %v", err)
	}

	layout, err := layoutForVHD(8)
	if err != nil {
		t.Fatalf("layoutForVHD returned error: %v", err)
	}

	const markerOffset = int64(2 * bytesPerMB)
	marker := bytesRepeated(0xaa, 64)
	file, err := os.OpenFile(imagePath, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("OpenFile returned error: %v", err)
	}
	if _, err := file.WriteAt(marker, int64(layout.startLBA)*vhdSectorSize+markerOffset); err != nil {
		file.Close()
		t.Fatalf("WriteAt returned error: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	mkfsPath, err := findMkfsFAT()
	if err != nil {
		t.Fatalf("findMkfsFAT returned error: %v", err)
	}

	fakeMkfsPath := filepath.Join(t.TempDir(), "mkfs-fat-fallback-metadata.sh")
	script := "#!/usr/bin/env bash\n" +
		"for arg in \"$@\"; do\n" +
		"    if [[ \"$arg\" == \"--offset\" || \"$arg\" == --offset=* ]]; then\n" +
		"        echo \"/usr/sbin/mkfs.fat: unrecognized option '--offset'\" >&2\n" +
		"        echo \"Unknown option: ?\" >&2\n" +
		"        exit 1\n" +
		"    fi\n" +
		"done\n" +
		"exec \"" + mkfsPath + "\" \"$@\"\n"
	if err := os.WriteFile(fakeMkfsPath, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	if err := formatPartitionedVHD(imagePath, fakeMkfsPath, layout); err != nil {
		t.Fatalf("formatPartitionedVHD returned error: %v", err)
	}

	got := readBytesAt(t, imagePath, int64(layout.startLBA)*vhdSectorSize+markerOffset, len(marker))
	if string(got) != string(marker) {
		t.Fatal("fallback overwrote data area beyond filesystem metadata")
	}
}

func TestFormatImageWritesSuperFloppyBootSector(t *testing.T) {
	requireMkfsFAT(t)

	imagePath := filepath.Join(t.TempDir(), "disk.img")
	if err := CreateImage(imagePath, 8); err != nil {
		t.Fatalf("CreateImage returned error: %v", err)
	}

	if err := FormatImage(imagePath, 8); err != nil {
		t.Fatalf("FormatImage returned error: %v", err)
	}

	assertFilesystemTypeAtLBA(t, imagePath, 0, 54, "FAT12   ")
	assertBootSignatureAtLBA(t, imagePath, 0)
	assertHiddenSectors(t, imagePath, 0, 0)
}

func TestFormatImageRejectsUnexpectedFileSize(t *testing.T) {
	requireMkfsFAT(t)

	imagePath := filepath.Join(t.TempDir(), "mismatch.vhd")
	if err := os.WriteFile(imagePath, make([]byte, 1024), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	if err := FormatImage(imagePath, 8); err == nil {
		t.Fatal("FormatImage returned nil error for mismatched file size")
	}
}

func TestConvertToVHDAppendsValidFooter(t *testing.T) {
	imagePath := filepath.Join(t.TempDir(), "disk.vhd")
	if err := CreateImage(imagePath, 8); err != nil {
		t.Fatalf("CreateImage returned error: %v", err)
	}

	if err := ConvertToVHD(imagePath); err != nil {
		t.Fatalf("ConvertToVHD returned error: %v", err)
	}

	info, err := os.Stat(imagePath)
	if err != nil {
		t.Fatalf("Stat returned error: %v", err)
	}

	const rawSize = int64(8 * bytesPerMB)
	if info.Size() != rawSize+vhdFooterSize {
		t.Fatalf("file size = %d, want %d", info.Size(), rawSize+vhdFooterSize)
	}

	footer := readFooter(t, imagePath)
	if string(footer[0:8]) != vhdFooterCookie {
		t.Fatalf("footer cookie = %q, want %q", string(footer[0:8]), vhdFooterCookie)
	}

	if binary.BigEndian.Uint32(footer[8:12]) != vhdFeatures {
		t.Fatalf("footer features = %#x, want %#x", binary.BigEndian.Uint32(footer[8:12]), vhdFeatures)
	}

	if binary.BigEndian.Uint64(footer[16:24]) != vhdDataOffsetNone {
		t.Fatalf("footer data offset = %#x, want %#x", binary.BigEndian.Uint64(footer[16:24]), vhdDataOffsetNone)
	}

	if binary.BigEndian.Uint64(footer[40:48]) != uint64(rawSize) {
		t.Fatalf("footer original size = %d, want %d", binary.BigEndian.Uint64(footer[40:48]), rawSize)
	}

	if binary.BigEndian.Uint64(footer[48:56]) != uint64(rawSize) {
		t.Fatalf("footer current size = %d, want %d", binary.BigEndian.Uint64(footer[48:56]), rawSize)
	}

	if binary.BigEndian.Uint32(footer[60:64]) != vhdDiskTypeFixed {
		t.Fatalf("footer disk type = %d, want %d", binary.BigEndian.Uint32(footer[60:64]), vhdDiskTypeFixed)
	}

	if !isValidVHDFooter(footer[:]) {
		t.Fatal("footer checksum validation failed")
	}
}

func TestConvertToVHDRejectsExistingFooter(t *testing.T) {
	imagePath := filepath.Join(t.TempDir(), "disk.vhd")
	if err := CreateImage(imagePath, 8); err != nil {
		t.Fatalf("CreateImage returned error: %v", err)
	}

	if err := ConvertToVHD(imagePath); err != nil {
		t.Fatalf("ConvertToVHD returned error: %v", err)
	}

	if err := ConvertToVHD(imagePath); err == nil {
		t.Fatal("ConvertToVHD returned nil error for an existing VHD footer")
	}
}

func TestConvertToVHDRejectsIMGExtension(t *testing.T) {
	imagePath := filepath.Join(t.TempDir(), "disk.img")
	if err := CreateImage(imagePath, 8); err != nil {
		t.Fatalf("CreateImage returned error: %v", err)
	}

	if err := ConvertToVHD(imagePath); err == nil {
		t.Fatal("ConvertToVHD returned nil error for .img extension")
	}
}

func TestMakeVHDCreatesFormattedIMG(t *testing.T) {
	requireMkfsFAT(t)

	imagePath := filepath.Join(t.TempDir(), "disk.img")
	if err := MakeVHD(imagePath, 8); err != nil {
		t.Fatalf("MakeVHD returned error: %v", err)
	}

	info, err := os.Stat(imagePath)
	if err != nil {
		t.Fatalf("Stat returned error: %v", err)
	}

	const rawSize = int64(8 * bytesPerMB)
	if info.Size() != rawSize {
		t.Fatalf(".img file size = %d, want %d", info.Size(), rawSize)
	}

	assertFilesystemTypeAtLBA(t, imagePath, 0, 54, "FAT12   ")
	assertBootSignatureAtLBA(t, imagePath, 0)
	assertHiddenSectors(t, imagePath, 0, 0)
}

func TestMakeVHDCreatesVHDWithFooter(t *testing.T) {
	requireMkfsFAT(t)

	imagePath := filepath.Join(t.TempDir(), "disk.vhd")
	if err := MakeVHD(imagePath, 8); err != nil {
		t.Fatalf("MakeVHD returned error: %v", err)
	}

	info, err := os.Stat(imagePath)
	if err != nil {
		t.Fatalf("Stat returned error: %v", err)
	}

	const rawSize = int64(8 * bytesPerMB)
	if info.Size() != rawSize+vhdFooterSize {
		t.Fatalf(".vhd file size = %d, want %d", info.Size(), rawSize+vhdFooterSize)
	}

	layout, err := layoutForVHD(8)
	if err != nil {
		t.Fatalf("layoutForVHD returned error: %v", err)
	}

	assertMBRPartition(t, imagePath, layout)
	assertFilesystemTypeAtLBA(t, imagePath, int64(layout.startLBA), 54, "FAT12   ")
	assertBootSignatureAtLBA(t, imagePath, int64(layout.startLBA))
	assertHiddenSectors(t, imagePath, int64(layout.startLBA), layout.startLBA)

	footer := readFooter(t, imagePath)
	if !isValidVHDFooter(footer[:]) {
		t.Fatal("footer checksum validation failed")
	}
}

func TestMakeVHDRejectsUnsupportedExtension(t *testing.T) {
	imagePath := filepath.Join(t.TempDir(), "disk.raw")

	if err := MakeVHD(imagePath, 8); err == nil {
		t.Fatal("MakeVHD returned nil error for unsupported filename extension")
	}
}

func TestBuildFixedVHDFooterUsesStableFields(t *testing.T) {
	footer, err := buildFixedVHDFooter(8*bytesPerMB, time.Unix(vhdTimestampBaseUnix+123, 0).UTC())
	if err != nil {
		t.Fatalf("buildFixedVHDFooter returned error: %v", err)
	}

	if string(footer[28:32]) != vhdCreatorApp {
		t.Fatalf("creator app = %q, want %q", string(footer[28:32]), vhdCreatorApp)
	}

	if string(footer[36:40]) != vhdCreatorHostOS {
		t.Fatalf("creator host OS = %q, want %q", string(footer[36:40]), vhdCreatorHostOS)
	}

	if binary.BigEndian.Uint32(footer[24:28]) != 123 {
		t.Fatalf("footer timestamp = %d, want 123", binary.BigEndian.Uint32(footer[24:28]))
	}

	if !isValidVHDFooter(footer[:]) {
		t.Fatal("footer checksum validation failed")
	}
}

func TestMkfsOffsetUnsupported(t *testing.T) {
	output := []byte("/usr/sbin/mkfs.fat: unrecognized option '--offset'\nUnknown option: ?\n")
	if !mkfsOffsetUnsupported(output) {
		t.Fatal("mkfsOffsetUnsupported returned false for unsupported --offset output")
	}

	if mkfsOffsetUnsupported([]byte("some other mkfs error")) {
		t.Fatal("mkfsOffsetUnsupported returned true for unrelated error output")
	}
}

func TestReadFATMetadataLayoutAt(t *testing.T) {
	requireMkfsFAT(t)

	imagePath := filepath.Join(t.TempDir(), "layout.vhd")
	if err := CreateImage(imagePath, 8); err != nil {
		t.Fatalf("CreateImage returned error: %v", err)
	}

	if err := writeMBR(imagePath, 8); err != nil {
		t.Fatalf("writeMBR returned error: %v", err)
	}

	if err := FormatImage(imagePath, 8); err != nil {
		t.Fatalf("FormatImage returned error: %v", err)
	}

	layout, err := layoutForVHD(8)
	if err != nil {
		t.Fatalf("layoutForVHD returned error: %v", err)
	}

	file, err := os.Open(imagePath)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer file.Close()

	metadata, err := readFATMetadataLayoutAt(file, int64(layout.startLBA)*vhdSectorSize, layout.fatBits)
	if err != nil {
		t.Fatalf("readFATMetadataLayoutAt returned error: %v", err)
	}

	if metadata.bytesPerSector != 512 {
		t.Fatalf("bytes per sector = %d, want 512", metadata.bytesPerSector)
	}

	if len(metadata.regions) != 1 {
		t.Fatalf("metadata region count = %d, want 1", len(metadata.regions))
	}

	if metadata.regions[0].offset != 0 {
		t.Fatalf("metadata region offset = %d, want 0", metadata.regions[0].offset)
	}
}

func requireMkfsFAT(t *testing.T) {
	t.Helper()

	if _, err := findMkfsFAT(); err != nil {
		t.Skipf("skipping test: %v", err)
	}
}

func assertFilesystemTypeAtLBA(t *testing.T, imagePath string, lba int64, offset int64, want string) {
	t.Helper()

	got := readBytesAt(t, imagePath, lba*vhdSectorSize+offset, len(want))

	if string(got) != want {
		t.Fatalf("filesystem type = %q, want %q", string(got), want)
	}
}

func assertBootSignatureAtLBA(t *testing.T, imagePath string, lba int64) {
	t.Helper()

	signature := readBytesAt(t, imagePath, lba*vhdSectorSize+510, 2)

	if signature[0] != 0x55 || signature[1] != 0xaa {
		t.Fatalf("boot signature = %x %x, want 55 aa", signature[0], signature[1])
	}
}

func assertHiddenSectors(t *testing.T, imagePath string, lba int64, want uint32) {
	t.Helper()

	raw := readBytesAt(t, imagePath, lba*vhdSectorSize+28, 4)
	got := binary.LittleEndian.Uint32(raw)
	if got != want {
		t.Fatalf("hidden sectors = %d, want %d", got, want)
	}
}

func assertMBRPartition(t *testing.T, imagePath string, layout partitionLayout) {
	t.Helper()

	sector0 := readBytesAt(t, imagePath, 0, mbrSize)
	if sector0[mbrSignatureOffset] != 0x55 || sector0[mbrSignatureOffset+1] != 0xaa {
		t.Fatalf("MBR signature = %x %x, want 55 aa", sector0[mbrSignatureOffset], sector0[mbrSignatureOffset+1])
	}

	entry := sector0[mbrPartitionTableOffset : mbrPartitionTableOffset+mbrPartitionEntrySize]
	if entry[4] != layout.partitionType {
		t.Fatalf("partition type = %#x, want %#x", entry[4], layout.partitionType)
	}

	startLBA := binary.LittleEndian.Uint32(entry[8:12])
	if startLBA != layout.startLBA {
		t.Fatalf("partition start LBA = %d, want %d", startLBA, layout.startLBA)
	}

	sectorCount := binary.LittleEndian.Uint32(entry[12:16])
	if sectorCount != layout.partitionSectors {
		t.Fatalf("partition sector count = %d, want %d", sectorCount, layout.partitionSectors)
	}
}

func readBytesAt(t *testing.T, imagePath string, offset int64, size int) []byte {
	t.Helper()

	file, err := os.Open(imagePath)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer file.Close()

	buf := make([]byte, size)
	if _, err := file.ReadAt(buf, offset); err != nil {
		t.Fatalf("ReadAt returned error: %v", err)
	}

	return buf
}

func bytesRepeated(value byte, count int) []byte {
	buf := make([]byte, count)
	for i := range buf {
		buf[i] = value
	}
	return buf
}

func readFooter(t *testing.T, imagePath string) [vhdFooterSize]byte {
	t.Helper()

	file, err := os.Open(imagePath)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		t.Fatalf("Stat returned error: %v", err)
	}

	var footer [vhdFooterSize]byte
	if _, err := file.ReadAt(footer[:], info.Size()-vhdFooterSize); err != nil {
		t.Fatalf("ReadAt returned error: %v", err)
	}

	return footer
}
