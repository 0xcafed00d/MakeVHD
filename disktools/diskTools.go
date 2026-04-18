package disktools

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const bytesPerMB = 1024 * 1024
const maxImageSizeMB = 2048
const maxFAT12SizeMB = 16
const maxFAT16SizeMB = 512
const minVHDSizeMB = 3
const imageExtIMG = ".img"
const imageExtVHD = ".vhd"
const mbrSize = 512
const mbrDiskSignatureOffset = 440
const mbrPartitionTableOffset = 446
const mbrPartitionEntrySize = 16
const mbrSignatureOffset = 510
const vhdPartitionStartLBA = 2048
const partitionTypeFAT12 = 0x01
const partitionTypeFAT16LBA = 0x0e
const partitionTypeFAT32LBA = 0x0c
const vhdFooterSize = 512
const vhdSectorSize = 512
const vhdFooterCookie = "conectix"
const vhdCreatorApp = "mkvh"
const vhdCreatorHostOS = "Wi2k"
const vhdFeatures = 0x00000002
const vhdFileFormatVersion = 0x00010000
const vhdDataOffsetNone = ^uint64(0)
const vhdCreatorVersion = 0x00010000
const vhdDiskTypeFixed = 2
const vhdTimestampBaseUnix = 946684800
const vhdMaxCHSCylinders = 65535
const vhdMaxCHSHeads = 16
const vhdMaxCHSSectors = 255
const vhdMaxGeometrySectors = vhdMaxCHSCylinders * vhdMaxCHSHeads * vhdMaxCHSSectors

type partitionLayout struct {
	rawSize          int64
	totalSectors     uint32
	startLBA         uint32
	partitionSectors uint32
	fatBits          int
	partitionType    byte
}

type fileRegion struct {
	offset int64
	length int64
}

type fatMetadataLayout struct {
	bytesPerSector    uint16
	sectorsPerCluster uint8
	regions           []fileRegion
}

func MakeVHD(filename string, size int) error {
	imageType, err := imageTypeFromFilename(filename)
	if err != nil {
		return err
	}

	if err := CreateImage(filename, size); err != nil {
		return fmt.Errorf("CreateImage: %w", err)
	}

	if imageType == imageExtIMG {
		if err := FormatImage(filename, size); err != nil {
			return fmt.Errorf("FormatImage: %w", err)
		}
		return nil
	}

	if err := writeMBR(filename, size); err != nil {
		return fmt.Errorf("writeMBR: %w", err)
	}

	if err := FormatImage(filename, size); err != nil {
		return fmt.Errorf("FormatImage: %w", err)
	}

	if err := ConvertToVHD(filename); err != nil {
		return fmt.Errorf("ConvertToVHD: %w", err)
	}

	return nil
}

// create a blank disk image file of the specified size (in MB)
// the image will be formatted as a raw disk image (i.e. no partition table or filesystem)
func CreateImage(filename string, size int) (err error) {
	if err := validateImageSpec(filename, size); err != nil {
		return err
	}

	sizeMB := int64(size)

	file, err := os.OpenFile(filename, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("create image file %q: %w", filename, err)
	}

	defer func() {
		if closeErr := file.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("close image file %q: %w", filename, closeErr)
		}
	}()

	if err := file.Truncate(sizeMB * bytesPerMB); err != nil {
		return fmt.Errorf("set image size to %d MB: %w", size, err)
	}

	return nil
}

