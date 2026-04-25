package disktools

import (
	"encoding/binary"
	"errors"
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

func TestFormatImageInitializesFAT12Entries(t *testing.T) {
	imagePath := filepath.Join(t.TempDir(), "fat12.img")
	if err := CreateImage(imagePath, 8); err != nil {
		t.Fatalf("CreateImage returned error: %v", err)
	}

	if err := FormatImage(imagePath, 8); err != nil {
		t.Fatalf("FormatImage returned error: %v", err)
	}

	assertBytesAt(t, imagePath, int64(fatReservedSectors12_16)*vhdSectorSize, []byte{0xf8, 0xff, 0xff})
}

func TestFormatImageInitializesFAT16Entries(t *testing.T) {
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

	assertBytesAt(t, imagePath, int64(layout.startLBA+fatReservedSectors12_16)*vhdSectorSize, []byte{0xf8, 0xff, 0xff, 0xff})
}

func TestFormatImageInitializesFAT32Metadata(t *testing.T) {
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

	fsInfo := readBytesAt(t, imagePath, int64(layout.startLBA+fat32FSInfoSector)*vhdSectorSize, vhdSectorSize)
	if binary.LittleEndian.Uint32(fsInfo[0:4]) != fatFSInfoLeadSignature {
		t.Fatalf("FSInfo lead signature = %#x, want %#x", binary.LittleEndian.Uint32(fsInfo[0:4]), fatFSInfoLeadSignature)
	}
	if binary.LittleEndian.Uint32(fsInfo[484:488]) != fatFSInfoStructSignature {
		t.Fatalf("FSInfo struct signature = %#x, want %#x", binary.LittleEndian.Uint32(fsInfo[484:488]), fatFSInfoStructSignature)
	}
	if binary.LittleEndian.Uint32(fsInfo[492:496]) != fatRootCluster {
		t.Fatalf("FSInfo next free cluster hint = %d, want %d", binary.LittleEndian.Uint32(fsInfo[492:496]), fatRootCluster)
	}

	assertBytesAt(t, imagePath, int64(layout.startLBA+fatReservedSectors32)*vhdSectorSize, []byte{
		0xf8, 0xff, 0xff, 0x0f,
		0xff, 0xff, 0xff, 0x0f,
		0xff, 0xff, 0xff, 0x0f,
	})
}

func TestFormatImageWritesSuperFloppyBootSector(t *testing.T) {
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

func TestCleanupPartialImageOnErrorRemovesFile(t *testing.T) {
	imagePath := filepath.Join(t.TempDir(), "partial.img")
	if err := os.WriteFile(imagePath, []byte("partial"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	err := errors.New("format failed")
	cleanupPartialImageOnError(imagePath, &err)

	if err == nil {
		t.Fatal("cleanupPartialImageOnError cleared the original error")
	}

	if _, statErr := os.Stat(imagePath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("partial image still exists after cleanup, stat error: %v", statErr)
	}
}

func TestCleanupPartialImageOnErrorKeepsFileWithoutError(t *testing.T) {
	imagePath := filepath.Join(t.TempDir(), "finished.img")
	if err := os.WriteFile(imagePath, []byte("finished"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	var err error
	cleanupPartialImageOnError(imagePath, &err)

	if _, statErr := os.Stat(imagePath); statErr != nil {
		t.Fatalf("finished image was removed, stat error: %v", statErr)
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

func assertBytesAt(t *testing.T, imagePath string, offset int64, want []byte) {
	t.Helper()

	got := readBytesAt(t, imagePath, offset, len(want))
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("byte at offset %d = %#x, want %#x", offset+int64(i), got[i], want[i])
		}
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
