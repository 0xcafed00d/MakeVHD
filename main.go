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

var supportedFloppyPresets = map[string]struct{}{
	"160k":  {},
	"180k":  {},
	"320k":  {},
	"360k":  {},
	"720k":  {},
	"1200k": {},
	"1440k": {},
	"2880k": {},
}

type commandLine struct {
	kind         commandKind
	filename     string
	sizeMB       int
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
	switch {
	case len(args) == 2:
		if preset, ok := strings.CutPrefix(args[1], "--floppy="); ok {
			return parseFloppyCommand(args[0], preset)
		}

		size, err := strconv.Atoi(args[1])
		if err != nil {
			return commandLine{}, fmt.Errorf("invalid size %q: %w", args[1], err)
		}

		return commandLine{
			kind:     commandKindImage,
			filename: args[0],
			sizeMB:   size,
		}, nil
	case len(args) == 3 && args[1] == "--floppy":
		return parseFloppyCommand(args[0], args[2])
	default:
		return commandLine{}, fmt.Errorf("invalid arguments")
	}
}

func parseFloppyCommand(filename, preset string) (commandLine, error) {
	if !strings.EqualFold(filepath.Ext(filename), ".img") {
		return commandLine{}, fmt.Errorf("floppy images must use .img extension")
	}

	preset = strings.ToLower(preset)
	if _, ok := supportedFloppyPresets[preset]; !ok {
		return commandLine{}, fmt.Errorf("unsupported floppy preset %q; supported presets: %s", preset, floppyPresetUsage)
	}

	return commandLine{
		kind:         commandKindFloppy,
		filename:     filename,
		floppyPreset: preset,
	}, nil
}

func runCommand(command commandLine) error {
	switch command.kind {
	case commandKindImage:
		return disktools.MakeVHD(command.filename, command.sizeMB)
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
	return fmt.Sprintf("usage:\n  %s <filename(.img|.vhd)> <size (MB)>\n  %s <filename(.img)> --floppy <%s>", program, program, floppyPresetUsage)
}
