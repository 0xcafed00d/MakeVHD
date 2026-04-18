package main

import (
	"fmt"
	"os"
	"strconv"

	"makevhd/disktools"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintf(os.Stderr, "usage: %s <filename(.img|.vhd)> <size (MB)>\n", os.Args[0])
		os.Exit(1)
	}

	filename := os.Args[1]
	size, err := strconv.Atoi(os.Args[2])
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid size %q: %v\n", os.Args[2], err)
		os.Exit(1)
	}

	if err := disktools.MakeVHD(filename, size); err != nil {
		fmt.Fprintf(os.Stderr, "MakeVHD failed: %v\n", err)
		os.Exit(1)
	}
}
