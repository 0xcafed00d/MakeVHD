package disktools

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	imageExtDSK = ".dsk"
	imageExtHFS = ".hfs"

	hfsPlusMinSizeMB                   = 1
	hfsPlusBlockSize                   = 4096
	hfsPlusVolumeHeaderOffset          = 1024
	hfsPlusAlternateVolumeHeaderOffset = 1024
	hfsPlusVolumeHeaderSize            = 512
	hfsPlusSignature                   = 0x482b // "H+"
	hfsPlusVersion                     = 4
	hfsPlusLastMountedVersion          = "mkvh"
	hfsPlusUnmountedMask               = 1 << 8
	hfsPlusMacEpochOffset              = 2082844800
	hfsPlusDefaultVolumeName           = "Untitled"

	hfsPlusRootParentID         = 1
	hfsPlusRootFolderID         = 2
	hfsPlusFirstUserCatalogID   = 16
	hfsPlusTextEncodingMacRoman = 0

	hfsPlusForkDataSize = 80

	hfsPlusBTreeNodeDescriptorSize = 14
	hfsPlusBTreeHeaderRecordSize   = 106
	hfsPlusBTreeUserDataSize       = 128
	hfsPlusBTreeHeaderRecordOffset = hfsPlusBTreeNodeDescriptorSize
	hfsPlusBTreeUserDataOffset     = hfsPlusBTreeHeaderRecordOffset + hfsPlusBTreeHeaderRecordSize
	hfsPlusBTreeMapRecordOffset    = hfsPlusBTreeUserDataOffset + hfsPlusBTreeUserDataSize

	hfsPlusBTreeHeaderNodeKind = 0x01
	hfsPlusBTreeLeafNodeKind   = 0xff
	hfsPlusBTreeType           = 0
	hfsPlusBTreeBigKeysMask    = 0x00000002
	hfsPlusBTreeVariableKeys   = 0x00000004

	hfsPlusCatalogNodeSize       = hfsPlusBlockSize
	hfsPlusCatalogMaxKeyLength   = 516
	hfsPlusExtentsMaxKeyLength   = 10
	hfsPlusFolderRecord          = 0x0001
	hfsPlusFolderThreadRecord    = 0x0003
	hfsPlusCatalogFolderDataSize = 88

	hfsPlusVolumeHeaderAllocationForkOffset = 112
	hfsPlusVolumeHeaderExtentsForkOffset    = hfsPlusVolumeHeaderAllocationForkOffset + hfsPlusForkDataSize
	hfsPlusVolumeHeaderCatalogForkOffset    = hfsPlusVolumeHeaderExtentsForkOffset + hfsPlusForkDataSize
	hfsPlusVolumeHeaderAttributesForkOffset = hfsPlusVolumeHeaderCatalogForkOffset + hfsPlusForkDataSize
	hfsPlusVolumeHeaderStartupForkOffset    = hfsPlusVolumeHeaderAttributesForkOffset + hfsPlusForkDataSize
)

type hfsPlusLayout struct {
	rawSize int64

	totalBlocks uint32
	freeBlocks  uint32

	allocationFileStart       uint32
	allocationFileBlocks      uint32
	allocationFileLogicalSize uint64

	extentsFileStart  uint32
	extentsFileBlocks uint32

	catalogFileStart  uint32
	catalogFileBlocks uint32

	nextAllocation uint32
	volumeName     string
	now            uint32
}

// MakeMacImage creates a raw Mac OS Extended (HFS+) disk image.
func MakeMacImage(filename string, size int) (err error) {
	if err := validateMacImageSpec(filename, size); err != nil {
		return err
	}

	layout, err := makeHFSPlusLayout(filename, size)
	if err != nil {
		return err
	}

	file, err := os.OpenFile(filename, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("create Mac image file %q: %w", filename, err)
	}
	defer cleanupPartialImageOnError(filename, &err)
	defer func() {
		if closeErr := file.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("close Mac image file %q: %w", filename, closeErr)
		}
	}()

	if err := file.Truncate(layout.rawSize); err != nil {
		return fmt.Errorf("set Mac image size to %d MB: %w", size, err)
	}

	if err := writeHFSPlusVolume(file, layout); err != nil {
		return fmt.Errorf("format Mac image %q: %w", filename, err)
	}

	return nil
}

