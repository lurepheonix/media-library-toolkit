// Package setmeta implements `meltk set-meta`: batch-write ALBUM/ARTIST/YEAR tags.
package setmeta

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.senan.xyz/taglib"

	"meltk/internal/shared"
)

// Run executes the set-meta subcommand. args are the subcommand args
// (without the "set-meta" prefix).
func Run(args []string) {
	fs := flag.NewFlagSet("set-meta", flag.ContinueOnError)
	albumPtr := fs.String("album", "", "New album name")
	artistPtr := fs.String("artist", "", "New artist name (writes ARTIST and ALBUMARTIST)")
	yearPtr := fs.String("year", "", "New release year (YYYY; full dates like 2009-05-01 accepted, year is stored)")
	dryPtr := fs.Bool("dry-run", false, "Print what would be updated without writing tags")
	verbosePtr := fs.Bool("verbose", false, "Print skip reasons (off by default)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: meltk set-meta [-album NAME] [-artist NAME] [-year YYYY] [-dry-run] [-verbose] <dir>...")
		fmt.Fprintln(os.Stderr, "  Writes ALBUM/ARTIST/YEAR tags to audio files directly inside each <dir>.")
		fmt.Fprintln(os.Stderr, "  At least one of -album, -artist, -year is required.")
		fmt.Fprintln(os.Stderr, "  <dir> is obligatory; use \".\" for the current directory.")
		fmt.Fprintln(os.Stderr, "  Year is normalized to YYYY (1000-2999); stored in DATE (plus YEAR alias).")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			os.Exit(0)
		}
		os.Exit(2)
	}

	album := strings.TrimSpace(*albumPtr)
	artist := strings.TrimSpace(*artistPtr)
	yearRaw := strings.TrimSpace(*yearPtr)
	dryRun, verbose := *dryPtr, *verbosePtr

	if album == "" && artist == "" && yearRaw == "" {
		fmt.Fprintln(os.Stderr, "Error: you must provide at least one of -album, -artist or -year")
		fs.Usage()
		os.Exit(2)
	}

	year := ""
	if yearRaw != "" {
		year = shared.ParseYear(yearRaw)
		if year == "" {
			fmt.Fprintf(os.Stderr, "Error: -year %q contains no valid YYYY (1000-2999)\n", yearRaw)
			os.Exit(2)
		}
	}

	dirs := fs.Args()
	if len(dirs) == 0 {
		fmt.Fprintln(os.Stderr, "Error: <dir> is obligatory; pass at least one directory (use \".\" for current dir)")
		fs.Usage()
		os.Exit(2)
	}

	updated, skipped, failed, wouldUpdate := 0, 0, 0, 0
	skipf := func(path, reason string) {
		skipped++
		if verbose {
			fmt.Printf("[SKIP] %q: %s\n", path, reason)
		}
	}
	failf := func(path, reason string) {
		failed++
		fmt.Printf("[FAIL] %q: %s\n", path, reason)
	}

	for _, dir := range dirs {
		info, err := os.Stat(dir)
		if err != nil {
			failf(dir, fmt.Sprintf("cannot stat: %v", err))
			continue
		}
		if !info.IsDir() {
			skipf(dir, "not a directory")
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			failf(dir, fmt.Sprintf("cannot list directory: %v", err))
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if !shared.IsMusicFile(e.Name()) {
				skipf(filepath.Join(dir, e.Name()), shared.FileSkipReason(e.Name()))
				continue
			}
			filePath := filepath.Join(dir, e.Name())
			tags, err := taglib.ReadTags(filePath)
			if err != nil {
				failf(filePath, fmt.Sprintf("could not read tags: %v", err))
				continue
			}
			if album != "" {
				tags[taglib.Album] = []string{album}
			}
			if artist != "" {
				tags[taglib.Artist] = []string{artist}
				tags[taglib.AlbumArtist] = []string{artist}
			}
			if year != "" {
				tags[taglib.Date] = []string{year}
				tags["YEAR"] = []string{year}
			}
			if dryRun {
				fmt.Printf("[DRY] %q would update (album=%q artist=%q year=%q)\n", filePath, album, artist, year)
				wouldUpdate++
				continue
			}
			// Passing 0 preserves non-conflicting existing tags.
			if err := taglib.WriteTags(filePath, tags, 0); err != nil {
				failf(filePath, fmt.Sprintf("could not update: %v", err))
			} else {
				fmt.Printf("[OK] Updated %q\n", filePath)
				updated++
			}
		}
	}

	if dryRun {
		fmt.Printf("\nFinished. Would update %d file(s) (%d skipped, %d failed).\n", wouldUpdate, skipped, failed)
	} else {
		fmt.Printf("\nFinished. Updated %d file(s) (%d skipped, %d failed).\n", updated, skipped, failed)
	}
	if failed > 0 {
		os.Exit(1)
	}
}