// FormatImage writes a FAT filesystem into an existing raw disk image.
func FormatImage(filename string, size int) error {
	if err := validateImageSpec(filename, size); err != nil {
		return err
	}

	imageType, err := imageTypeFromFilename(filename)
	if err != nil {
		return err
	}

	info, err := os.Stat(filename)
	if err != nil {
		return fmt.Errorf("stat image file %q: %w", filename, err)
	}

	if !info.Mode().IsRegular() {
		return fmt.Errorf("image file %q is not a regular file", filename)
	}

	mkfsPath, err := findMkfsFAT()
	if err != nil {
		return err
	}

	wantSize := int64(size) * bytesPerMB
	args := []string{"--invariant"}

	switch imageType {
	case imageExtIMG:
		if info.Size() != wantSize {
			return fmt.Errorf("image file %q has size %d bytes, want %d bytes", filename, info.Size(), wantSize)
		}

		args = append(args,
			"-F", fmt.Sprintf("%d", fatBitsForSize(size)),
			filename,
		)
	case imageExtVHD:
		layout, err := layoutForVHD(size)
		if err != nil {
			return err
		}

		if info.Size() != layout.rawSize {
			return fmt.Errorf("image file %q has size %d bytes, want %d bytes", filename, info.Size(), layout.rawSize)
		}

		if err := formatPartitionedVHD(filename, mkfsPath, layout); err != nil {
			return fmt.Errorf("format image %q: %w", filename, err)
		}
		return nil
	default:
		return fmt.Errorf("unsupported image type %q", imageType)
	}

	cmd := exec.Command(mkfsPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("format image %q: %w: %s", filename, err, output)
	}

	return nil
}

func formatPartitionedVHD(filename, mkfsPath string, layout partitionLayout) error {
	args := []string{
		"--invariant",
		"-F", fmt.Sprintf("%d", layout.fatBits),
		"--offset", fmt.Sprintf("%d", layout.startLBA),
		"-h", fmt.Sprintf("%d", layout.startLBA),
		filename,
	}

	output, err := runMkfsCommand(mkfsPath, args)
	if err == nil {
		return nil
	}

	if !mkfsOffsetUnsupported(output) {
		return fmt.Errorf("%w: %s", err, output)
	}

	if err := formatPartitionedVHDFallback(filename, mkfsPath, layout); err != nil {
		return fmt.Errorf("mkfs.fat without --offset fallback failed: %w", err)
	}

	return nil
}

func formatPartitionedVHDFallback(filename, mkfsPath string, layout partitionLayout) (err error) {
	partitionSize := int64(layout.partitionSectors) * vhdSectorSize

	tempFile, err := os.CreateTemp("", "makevhd-partition-*.img")
	if err != nil {
		return fmt.Errorf("create temp filesystem image: %w", err)
	}

	tempPath := tempFile.Name()
	defer func() {
		tempFile.Close()
		os.Remove(tempPath)
	}()

	if err := tempFile.Truncate(partitionSize); err != nil {
		return fmt.Errorf("size temp filesystem image: %w", err)
	}

	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close temp filesystem image: %w", err)
	}

	args := []string{
		"--invariant",
		"-F", fmt.Sprintf("%d", layout.fatBits),
		"-h", fmt.Sprintf("%d", layout.startLBA),
		tempPath,
	}

	output, err := runMkfsCommand(mkfsPath, args)
	if err != nil {
		return fmt.Errorf("%w: %s", err, output)
	}

	source, err := os.Open(tempPath)
	if err != nil {
		return fmt.Errorf("open temp filesystem image: %w", err)
	}
	defer source.Close()

	metadata, err := readFATMetadataLayoutAt(source, 0, layout.fatBits)
	if err != nil {
		return fmt.Errorf("read FAT metadata layout: %w", err)
	}

	target, err := os.OpenFile(filename, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open target image %q: %w", filename, err)
	}
	defer target.Close()

	if err := copyFileRegions(target, source, int64(layout.startLBA)*vhdSectorSize, metadata.regions); err != nil {
		return fmt.Errorf("copy filesystem metadata into partition: %w", err)
	}

	return nil
}

func runMkfsCommand(mkfsPath string, args []string) ([]byte, error) {
	cmd := exec.Command(mkfsPath, args...)
	output, err := cmd.CombinedOutput()
	return output, err
}

func mkfsOffsetUnsupported(output []byte) bool {
	text := strings.ToLower(string(output))
	return strings.Contains(text, "--offset") &&
		(strings.Contains(text, "unrecognized option") ||
			strings.Contains(text, "unknown option"))
}

