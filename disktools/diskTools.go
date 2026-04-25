package disktools

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
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
const fatReservedSectors12_16 = 4
const fatReservedSectors32 = 32
const fatCount = 2
const fatRootEntryCount = 512
const fatRootCluster = 2
const fat32FSInfoSector = 1
const fat32BackupBootSector = 6
const fatMediaFixedDisk = 0xf8
const fatExtBootSignature = 0x29
const fatTypeThreshold12 = 4085
const fatTypeThreshold16 = 65525
const fat32EndOfChain = 0x0fffffff
const fatBootOEMName = "MSDOS5.0"
const fatVolumeLabel = "NO NAME    "
const fatFSInfoLeadSignature = 0x41615252
const fatFSInfoStructSignature = 0x61417272
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

type fatFormat struct {
	fatBits           int
	totalSectors      uint32
	hiddenSectors     uint32
	volumeOffset      int64
	sectorsPerCluster uint8
	reservedSectors   uint16
	sectorsPerFAT     uint32
	rootEntryCount    uint16
	rootDirSectors    uint32
	firstDataSector   uint32
	clusterCount      uint32
	sectorsPerTrack   uint16
	headCount         uint16
	volumeID          uint32
}

// MakeVHD creates either a FAT-formatted superfloppy IMG or a fixed VHD.
func MakeVHD(filename string, size int) (err error) {
	imageType, err := imageTypeFromFilename(filename)
	if err != nil {
		return err
	}

	if err := CreateImage(filename, size); err != nil {
		return fmt.Errorf("CreateImage: %w", err)
	}
	defer cleanupPartialImageOnError(filename, &err)

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

func cleanupPartialImageOnError(filename string, err *error) {
	if *err == nil {
		return
	}

	if removeErr := os.Remove(filename); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		*err = fmt.Errorf("%w; additionally failed to remove partial image %q: %v", *err, filename, removeErr)
	}
}

// MakeFloppyImage creates a DOS floppy image for a standard floppy preset.
func MakeFloppyImage(filename string, preset string) error {
	return errors.New("floppy image creation is not implemented")
}

// CreateImage creates a blank image file at the requested size.
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

	wantSize := int64(size) * bytesPerMB
	file, err := os.OpenFile(filename, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open image file %q: %w", filename, err)
	}
	defer file.Close()

	var format fatFormat
	switch imageType {
	case imageExtIMG:
		if info.Size() != wantSize {
			return fmt.Errorf("image file %q has size %d bytes, want %d bytes", filename, info.Size(), wantSize)
		}

		format, err = makeFATFormat(info.Size(), fatBitsForSize(size), 0, 0)
		if err != nil {
			return err
		}
	case imageExtVHD:
		layout, err := layoutForVHD(size)
		if err != nil {
			return err
		}

		if info.Size() != layout.rawSize {
			return fmt.Errorf("image file %q has size %d bytes, want %d bytes", filename, info.Size(), layout.rawSize)
		}

		format, err = makeFATFormat(
			int64(layout.partitionSectors)*vhdSectorSize,
			layout.fatBits,
			layout.startLBA,
			int64(layout.startLBA)*vhdSectorSize,
		)
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported image type %q", imageType)
	}

	if err := writeFATVolume(file, format); err != nil {
		return fmt.Errorf("format image %q: %w", filename, err)
	}

	return nil
}

// makeFATFormat derives the FAT layout parameters for a volume.
func makeFATFormat(volumeSizeBytes int64, fatBits int, hiddenSectors uint32, volumeOffset int64) (fatFormat, error) {
	var format fatFormat

	if volumeSizeBytes <= 0 {
		return format, errors.New("volume size must be greater than 0")
	}

	if volumeSizeBytes%vhdSectorSize != 0 {
		return format, fmt.Errorf("volume size %d is not a multiple of %d bytes", volumeSizeBytes, vhdSectorSize)
	}

	totalSectors := uint32(volumeSizeBytes / vhdSectorSize)
	sectorsPerCluster := initialSectorsPerCluster(fatBits, totalSectors)
	sectorsPerTrack, headCount := defaultBPBGeometry(totalSectors, fatBits)

	for sectorsPerCluster <= 128 {
		reservedSectors := uint16(fatReservedSectors12_16)
		rootEntryCount := uint16(fatRootEntryCount)
		if fatBits == 32 {
			reservedSectors = fatReservedSectors32
			rootEntryCount = 0
		}

		rootDirSectors := (uint32(rootEntryCount)*32 + vhdSectorSize - 1) / vhdSectorSize
		sectorsPerFAT, clusterCount, firstDataSector, err := computeFATLayout(totalSectors, reservedSectors, rootDirSectors, sectorsPerCluster, fatBits)
		if err != nil {
			return format, err
		}

		if validClusterCountForFAT(clusterCount, fatBits) {
			format = fatFormat{
				fatBits:           fatBits,
				totalSectors:      totalSectors,
				hiddenSectors:     hiddenSectors,
				volumeOffset:      volumeOffset,
				sectorsPerCluster: sectorsPerCluster,
				reservedSectors:   reservedSectors,
				sectorsPerFAT:     sectorsPerFAT,
				rootEntryCount:    rootEntryCount,
				rootDirSectors:    rootDirSectors,
				firstDataSector:   firstDataSector,
				clusterCount:      clusterCount,
				sectorsPerTrack:   sectorsPerTrack,
				headCount:         headCount,
				volumeID:          0x1234abcd,
			}
			return format, nil
		}

		if fatBits == 32 {
			break
		}

		sectorsPerCluster *= 2
	}

	return format, fmt.Errorf("unable to derive valid FAT%d layout for %d sectors", fatBits, totalSectors)
}

