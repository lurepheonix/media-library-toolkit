// Command meltk is the unified media toolkit binary.
package main

import (
	"fmt"
	"os"

	"meltk/internal/normalize"
	"meltk/internal/restore"
	"meltk/internal/setmeta"
)

func usage() {
	fmt.Fprintln(os.Stderr, "Usage: meltk <command> [flags] <path>...")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Commands:")
	fmt.Fprintln(os.Stderr, "  set-meta   Write ALBUM/ARTIST/YEAR tags (ex music-meta-fixer)")
	fmt.Fprintln(os.Stderr, "  normalize  Canonical rename from tags: dirs to \"YYYY - Album\", files to \"NN. Title\" (ex path-normalizer)")
	fmt.Fprintln(os.Stderr, "  restore    Restore \"_\" placeholders in names from tags (ex pathname-fixer)")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Run 'meltk <command> -h' for command help.")
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd, rest := os.Args[1], os.Args[2:]
	switch cmd {
	case "set-meta":
		setmeta.Run(rest)
	case "normalize":
		normalize.Run(rest)
	case "restore":
		restore.Run(rest)
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command %q\n", cmd)
		usage()
		os.Exit(2)
	}
}