func validateMacImageSpec(filename string, size int) error {
	if _, err := macImageTypeFromFilename(filename); err != nil {
		return err
	}

	if size < hfsPlusMinSizeMB {
		return fmt.Errorf("size for Mac images must be at least %d MB, got %d", hfsPlusMinSizeMB, size)
	}

	if size > maxImageSizeMB {
		return fmt.Errorf("size must not exceed %d MB, got %d", maxImageSizeMB, size)
	}

	return nil
}

func macImageTypeFromFilename(filename string) (string, error) {
	if filename == "" {
		return "", errors.New("filename must not be empty")
	}

	switch strings.ToLower(filepath.Ext(filename)) {
	case imageExtIMG:
		return imageExtIMG, nil
	case imageExtDSK:
		return imageExtDSK, nil
	case imageExtHFS:
		return imageExtHFS, nil
	default:
		return "", fmt.Errorf("Mac images must use %q, %q, or %q extension", imageExtIMG, imageExtDSK, imageExtHFS)
	}
}

func makeHFSPlusLayout(filename string, size int) (hfsPlusLayout, error) {
	rawSize := int64(size) * bytesPerMB
	if rawSize%hfsPlusBlockSize != 0 {
		return hfsPlusLayout{}, fmt.Errorf("Mac image size %d bytes is not a multiple of %d", rawSize, hfsPlusBlockSize)
	}

	totalBlocks := uint32(rawSize / hfsPlusBlockSize)
	if totalBlocks < 8 {
		return hfsPlusLayout{}, fmt.Errorf("Mac image has only %d allocation blocks; need at least 8", totalBlocks)
	}

	layout := hfsPlusLayout{
		rawSize:                   rawSize,
		totalBlocks:               totalBlocks,
		allocationFileLogicalSize: uint64((totalBlocks + 7) / 8),
		volumeName:                hfsPlusVolumeNameFromFilename(filename),
		now:                       hfsPlusTimestamp(time.Now().UTC()),
	}

	nextBlock := uint32(1)
	layout.allocationFileStart = nextBlock
	layout.allocationFileBlocks = hfsPlusBlocksForBytes(layout.allocationFileLogicalSize)
	nextBlock += layout.allocationFileBlocks

	layout.extentsFileStart = nextBlock
	layout.extentsFileBlocks = 1
	nextBlock += layout.extentsFileBlocks

	layout.catalogFileStart = nextBlock
	layout.catalogFileBlocks = 2
	nextBlock += layout.catalogFileBlocks

	if nextBlock >= totalBlocks-1 {
		return hfsPlusLayout{}, fmt.Errorf("Mac image is too small for HFS+ metadata")
	}

	layout.nextAllocation = nextBlock
	usedBlocks := nextBlock + 1 // front reserved/metadata blocks plus the final alternate-header block.
	layout.freeBlocks = totalBlocks - usedBlocks
	return layout, nil
}

func hfsPlusBlocksForBytes(size uint64) uint32 {
	return uint32((size + hfsPlusBlockSize - 1) / hfsPlusBlockSize)
}

func hfsPlusVolumeNameFromFilename(filename string) string {
	base := filepath.Base(filename)
	ext := filepath.Ext(base)
	if ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	base = strings.TrimSpace(base)
	if base == "" {
		return hfsPlusDefaultVolumeName
	}

	var builder strings.Builder
	for _, r := range base {
		if builder.Len() >= 255 {
			break
		}

		switch {
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r)
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == ' ' || r == '-' || r == '_' || r == '.':
			builder.WriteRune(r)
		default:
			builder.WriteByte('_')
		}
	}

	name := strings.TrimSpace(builder.String())
	if name == "" {
		return hfsPlusDefaultVolumeName
	}
	return name
}

