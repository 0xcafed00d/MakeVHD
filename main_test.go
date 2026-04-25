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

func TestParseCommandLineFloppyPresetAliases(t *testing.T) {
	tests := []struct {
		name string
		arg  string
		want string
	}{
		{name: "720 KB", arg: "720KB", want: "720k"},
		{name: "1.2 MB", arg: "1.2M", want: "1200k"},
		{name: "1.44 MB", arg: "1.44M", want: "1440k"},
		{name: "2.88 MB", arg: "2.88MB", want: "2880k"},
		{name: "3.5 DD", arg: "3.5dd", want: "720k"},
		{name: "3.5 HD", arg: "3.5hd", want: "1440k"},
		{name: "3.5 ED", arg: "3.5ed", want: "2880k"},
		{name: "5.25 DD", arg: "5.25dd", want: "360k"},
		{name: "5.25 HD", arg: "5.25hd", want: "1200k"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			command, err := parseCommandLine([]string{"floppy.img", "--floppy", tt.arg})
			if err != nil {
				t.Fatalf("parseCommandLine returned error: %v", err)
			}

			if command.floppyPreset != tt.want {
				t.Fatalf("floppyPreset = %q, want %q", command.floppyPreset, tt.want)
			}
		})
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