func readFATMetadataLayoutAt(file *os.File, volumeOffset int64, fatBits int) (fatMetadataLayout, error) {
	var layout fatMetadataLayout
	var boot [512]byte

	if _, err := file.ReadAt(boot[:], volumeOffset); err != nil {
		return layout, err
	}

	bytesPerSector := binary.LittleEndian.Uint16(boot[11:13])
	if bytesPerSector == 0 {
		return layout, errors.New("invalid FAT boot sector: bytes per sector is 0")
	}

	sectorsPerCluster := boot[13]
	if sectorsPerCluster == 0 {
		return layout, errors.New("invalid FAT boot sector: sectors per cluster is 0")
	}

	reservedSectors := uint32(binary.LittleEndian.Uint16(boot[14:16]))
	fatCount := uint32(boot[16])
	if fatCount == 0 {
		return layout, errors.New("invalid FAT boot sector: FAT count is 0")
	}

	var sectorsPerFAT uint32
	switch fatBits {
	case 12, 16:
		sectorsPerFAT = uint32(binary.LittleEndian.Uint16(boot[22:24]))
	case 32:
		sectorsPerFAT = binary.LittleEndian.Uint32(boot[36:40])
	default:
		return layout, fmt.Errorf("unsupported FAT size %d", fatBits)
	}
	if sectorsPerFAT == 0 {
		return layout, errors.New("invalid FAT boot sector: sectors per FAT is 0")
	}

	layout.bytesPerSector = bytesPerSector
	layout.sectorsPerCluster = sectorsPerCluster

	regions := []fileRegion{
		{
			offset: 0,
			length: int64(reservedSectors) * int64(bytesPerSector),
		},
		{
			offset: int64(reservedSectors) * int64(bytesPerSector),
			length: int64(fatCount*sectorsPerFAT) * int64(bytesPerSector),
		},
	}

	switch fatBits {
	case 12, 16:
		rootEntries := uint32(binary.LittleEndian.Uint16(boot[17:19]))
		rootDirSectors := (rootEntries*32 + uint32(bytesPerSector) - 1) / uint32(bytesPerSector)
		rootDirOffset := int64(reservedSectors+fatCount*sectorsPerFAT) * int64(bytesPerSector)
		regions = append(regions, fileRegion{
			offset: rootDirOffset,
			length: int64(rootDirSectors) * int64(bytesPerSector),
		})
	case 32:
		rootCluster := binary.LittleEndian.Uint32(boot[44:48])
		if rootCluster < 2 {
			return layout, errors.New("invalid FAT32 boot sector: root cluster is less than 2")
		}

		firstDataSector := reservedSectors + fatCount*sectorsPerFAT
		rootDirSector := firstDataSector + (rootCluster-2)*uint32(sectorsPerCluster)
		regions = append(regions, fileRegion{
			offset: int64(rootDirSector) * int64(bytesPerSector),
			length: int64(sectorsPerCluster) * int64(bytesPerSector),
		})
	}

	layout.regions = mergeFileRegions(regions)
	return layout, nil
}

func mergeFileRegions(regions []fileRegion) []fileRegion {
	if len(regions) == 0 {
		return nil
	}

	merged := []fileRegion{regions[0]}
	for _, region := range regions[1:] {
		last := &merged[len(merged)-1]
		lastEnd := last.offset + last.length
		if region.offset <= lastEnd {
			regionEnd := region.offset + region.length
			if regionEnd > lastEnd {
				last.length = regionEnd - last.offset
			}
			continue
		}

		merged = append(merged, region)
	}

	return merged
}

func copyFileRegions(dst, src *os.File, dstBaseOffset int64, regions []fileRegion) error {
	for _, region := range regions {
		if region.length == 0 {
			continue
		}

		if _, err := src.Seek(region.offset, io.SeekStart); err != nil {
			return err
		}

		if _, err := dst.Seek(dstBaseOffset+region.offset, io.SeekStart); err != nil {
			return err
		}

		written, err := io.CopyN(dst, src, region.length)
		if err != nil {
			return err
		}

		if written != region.length {
			return fmt.Errorf("copied %d bytes for region at offset %d, want %d", written, region.offset, region.length)
		}
	}

	return nil
}