func writeHFSPlusVolume(file *os.File, layout hfsPlusLayout) error {
	if err := writeHFSPlusAllocationFile(file, layout); err != nil {
		return err
	}

	if err := writeHFSPlusBTreeHeaderFile(file, layout.extentsFileStart, hfsPlusBTreeHeader{
		nodeSize:     hfsPlusCatalogNodeSize,
		maxKeyLength: hfsPlusExtentsMaxKeyLength,
		totalNodes:   1,
		freeNodes:    0,
		clumpSize:    hfsPlusBlockSize,
		attributes:   hfsPlusBTreeBigKeysMask,
	}); err != nil {
		return fmt.Errorf("write HFS+ extents B-tree: %w", err)
	}

	if err := writeHFSPlusCatalogFile(file, layout); err != nil {
		return err
	}

	header := makeHFSPlusVolumeHeader(layout)
	if _, err := file.WriteAt(header, hfsPlusVolumeHeaderOffset); err != nil {
		return fmt.Errorf("write HFS+ volume header: %w", err)
	}

	alternateOffset := layout.rawSize - hfsPlusAlternateVolumeHeaderOffset
	if _, err := file.WriteAt(header, alternateOffset); err != nil {
		return fmt.Errorf("write HFS+ alternate volume header: %w", err)
	}

	return nil
}

func writeHFSPlusAllocationFile(file *os.File, layout hfsPlusLayout) error {
	bitmap := make([]byte, layout.allocationFileLogicalSize)
	for block := uint32(0); block < layout.nextAllocation; block++ {
		setHFSPlusAllocationBit(bitmap, block)
	}
	setHFSPlusAllocationBit(bitmap, layout.totalBlocks-1)

	offset := int64(layout.allocationFileStart) * hfsPlusBlockSize
	if _, err := file.WriteAt(bitmap, offset); err != nil {
		return fmt.Errorf("write HFS+ allocation bitmap: %w", err)
	}

	return nil
}

func setHFSPlusAllocationBit(bitmap []byte, block uint32) {
	byteIndex := block / 8
	bitIndex := 7 - (block % 8)
	bitmap[byteIndex] |= 1 << bitIndex
}

type hfsPlusBTreeHeader struct {
	treeDepth     uint16
	rootNode      uint32
	leafRecords   uint32
	firstLeafNode uint32
	lastLeafNode  uint32
	nodeSize      uint16
	maxKeyLength  uint16
	totalNodes    uint32
	freeNodes     uint32
	clumpSize     uint32
	attributes    uint32
}

func writeHFSPlusBTreeHeaderFile(file *os.File, startBlock uint32, header hfsPlusBTreeHeader) error {
	node := makeHFSPlusBTreeHeaderNode(header)
	offset := int64(startBlock) * hfsPlusBlockSize
	if _, err := file.WriteAt(node, offset); err != nil {
		return err
	}

	return nil
}

func makeHFSPlusBTreeHeaderNode(header hfsPlusBTreeHeader) []byte {
	nodeSize := int(header.nodeSize)
	node := make([]byte, nodeSize)

	putHFSPlusNodeDescriptor(node, 0, 0, hfsPlusBTreeHeaderNodeKind, 0, 3)

	record := node[hfsPlusBTreeHeaderRecordOffset:]
	binary.BigEndian.PutUint16(record[0:2], header.treeDepth)
	binary.BigEndian.PutUint32(record[2:6], header.rootNode)
	binary.BigEndian.PutUint32(record[6:10], header.leafRecords)
	binary.BigEndian.PutUint32(record[10:14], header.firstLeafNode)
	binary.BigEndian.PutUint32(record[14:18], header.lastLeafNode)
	binary.BigEndian.PutUint16(record[18:20], header.nodeSize)
	binary.BigEndian.PutUint16(record[20:22], header.maxKeyLength)
	binary.BigEndian.PutUint32(record[22:26], header.totalNodes)
	binary.BigEndian.PutUint32(record[26:30], header.freeNodes)
	binary.BigEndian.PutUint32(record[32:36], header.clumpSize)
	record[36] = hfsPlusBTreeType
	binary.BigEndian.PutUint32(record[38:42], header.attributes)

	mapRecord := node[hfsPlusBTreeMapRecordOffset : nodeSize-8]
	markHFSPlusBTreeNodesUsed(mapRecord, header.totalNodes-header.freeNodes)

	putHFSPlusNodeOffsets(node, []uint16{
		hfsPlusBTreeHeaderRecordOffset,
		hfsPlusBTreeUserDataOffset,
		hfsPlusBTreeMapRecordOffset,
		uint16(nodeSize - 8),
	})
	return node
}