// initialSectorsPerCluster picks a starting cluster size for the requested FAT type.
func initialSectorsPerCluster(fatBits int, totalSectors uint32) uint8 {
	sizeMB := int((uint64(totalSectors) * vhdSectorSize) / bytesPerMB)

	switch fatBits {
	case 12:
		if sizeMB <= 8 {
			return 4
		}
		return 8
	case 16:
		switch {
		case sizeMB <= 128:
			return 4
		case sizeMB <= 256:
			return 8
		default:
			return 16
		}
	default:
		return 8
	}
}

// defaultBPBGeometry returns conventional BPB geometry values for the volume.
func defaultBPBGeometry(totalSectors uint32, fatBits int) (uint16, uint16) {
	switch fatBits {
	case 12:
		return 32, 2
	case 16:
		if totalSectors < 65536 {
			return 32, 2
		}
		return 32, 8
	default:
		return 63, 32
	}
}

// computeFATLayout calculates the FAT size, cluster count, and first data sector.
func computeFATLayout(totalSectors uint32, reservedSectors uint16, rootDirSectors uint32, sectorsPerCluster uint8, fatBits int) (uint32, uint32, uint32, error) {
	sectorsPerFAT := uint32(1)
	reserved := uint32(reservedSectors)

	for i := 0; i < 16; i++ {
		overhead := reserved + fatCount*sectorsPerFAT + rootDirSectors
		if totalSectors <= overhead {
			return 0, 0, 0, fmt.Errorf("volume with %d sectors is too small for FAT%d", totalSectors, fatBits)
		}

		dataSectors := totalSectors - overhead
		clusterCount := dataSectors / uint32(sectorsPerCluster)
		if clusterCount == 0 {
			return 0, 0, 0, fmt.Errorf("volume with %d sectors leaves no data clusters for FAT%d", totalSectors, fatBits)
		}

		nextSectorsPerFAT := fatSectorsForEntries(clusterCount+2, fatBits)
		if nextSectorsPerFAT == sectorsPerFAT {
			return sectorsPerFAT, clusterCount, reserved + fatCount*sectorsPerFAT + rootDirSectors, nil
		}

		sectorsPerFAT = nextSectorsPerFAT
	}

	return 0, 0, 0, fmt.Errorf("FAT%d layout did not converge for %d sectors", fatBits, totalSectors)
}

// fatSectorsForEntries returns the FAT length needed for the given entry count.
func fatSectorsForEntries(entryCount uint32, fatBits int) uint32 {
	var fatBytes uint32

	switch fatBits {
	case 12:
		fatBytes = (entryCount*3 + 1) / 2
	case 16:
		fatBytes = entryCount * 2
	default:
		fatBytes = entryCount * 4
	}

	return (fatBytes + vhdSectorSize - 1) / vhdSectorSize
}

// validClusterCountForFAT reports whether the cluster count fits the FAT type.
func validClusterCountForFAT(clusterCount uint32, fatBits int) bool {
	switch fatBits {
	case 12:
		return clusterCount < fatTypeThreshold12
	case 16:
		return clusterCount >= fatTypeThreshold12 && clusterCount < fatTypeThreshold16
	default:
		return clusterCount >= fatTypeThreshold16
	}
}

// writeFATVolume writes the reserved area, FAT tables, and root region for a volume.
func writeFATVolume(file *os.File, format fatFormat) error {
	if err := writeReservedRegion(file, format); err != nil {
		return err
	}

	if err := writeFATTables(file, format); err != nil {
		return err
	}

	if err := writeRootRegion(file, format); err != nil {
		return err
	}

	return nil
}

