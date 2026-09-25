package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"go.senan.xyz/taglib"
)

// Supported multi-format extensions
var supportedExtensions = map[string]bool{
	".mp3":  true,
	".flac": true,
	".aac":  true,
	".m4a":  true,
	".opus": true,
	".ogg":  true,
	".wav":  true,
}

func main() {
	// Defaults to current directory "." if -dir is omitted
	dirPtr := flag.String("dir", ".", "Directory containing music files")
	albumPtr := flag.String("album", "", "New album name")
	yearPtr := flag.String("year", "", "New release year (e.g., 2024)")
	flag.Parse()

	if *albumPtr == "" && *yearPtr == "" {
		log.Fatal("Error: You must provide at least -album or -year")
	}

	files, err := os.ReadDir(*dirPtr)
	if err != nil {
		log.Fatalf("Failed to read directory %q: %v", *dirPtr, err)
	}

	updated := 0

	for _, file := range files {
		ext := strings.ToLower(filepath.Ext(file.Name()))
		if file.IsDir() || !supportedExtensions[ext] {
			continue
		}

		filePath := filepath.Join(*dirPtr, file.Name())

		// Read existing tags into a map[string][]string
		tags, err := taglib.ReadTags(filePath)
		if err != nil {
			fmt.Printf("[SKIP] Could not read tags for %s: %v\n", file.Name(), err)
			continue
		}

		// Update target keys in tag map
		if *albumPtr != "" {
			tags[taglib.Album] = []string{*albumPtr}
		}
		if *yearPtr != "" {
			tags[taglib.Date] = []string{*yearPtr}
		}

		// Write modified tags back to the file
		// Passing 0 preserves non-conflicting existing tags
		if err := taglib.WriteTags(filePath, tags, 0); err != nil {
			fmt.Printf("[FAIL] Could not update %s: %v\n", file.Name(), err)
		} else {
			fmt.Printf("[OK] Updated %s (%s)\n", file.Name(), ext)
			updated++
		}
	}

	fmt.Printf("\nFinished. Updated %d file(s) in %q.\n", updated, *dirPtr)
}