func putHFSPlusNodeDescriptor(node []byte, fLink, bLink uint32, kind byte, height byte, records uint16) {
	binary.BigEndian.PutUint32(node[0:4], fLink)
	binary.BigEndian.PutUint32(node[4:8], bLink)
	node[8] = kind
	node[9] = height
	binary.BigEndian.PutUint16(node[10:12], records)
}

func markHFSPlusBTreeNodesUsed(bitmap []byte, usedNodes uint32) {
	for node := uint32(0); node < usedNodes; node++ {
		byteIndex := node / 8
		bitIndex := 7 - (node % 8)
		bitmap[byteIndex] |= 1 << bitIndex
	}
}

func putHFSPlusNodeOffsets(node []byte, offsets []uint16) {
	for i, offset := range offsets {
		binary.BigEndian.PutUint16(node[len(node)-2*(i+1):], offset)
	}
}

func writeHFSPlusCatalogFile(file *os.File, layout hfsPlusLayout) error {
	if err := writeHFSPlusBTreeHeaderFile(file, layout.catalogFileStart, hfsPlusBTreeHeader{
		treeDepth:     1,
		rootNode:      1,
		leafRecords:   2,
		firstLeafNode: 1,
		lastLeafNode:  1,
		nodeSize:      hfsPlusCatalogNodeSize,
		maxKeyLength:  hfsPlusCatalogMaxKeyLength,
		totalNodes:    2,
		freeNodes:     0,
		clumpSize:     hfsPlusBlockSize,
		attributes:    hfsPlusBTreeBigKeysMask | hfsPlusBTreeVariableKeys,
	}); err != nil {
		return fmt.Errorf("write HFS+ catalog B-tree header: %w", err)
	}

	leaf := make([]byte, hfsPlusCatalogNodeSize)
	putHFSPlusNodeDescriptor(leaf, 0, 0, hfsPlusBTreeLeafNodeKind, 1, 2)

	records := [][]byte{
		makeHFSPlusCatalogFolderRecord(layout.volumeName, layout.now),
		makeHFSPlusCatalogThreadRecord(layout.volumeName),
	}

	offsets := make([]uint16, 0, len(records)+1)
	offset := uint16(hfsPlusBTreeNodeDescriptorSize)
	for _, record := range records {
		offsets = append(offsets, offset)
		copy(leaf[offset:], record)
		offset += uint16(len(record))
	}
	offsets = append(offsets, offset)
	putHFSPlusNodeOffsets(leaf, offsets)

	leafOffset := int64(layout.catalogFileStart)*hfsPlusBlockSize + hfsPlusCatalogNodeSize
	if _, err := file.WriteAt(leaf, leafOffset); err != nil {
		return fmt.Errorf("write HFS+ catalog leaf: %w", err)
	}

	return nil
}

func makeHFSPlusCatalogFolderRecord(volumeName string, now uint32) []byte {
	key := makeHFSPlusCatalogKey(hfsPlusRootParentID, volumeName)
	data := make([]byte, hfsPlusCatalogFolderDataSize)
	binary.BigEndian.PutUint16(data[0:2], hfsPlusFolderRecord)
	binary.BigEndian.PutUint32(data[8:12], hfsPlusRootFolderID)
	for _, offset := range []int{12, 16, 20, 24, 28} {
		binary.BigEndian.PutUint32(data[offset:offset+4], now)
	}
	binary.BigEndian.PutUint32(data[80:84], hfsPlusTextEncodingMacRoman)

	record := make([]byte, len(key)+len(data))
	copy(record, key)
	copy(record[len(key):], data)
	return record
}

func makeHFSPlusCatalogThreadRecord(volumeName string) []byte {
	key := makeHFSPlusCatalogKey(hfsPlusRootFolderID, "")
	name := hfsPlusEncodeName(volumeName)
	data := make([]byte, 10+len(name)*2)
	binary.BigEndian.PutUint16(data[0:2], hfsPlusFolderThreadRecord)
	binary.BigEndian.PutUint32(data[4:8], hfsPlusRootParentID)
	putHFSPlusUnicodeName(data[8:], name)

	record := make([]byte, len(key)+len(data))
	copy(record, key)
	copy(record[len(key):], data)
	return record
}