// writeReservedRegion writes the boot and reserved sectors for the filesystem.
func writeReservedRegion(file *os.File, format fatFormat) error {
	buf := make([]byte, int(format.reservedSectors)*vhdSectorSize)

	if format.fatBits == 32 {
		boot := makeFAT32BootSector(format)
		copy(buf[:vhdSectorSize], boot[:])

		fsInfo := makeFAT32FSInfo(format)
		copy(buf[int(fat32FSInfoSector)*vhdSectorSize:], fsInfo[:])
		copy(buf[int(fat32BackupBootSector)*vhdSectorSize:], boot[:])
		copy(buf[int(fat32BackupBootSector+1)*vhdSectorSize:], fsInfo[:])
	} else {
		boot := makeFAT12Or16BootSector(format)
		copy(buf[:vhdSectorSize], boot[:])
	}

	if _, err := file.WriteAt(buf, format.volumeOffset); err != nil {
		return fmt.Errorf("write FAT reserved region: %w", err)
	}

	return nil
}

// makeFAT12Or16BootSector builds the boot sector for FAT12 and FAT16 volumes.
func makeFAT12Or16BootSector(format fatFormat) [vhdSectorSize]byte {
	var sector [vhdSectorSize]byte

	sector[0] = 0xeb
	sector[1] = 0x3c
	sector[2] = 0x90
	copy(sector[3:11], fatBootOEMName)
	binary.LittleEndian.PutUint16(sector[11:13], vhdSectorSize)
	sector[13] = format.sectorsPerCluster
	binary.LittleEndian.PutUint16(sector[14:16], format.reservedSectors)
	sector[16] = fatCount
	binary.LittleEndian.PutUint16(sector[17:19], format.rootEntryCount)
	putTotalSectorFields(sector[:], format.totalSectors)
	sector[21] = fatMediaFixedDisk
	binary.LittleEndian.PutUint16(sector[22:24], uint16(format.sectorsPerFAT))
	binary.LittleEndian.PutUint16(sector[24:26], format.sectorsPerTrack)
	binary.LittleEndian.PutUint16(sector[26:28], format.headCount)
	binary.LittleEndian.PutUint32(sector[28:32], format.hiddenSectors)
	sector[36] = 0x80
	sector[38] = fatExtBootSignature
	binary.LittleEndian.PutUint32(sector[39:43], format.volumeID)
	copy(sector[43:54], fatVolumeLabel)
	copy(sector[54:62], fmt.Sprintf("FAT%-5d", format.fatBits))
	sector[510] = 0x55
	sector[511] = 0xaa
	return sector
}

// makeFAT32BootSector builds the boot sector for a FAT32 volume.
func makeFAT32BootSector(format fatFormat) [vhdSectorSize]byte {
	var sector [vhdSectorSize]byte

	sector[0] = 0xeb
	sector[1] = 0x58
	sector[2] = 0x90
	copy(sector[3:11], fatBootOEMName)
	binary.LittleEndian.PutUint16(sector[11:13], vhdSectorSize)
	sector[13] = format.sectorsPerCluster
	binary.LittleEndian.PutUint16(sector[14:16], format.reservedSectors)
	sector[16] = fatCount
	sector[21] = fatMediaFixedDisk
	binary.LittleEndian.PutUint16(sector[24:26], format.sectorsPerTrack)
	binary.LittleEndian.PutUint16(sector[26:28], format.headCount)
	binary.LittleEndian.PutUint32(sector[28:32], format.hiddenSectors)
	binary.LittleEndian.PutUint32(sector[32:36], format.totalSectors)
	binary.LittleEndian.PutUint32(sector[36:40], format.sectorsPerFAT)
	binary.LittleEndian.PutUint32(sector[44:48], fatRootCluster)
	binary.LittleEndian.PutUint16(sector[48:50], fat32FSInfoSector)
	binary.LittleEndian.PutUint16(sector[50:52], fat32BackupBootSector)
	sector[64] = 0x80
	sector[66] = fatExtBootSignature
	binary.LittleEndian.PutUint32(sector[67:71], format.volumeID)
	copy(sector[71:82], fatVolumeLabel)
	copy(sector[82:90], "FAT32   ")
	sector[510] = 0x55
	sector[511] = 0xaa
	return sector
}

// makeFAT32FSInfo builds the FAT32 FSInfo sector.
func makeFAT32FSInfo(format fatFormat) [vhdSectorSize]byte {
	var sector [vhdSectorSize]byte

	binary.LittleEndian.PutUint32(sector[0:4], fatFSInfoLeadSignature)
	binary.LittleEndian.PutUint32(sector[484:488], fatFSInfoStructSignature)
	binary.LittleEndian.PutUint32(sector[488:492], format.clusterCount-1)
	binary.LittleEndian.PutUint32(sector[492:496], fatRootCluster)
	sector[510] = 0x55
	sector[511] = 0xaa
	return sector
}