// add the 512 byte VHD footer to the end of the image file
// the footer contains metadata about the disk image, such as its size and geometry, and is required for the image to be mounted on Windows
func ConvertToVHD(filename string) error {
	imageType, err := imageTypeFromFilename(filename)
	if err != nil {
		return err
	}

	if imageType != imageExtVHD {
		return fmt.Errorf("VHD footer can only be written to %q files", imageExtVHD)
	}

	file, err := os.OpenFile(filename, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open image file %q: %w", filename, err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat image file %q: %w", filename, err)
	}

	if !info.Mode().IsRegular() {
		return fmt.Errorf("image file %q is not a regular file", filename)
	}

	rawSize := info.Size()
	if rawSize <= 0 {
		return fmt.Errorf("image file %q is empty", filename)
	}

	maxImageBytes := int64(maxImageSizeMB) * bytesPerMB
	if rawSize > maxImageBytes {
		return fmt.Errorf("image file %q exceeds the maximum raw size of %d MB", filename, maxImageSizeMB)
	}

	if rawSize%vhdSectorSize != 0 {
		return fmt.Errorf("image file %q size %d is not a multiple of %d bytes", filename, rawSize, vhdSectorSize)
	}

	if hasFooter, err := hasValidVHDFooter(file, rawSize); err != nil {
		return fmt.Errorf("inspect image file %q: %w", filename, err)
	} else if hasFooter {
		return fmt.Errorf("image file %q already contains a VHD footer", filename)
	}

	footer, err := buildFixedVHDFooter(rawSize, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("build VHD footer: %w", err)
	}

	if _, err := file.WriteAt(footer[:], rawSize); err != nil {
		return fmt.Errorf("append VHD footer to %q: %w", filename, err)
	}

	return nil
}

func validateImageSpec(filename string, size int) error {
	imageType, err := imageTypeFromFilename(filename)
	if err != nil {
		return err
	}

	if size <= 0 {
		return fmt.Errorf("size must be greater than 0 MB, got %d", size)
	}

	if size > maxImageSizeMB {
		return fmt.Errorf("size must not exceed %d MB, got %d", maxImageSizeMB, size)
	}

	if imageType == imageExtVHD && size < minVHDSizeMB {
		return fmt.Errorf("size for %q files must be at least %d MB, got %d", imageExtVHD, minVHDSizeMB, size)
	}

	return nil
}

func imageTypeFromFilename(filename string) (string, error) {
	if filename == "" {
		return "", errors.New("filename must not be empty")
	}

	switch strings.ToLower(filepath.Ext(filename)) {
	case imageExtIMG:
		return imageExtIMG, nil
	case imageExtVHD:
		return imageExtVHD, nil
	default:
		return "", fmt.Errorf("filename %q must use %q or %q extension", filename, imageExtIMG, imageExtVHD)
	}
}

func fatBitsForSize(size int) int {
	switch {
	case size <= maxFAT12SizeMB:
		return 12
	case size <= maxFAT16SizeMB:
		return 16
	default:
		return 32
	}
}

func findMkfsFAT() (string, error) {
	if path, err := exec.LookPath("mkfs.fat"); err == nil {
		return path, nil
	}

	if path, err := exec.LookPath("mkfs.vfat"); err == nil {
		return path, nil
	}

	return "", errors.New("mkfs.fat or mkfs.vfat is required to format disk images")
}

func layoutForVHD(size int) (partitionLayout, error) {
	var layout partitionLayout

	if size < minVHDSizeMB {
		return layout, fmt.Errorf("size for %q files must be at least %d MB, got %d", imageExtVHD, minVHDSizeMB, size)
	}

	rawSize := int64(size) * bytesPerMB
	totalSectors := rawSize / vhdSectorSize
	if totalSectors <= vhdPartitionStartLBA {
		return layout, fmt.Errorf("disk size %d MB is too small for a partitioned %q image", size, imageExtVHD)
	}

	partitionSectors := totalSectors - vhdPartitionStartLBA
	partitionSizeMB := int((partitionSectors * vhdSectorSize) / bytesPerMB)
	fatBits := fatBitsForSize(partitionSizeMB)

	layout.rawSize = rawSize
	layout.totalSectors = uint32(totalSectors)
	layout.startLBA = vhdPartitionStartLBA
	layout.partitionSectors = uint32(partitionSectors)
	layout.fatBits = fatBits
	layout.partitionType = partitionTypeForFAT(fatBits)
	return layout, nil
}