func makeHFSPlusCatalogKey(parentID uint32, name string) []byte {
	encodedName := hfsPlusEncodeName(name)
	keyLength := uint16(6 + len(encodedName)*2)
	key := make([]byte, 2+int(keyLength))
	binary.BigEndian.PutUint16(key[0:2], keyLength)
	binary.BigEndian.PutUint32(key[2:6], parentID)
	putHFSPlusUnicodeName(key[6:], encodedName)
	return key
}

func hfsPlusEncodeName(name string) []uint16 {
	encoded := make([]uint16, 0, len(name))
	for _, r := range name {
		if len(encoded) >= 255 {
			break
		}
		encoded = append(encoded, uint16(r))
	}
	return encoded
}

func putHFSPlusUnicodeName(buf []byte, encodedName []uint16) {
	binary.BigEndian.PutUint16(buf[0:2], uint16(len(encodedName)))
	offset := 2
	for _, r := range encodedName {
		binary.BigEndian.PutUint16(buf[offset:offset+2], r)
		offset += 2
	}
}

func makeHFSPlusVolumeHeader(layout hfsPlusLayout) []byte {
	header := make([]byte, hfsPlusVolumeHeaderSize)
	binary.BigEndian.PutUint16(header[0:2], hfsPlusSignature)
	binary.BigEndian.PutUint16(header[2:4], hfsPlusVersion)
	binary.BigEndian.PutUint32(header[4:8], hfsPlusUnmountedMask)
	copy(header[8:12], hfsPlusLastMountedVersion)
	binary.BigEndian.PutUint32(header[16:20], layout.now)
	binary.BigEndian.PutUint32(header[20:24], layout.now)
	binary.BigEndian.PutUint32(header[28:32], layout.now)
	binary.BigEndian.PutUint32(header[40:44], hfsPlusBlockSize)
	binary.BigEndian.PutUint32(header[44:48], layout.totalBlocks)
	binary.BigEndian.PutUint32(header[48:52], layout.freeBlocks)
	binary.BigEndian.PutUint32(header[52:56], layout.nextAllocation)
	binary.BigEndian.PutUint32(header[56:60], hfsPlusBlockSize)
	binary.BigEndian.PutUint32(header[60:64], hfsPlusBlockSize)
	binary.BigEndian.PutUint32(header[64:68], hfsPlusFirstUserCatalogID)
	binary.BigEndian.PutUint32(header[68:72], 1)
	binary.BigEndian.PutUint64(header[72:80], 1<<hfsPlusTextEncodingMacRoman)

	putHFSPlusForkData(
		header[hfsPlusVolumeHeaderAllocationForkOffset:],
		layout.allocationFileLogicalSize,
		layout.allocationFileBlocks,
		layout.allocationFileStart,
		layout.allocationFileBlocks,
	)
	putHFSPlusForkData(
		header[hfsPlusVolumeHeaderExtentsForkOffset:],
		uint64(layout.extentsFileBlocks)*hfsPlusBlockSize,
		layout.extentsFileBlocks,
		layout.extentsFileStart,
		layout.extentsFileBlocks,
	)
	putHFSPlusForkData(
		header[hfsPlusVolumeHeaderCatalogForkOffset:],
		uint64(layout.catalogFileBlocks)*hfsPlusBlockSize,
		layout.catalogFileBlocks,
		layout.catalogFileStart,
		layout.catalogFileBlocks,
	)

	return header
}

func putHFSPlusForkData(buf []byte, logicalSize uint64, totalBlocks uint32, startBlock uint32, blockCount uint32) {
	binary.BigEndian.PutUint64(buf[0:8], logicalSize)
	binary.BigEndian.PutUint32(buf[8:12], hfsPlusBlockSize)
	binary.BigEndian.PutUint32(buf[12:16], totalBlocks)
	binary.BigEndian.PutUint32(buf[16:20], startBlock)
	binary.BigEndian.PutUint32(buf[20:24], blockCount)
}

func hfsPlusTimestamp(now time.Time) uint32 {
	timestamp := now.Unix() + hfsPlusMacEpochOffset
	if timestamp < 0 {
		return 0
	}
	if timestamp > int64(^uint32(0)) {
		return ^uint32(0)
	}
	return uint32(timestamp)
}
