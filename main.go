package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"makevhd/disktools"
)

type commandKind int

const (
	commandKindImage commandKind = iota
	commandKindFloppy
)

const floppyPresetUsage = "160k|180k|320k|360k|720k|1200k|1440k|2880k"
const floppyAliasUsage = "1.2m|1.44m|2.88m|3.5dd|3.5hd|3.5ed|5.25dd|5.25hd"

var floppyPresetAliases = map[string]string{
	"160k":    "160k",
	"160kb":   "160k",
	"180k":    "180k",
	"180kb":   "180k",
	"320k":    "320k",
	"320kb":   "320k",
	"360k":    "360k",
	"360kb":   "360k",
	"720k":    "720k",
	"720kb":   "720k",
	"1200k":   "1200k",
	"1200kb":  "1200k",
	"1440k":   "1440k",
	"1440kb":  "1440k",
	"2880k":   "2880k",
	"2880kb":  "2880k",
	"1.2m":    "1200k",
	"1.2mb":   "1200k",
	"1.44m":   "1440k",
	"1.44mb":  "1440k",
	"2.88m":   "2880k",
	"2.88mb":  "2880k",
	"3.5dd":   "720k",
	"3.5-dd":  "720k",
	"3.5hd":   "1440k",
	"3.5-hd":  "1440k",
	"3.5ed":   "2880k",
	"3.5-ed":  "2880k",
	"5.25dd":  "360k",
	"5.25-dd": "360k",
	"5.25hd":  "1200k",
	"5.25-hd": "1200k",
}

type commandLine struct {
	kind         commandKind
	filename     string
	sizeMB       int
	fatBits      int
	floppyPreset string
}

func main() {
	command, err := parseCommandLine(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n\n%s\n", err, usage(os.Args[0]))
		os.Exit(1)
	}

	if err := runCommand(command); err != nil {
		fmt.Fprintf(os.Stderr, "%s failed: %v\n", command.actionName(), err)
		os.Exit(1)
	}
}

func parseCommandLine(args []string) (commandLine, error) {
	if len(args) < 2 {
		return commandLine{}, fmt.Errorf("invalid arguments")
	}

	if preset, ok := strings.CutPrefix(args[1], "--floppy="); ok {
		if len(args) != 2 {
			return commandLine{}, fmt.Errorf("--floppy cannot be combined with other options")
		}
		return parseFloppyCommand(args[0], preset)
	}

	if args[1] == "--floppy" {
		if len(args) != 3 {
			return commandLine{}, fmt.Errorf("invalid arguments")
		}
		return parseFloppyCommand(args[0], args[2])
	}

	return parseImageCommand(args[0], args[1:])
}

func parseImageCommand(filename string, args []string) (commandLine, error) {
	var sizeMB int
	var hasSize bool
	var fatBits int

	for index := 0; index < len(args); index++ {
		arg := args[index]

		if value, ok := strings.CutPrefix(arg, "--fat="); ok {
			parsedFATBits, err := parseFATBits(value)
			if err != nil {
				return commandLine{}, err
			}
			fatBits = parsedFATBits
			continue
		}

		if arg == "--fat" {
			index++
			if index >= len(args) {
				return commandLine{}, fmt.Errorf("--fat requires 12, 16, or 32")
			}

			parsedFATBits, err := parseFATBits(args[index])
			if err != nil {
				return commandLine{}, err
			}
			fatBits = parsedFATBits
			continue
		}

		if strings.HasPrefix(arg, "--") {
			return commandLine{}, fmt.Errorf("unknown option %q", arg)
		}

		if hasSize {
			return commandLine{}, fmt.Errorf("invalid arguments")
		}

		size, err := strconv.Atoi(arg)
		if err != nil {
			return commandLine{}, fmt.Errorf("invalid size %q: %w", arg, err)
		}

		sizeMB = size
		hasSize = true
	}

	if !hasSize {
		return commandLine{}, fmt.Errorf("invalid arguments")
	}

	return commandLine{
		kind:     commandKindImage,
		filename: filename,
		sizeMB:   sizeMB,
		fatBits:  fatBits,
	}, nil
}

func parseFloppyCommand(filename, preset string) (commandLine, error) {
	if !strings.EqualFold(filepath.Ext(filename), ".img") {
		return commandLine{}, fmt.Errorf("floppy images must use .img extension")
	}

	canonicalPreset, ok := normalizeFloppyPreset(preset)
	if !ok {
		return commandLine{}, fmt.Errorf("unsupported floppy preset %q; supported presets: %s; aliases: %s", preset, floppyPresetUsage, floppyAliasUsage)
	}

	return commandLine{
		kind:         commandKindFloppy,
		filename:     filename,
		floppyPreset: canonicalPreset,
	}, nil
}

func normalizeFloppyPreset(preset string) (string, bool) {
	canonicalPreset, ok := floppyPresetAliases[strings.ToLower(preset)]
	return canonicalPreset, ok
}

func parseFATBits(value string) (int, error) {
	fatBits, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("invalid FAT type %q: %w", value, err)
	}

	switch fatBits {
	case 12, 16, 32:
		return fatBits, nil
	default:
		return 0, fmt.Errorf("invalid FAT type %d; supported FAT types: 12, 16, 32", fatBits)
	}
}

func runCommand(command commandLine) error {
	switch command.kind {
	case commandKindImage:
		return disktools.MakeVHDWithFAT(command.filename, command.sizeMB, command.fatBits)
	case commandKindFloppy:
		return disktools.MakeFloppyImage(command.filename, command.floppyPreset)
	default:
		return fmt.Errorf("unknown command kind %d", command.kind)
	}
}

func (command commandLine) actionName() string {
	if command.kind == commandKindFloppy {
		return "MakeFloppyImage"
	}

	return "MakeVHD"
}

func usage(program string) string {
	return fmt.Sprintf("usage:\n  %s <filename(.img|.vhd)> <size (MB)> [--fat <12|16|32>]\n  %s <filename(.img|.vhd)> <size (MB)> [--fat=<12|16|32>]\n  %s <filename(.img)> --floppy <%s>\n\nfloppy aliases: %s", program, program, program, floppyPresetUsage, floppyAliasUsage)
}