func partitionTypeForFAT(fatBits int) byte {
	switch fatBits {
	case 12:
		return partitionTypeFAT12
	case 16:
		return partitionTypeFAT16LBA
	default:
		return partitionTypeFAT32LBA
	}
}

func writeMBR(filename string, size int) error {
	layout, err := layoutForVHD(size)
	if err != nil {
		return err
	}

	file, err := os.OpenFile(filename, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open image file %q: %w", filename, err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat image file %q: %w", filename, err)
	}

	if !info.Mode().IsRegular() {
		return fmt.Errorf("image file %q is not a regular file", filename)
	}

	if info.Size() != layout.rawSize {
		return fmt.Errorf("image file %q has size %d bytes, want %d bytes", filename, info.Size(), layout.rawSize)
	}

	var mbr [mbrSize]byte
	if _, err := io.ReadFull(rand.Reader, mbr[mbrDiskSignatureOffset:mbrDiskSignatureOffset+4]); err != nil {
		return fmt.Errorf("generate disk signature: %w", err)
	}

	_, heads, sectorsPerTrack := vhdGeometry(int64(layout.totalSectors))
	entry := mbr[mbrPartitionTableOffset : mbrPartitionTableOffset+mbrPartitionEntrySize]
	entry[0] = 0x00

	startHead, startSector, startCylinder := lbaToCHS(uint32(layout.startLBA), heads, sectorsPerTrack)
	entry[1] = startHead
	entry[2] = startSector
	entry[3] = startCylinder

	entry[4] = layout.partitionType

	endLBA := layout.startLBA + layout.partitionSectors - 1
	endHead, endSector, endCylinder := lbaToCHS(uint32(endLBA), heads, sectorsPerTrack)
	entry[5] = endHead
	entry[6] = endSector
	entry[7] = endCylinder

	binary.LittleEndian.PutUint32(entry[8:12], layout.startLBA)
	binary.LittleEndian.PutUint32(entry[12:16], layout.partitionSectors)
	mbr[mbrSignatureOffset] = 0x55
	mbr[mbrSignatureOffset+1] = 0xaa

	if _, err := file.WriteAt(mbr[:], 0); err != nil {
		return fmt.Errorf("write MBR to %q: %w", filename, err)
	}

	return nil
}

func buildFixedVHDFooter(rawSize int64, now time.Time) ([vhdFooterSize]byte, error) {
	var footer [vhdFooterSize]byte

	if rawSize <= 0 {
		return footer, errors.New("raw image size must be greater than 0")
	}

	if rawSize%vhdSectorSize != 0 {
		return footer, fmt.Errorf("raw image size %d is not a multiple of %d bytes", rawSize, vhdSectorSize)
	}

	copy(footer[0:8], vhdFooterCookie)
	binary.BigEndian.PutUint32(footer[8:12], vhdFeatures)
	binary.BigEndian.PutUint32(footer[12:16], vhdFileFormatVersion)
	binary.BigEndian.PutUint64(footer[16:24], vhdDataOffsetNone)
	binary.BigEndian.PutUint32(footer[24:28], vhdTimestamp(now))
	copy(footer[28:32], vhdCreatorApp)
	binary.BigEndian.PutUint32(footer[32:36], vhdCreatorVersion)
	copy(footer[36:40], vhdCreatorHostOS)
	binary.BigEndian.PutUint64(footer[40:48], uint64(rawSize))
	binary.BigEndian.PutUint64(footer[48:56], uint64(rawSize))

	cylinders, heads, sectorsPerTrack := vhdGeometry(rawSize / vhdSectorSize)
	binary.BigEndian.PutUint16(footer[56:58], cylinders)
	footer[58] = heads
	footer[59] = sectorsPerTrack

	binary.BigEndian.PutUint32(footer[60:64], vhdDiskTypeFixed)

	if _, err := io.ReadFull(rand.Reader, footer[68:84]); err != nil {
		return footer, fmt.Errorf("generate unique ID: %w", err)
	}

	binary.BigEndian.PutUint32(footer[64:68], vhdChecksum(footer[:]))
	return footer, nil
}