// putTotalSectorFields fills the BPB total-sector fields for the volume size.
func putTotalSectorFields(bootSector []byte, totalSectors uint32) {
	if totalSectors < 65536 {
		binary.LittleEndian.PutUint16(bootSector[19:21], uint16(totalSectors))
		binary.LittleEndian.PutUint32(bootSector[32:36], 0)
		return
	}

	binary.LittleEndian.PutUint16(bootSector[19:21], 0)
	binary.LittleEndian.PutUint32(bootSector[32:36], totalSectors)
}

// writeFATTables writes both FAT copies for the volume.
func writeFATTables(file *os.File, format fatFormat) error {
	fatTable := make([]byte, int(format.sectorsPerFAT)*vhdSectorSize)
	switch format.fatBits {
	case 12:
		setFAT12Entry(fatTable, 0, 0x0ff0|uint16(fatMediaFixedDisk))
		setFAT12Entry(fatTable, 1, 0x0fff)
	case 16:
		binary.LittleEndian.PutUint16(fatTable[0:2], 0xff00|uint16(fatMediaFixedDisk))
		binary.LittleEndian.PutUint16(fatTable[2:4], 0xffff)
	default:
		binary.LittleEndian.PutUint32(fatTable[0:4], fat32EndOfChain&0xffffff00|uint32(fatMediaFixedDisk))
		binary.LittleEndian.PutUint32(fatTable[4:8], fat32EndOfChain)
		binary.LittleEndian.PutUint32(fatTable[8:12], fat32EndOfChain)
	}

	fatOffset := format.volumeOffset + int64(format.reservedSectors)*vhdSectorSize
	for copyIndex := 0; copyIndex < fatCount; copyIndex++ {
		offset := fatOffset + int64(copyIndex)*int64(len(fatTable))
		if _, err := file.WriteAt(fatTable, offset); err != nil {
			return fmt.Errorf("write FAT table %d: %w", copyIndex+1, err)
		}
	}

	return nil
}

// setFAT12Entry encodes a single 12-bit FAT entry into the table buffer.
func setFAT12Entry(fatTable []byte, cluster uint16, value uint16) {
	index := int(cluster) + int(cluster)/2
	if cluster%2 == 0 {
		fatTable[index] = byte(value & 0xff)
		fatTable[index+1] = (fatTable[index+1] & 0xf0) | byte((value>>8)&0x0f)
		return
	}

	fatTable[index] = (fatTable[index] & 0x0f) | byte((value<<4)&0xf0)
	fatTable[index+1] = byte(value >> 4)
}

// writeRootRegion clears the initial root directory area for an empty volume.
func writeRootRegion(file *os.File, format fatFormat) error {
	var offset int64
	var size int64

	if format.fatBits == 32 {
		offset = format.volumeOffset + int64(format.firstDataSector)*vhdSectorSize
		size = int64(format.sectorsPerCluster) * vhdSectorSize
	} else {
		rootDirStart := uint32(format.reservedSectors) + fatCount*format.sectorsPerFAT
		offset = format.volumeOffset + int64(rootDirStart)*vhdSectorSize
		size = int64(format.rootDirSectors) * vhdSectorSize
	}

	if size == 0 {
		return nil
	}

	if _, err := file.WriteAt(make([]byte, int(size)), offset); err != nil {
		return fmt.Errorf("write FAT root region: %w", err)
	}

	return nil
}

// ConvertToVHD appends a fixed VHD footer to a raw disk image.
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

// validateImageSpec enforces shared filename and size constraints.
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

// imageTypeFromFilename returns the supported image type for the filename.
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

// fatBitsForSize chooses FAT12, FAT16, or FAT32 from the image size.
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

// layoutForVHD calculates the disk and partition layout for a VHD image.
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

// partitionTypeForFAT returns the MBR partition type for the FAT variant.
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

// writeMBR writes a single-partition MBR for a VHD image.
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

// buildFixedVHDFooter builds a fixed-disk VHD footer for the raw image size.
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

// lbaToCHS converts an LBA address into packed CHS fields.
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

// hasValidVHDFooter reports whether the file already ends with a valid footer.
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

// isValidVHDFooter validates the fixed fields and checksum of a VHD footer.
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

// vhdGeometry chooses a CHS geometry that can represent the disk size.
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

// chsGeometry derives a legacy CHS geometry from a sector count.
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

// vhdChecksum computes the checksum used in a VHD footer.
func vhdChecksum(footer []byte) uint32 {
	var sum uint32
	for _, b := range footer {
		sum += uint32(b)
	}

	return ^sum
}

// vhdTimestamp converts a time into the VHD epoch.
func vhdTimestamp(now time.Time) uint32 {
	timestamp := now.Unix() - vhdTimestampBaseUnix
	if timestamp < 0 {
		return 0
	}

	return uint32(timestamp)
}
