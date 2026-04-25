package main

import "testing"

func TestParseCommandLineMegabyteImage(t *testing.T) {
	command, err := parseCommandLine([]string{"disk.vhd", "64"})
	if err != nil {
		t.Fatalf("parseCommandLine returned error: %v", err)
	}

	if command.kind != commandKindImage {
		t.Fatalf("command kind = %d, want %d", command.kind, commandKindImage)
	}

	if command.filename != "disk.vhd" {
		t.Fatalf("filename = %q, want %q", command.filename, "disk.vhd")
	}

	if command.sizeMB != 64 {
		t.Fatalf("sizeMB = %d, want 64", command.sizeMB)
	}
}

func TestParseCommandLineFloppyPreset(t *testing.T) {
	command, err := parseCommandLine([]string{"floppy.img", "--floppy", "1440k"})
	if err != nil {
		t.Fatalf("parseCommandLine returned error: %v", err)
	}

	if command.kind != commandKindFloppy {
		t.Fatalf("command kind = %d, want %d", command.kind, commandKindFloppy)
	}

	if command.filename != "floppy.img" {
		t.Fatalf("filename = %q, want %q", command.filename, "floppy.img")
	}

	if command.floppyPreset != "1440k" {
		t.Fatalf("floppyPreset = %q, want %q", command.floppyPreset, "1440k")
	}
}

func TestParseCommandLineFloppyPresetEqualsForm(t *testing.T) {
	command, err := parseCommandLine([]string{"floppy.img", "--floppy=720K"})
	if err != nil {
		t.Fatalf("parseCommandLine returned error: %v", err)
	}

	if command.kind != commandKindFloppy {
		t.Fatalf("command kind = %d, want %d", command.kind, commandKindFloppy)
	}

	if command.floppyPreset != "720k" {
		t.Fatalf("floppyPreset = %q, want %q", command.floppyPreset, "720k")
	}
}

func TestParseCommandLineRejectsInvalidSize(t *testing.T) {
	if _, err := parseCommandLine([]string{"disk.img", "1440k"}); err == nil {
		t.Fatal("parseCommandLine returned nil error for invalid size")
	}
}

func TestParseCommandLineRejectsUnsupportedFloppyPreset(t *testing.T) {
	if _, err := parseCommandLine([]string{"floppy.img", "--floppy", "123k"}); err == nil {
		t.Fatal("parseCommandLine returned nil error for unsupported floppy preset")
	}
}

func TestParseCommandLineRejectsFloppyVHD(t *testing.T) {
	if _, err := parseCommandLine([]string{"floppy.vhd", "--floppy", "1440k"}); err == nil {
		t.Fatal("parseCommandLine returned nil error for floppy VHD")
	}
}

func TestParseCommandLineRejectsInvalidArgumentShape(t *testing.T) {
	if _, err := parseCommandLine([]string{"floppy.img", "--floppy"}); err == nil {
		t.Fatal("parseCommandLine returned nil error for missing floppy preset")
	}
}