func lbaToCHS(lba uint32, heads uint8, sectorsPerTrack uint8) (byte, byte, byte) {
	if heads == 0 || sectorsPerTrack == 0 {
		return 0, 0, 0
	}

	sectorsPerCylinder := uint32(heads) * uint32(sectorsPerTrack)
	cylinder := lba / sectorsPerCylinder
	temp := lba % sectorsPerCylinder
	head := temp / uint32(sectorsPerTrack)
	sector := (temp % uint32(sectorsPerTrack)) + 1

	if cylinder > 1023 {
		return 0xfe, 0xff, 0xff
	}

	sectorByte := byte((sector & 0x3f) | ((cylinder >> 2) & 0xc0))
	cylinderByte := byte(cylinder & 0xff)
	return byte(head), sectorByte, cylinderByte
}

func hasValidVHDFooter(file *os.File, size int64) (bool, error) {
	if size < vhdFooterSize {
		return false, nil
	}

	var footer [vhdFooterSize]byte
	if _, err := file.ReadAt(footer[:], size-vhdFooterSize); err != nil {
		return false, err
	}

	return isValidVHDFooter(footer[:]), nil
}

func isValidVHDFooter(footer []byte) bool {
	if len(footer) != vhdFooterSize {
		return false
	}

	if string(footer[0:8]) != vhdFooterCookie {
		return false
	}

	if binary.BigEndian.Uint32(footer[12:16]) != vhdFileFormatVersion {
		return false
	}

	if binary.BigEndian.Uint32(footer[60:64]) != vhdDiskTypeFixed {
		return false
	}

	wantChecksum := binary.BigEndian.Uint32(footer[64:68])
	buf := make([]byte, len(footer))
	copy(buf, footer)
	for i := 64; i < 68; i++ {
		buf[i] = 0
	}

	return vhdChecksum(buf) == wantChecksum
}

func vhdGeometry(totalSectors int64) (uint16, uint8, uint8) {
	candidateSectors := totalSectors
	if candidateSectors > vhdMaxGeometrySectors {
		candidateSectors = vhdMaxGeometrySectors
	}

	for {
		cylinders, heads, sectorsPerTrack := chsGeometry(candidateSectors)
		if int64(cylinders)*int64(heads)*int64(sectorsPerTrack) >= totalSectors {
			return cylinders, heads, sectorsPerTrack
		}

		candidateSectors++
	}
}

func chsGeometry(totalSectors int64) (uint16, uint8, uint8) {
	if totalSectors > vhdMaxGeometrySectors {
		totalSectors = vhdMaxGeometrySectors
	}

	var cylindersTimesHeads int64
	var heads uint8
	var sectorsPerTrack uint8

	if totalSectors >= 65535*16*63 {
		sectorsPerTrack = 255
		heads = 16
		cylindersTimesHeads = totalSectors / int64(sectorsPerTrack)
	} else {
		sectorsPerTrack = 17
		cylindersTimesHeads = totalSectors / int64(sectorsPerTrack)
		heads = uint8((cylindersTimesHeads + 1023) / 1024)

		if heads < 4 {
			heads = 4
		}

		if cylindersTimesHeads >= int64(heads)*1024 || heads > 16 {
			sectorsPerTrack = 31
			heads = 16
			cylindersTimesHeads = totalSectors / int64(sectorsPerTrack)
		}

		if cylindersTimesHeads >= int64(heads)*1024 {
			sectorsPerTrack = 63
			heads = 16
			cylindersTimesHeads = totalSectors / int64(sectorsPerTrack)
		}
	}

	cylinders := uint16(cylindersTimesHeads / int64(heads))
	return cylinders, heads, sectorsPerTrack
}

func vhdChecksum(footer []byte) uint32 {
	var sum uint32
	for _, b := range footer {
		sum += uint32(b)
	}

	return ^sum
}

func vhdTimestamp(now time.Time) uint32 {
	timestamp := now.Unix() - vhdTimestampBaseUnix
	if timestamp < 0 {
		return 0
	}

	return uint32(timestamp)
}
