// Package normalize implements `meltk normalize`: canonical rename from tags.
package normalize

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"go.senan.xyz/taglib"

	"meltk/internal/shared"
)

var (
	fixAlbum  bool
	fixTrack  bool
	recursive bool
	dryRun    bool
	verbose   bool
	fixed     int
	skipped   int
	failed    int
	wouldFix  int
)

// Run executes the normalize subcommand. args are the subcommand args
// (without the "normalize" prefix).
func Run(args []string) {
	// Reset counters so repeated calls (e.g. tests) start clean.
	fixed, skipped, failed, wouldFix = 0, 0, 0, 0

	fs := flag.NewFlagSet("normalize", flag.ContinueOnError)
	albumPtr := fs.Bool("album", false, "Normalize album directory names to \"YYYY - Album\" from metadata")
	trackPtr := fs.Bool("track", false, "Normalize track file names to \"NN. Title\" from metadata")
	allPtr := fs.Bool("all", false, "Normalize both album dirs and track files (default when no mode flag is given)")
	recursivePtr := fs.Bool("r", false, "Walk directories recursively and normalize every subfolder containing music")
	dryPtr := fs.Bool("dry-run", false, "Print what would be renamed without renaming")
	verbosePtr := fs.Bool("verbose", false, "Print skip reasons (off by default)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: meltk normalize [-r] [-album] [-track] [-all] [-dry-run] [-verbose] <dir>...")
		fmt.Fprintln(os.Stderr, "  No mode flags = -all (normalize both album dirs and track files).")
		fmt.Fprintln(os.Stderr, "  Album: normalizes album directory names to \"YYYY - Album\" from metadata.")
		fmt.Fprintln(os.Stderr, "  Track: normalizes file names to \"NN. Title\" (e.g. \"01. Uragan.flac\") from metadata.")
		fmt.Fprintln(os.Stderr, "  Without -r each <dir> is treated as one album folder.")
		fmt.Fprintln(os.Stderr, "  With -r every audio-bearing subfolder is normalized; the root itself is")
		fmt.Fprintln(os.Stderr, "  excluded when audio-bearing subfolders exist, and included as a fallback")
		fmt.Fprintln(os.Stderr, "  when it directly holds audio but no subfolder does (e.g. lone album dir, covers-only subfolders).")
		fmt.Fprintln(os.Stderr, "  Characters illegal on portable filesystems (< > : \" | ? *) are stripped; / and \\ become -;")
		fmt.Fprintln(os.Stderr, "  leading/trailing dots and surrounding spaces are trimmed. macOS artifacts (.DS_Store, ._) are skipped.")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			os.Exit(0)
		}
		os.Exit(2)
	}

	fixAlbum, fixTrack = *albumPtr || *allPtr, *trackPtr || *allPtr
	if !fixAlbum && !fixTrack {
		fixAlbum, fixTrack = true, true
		fmt.Fprintln(os.Stderr, "(* no mode specified, assuming -all *)")
	}
	recursive, dryRun, verbose = *recursivePtr, *dryPtr, *verbosePtr

	roots := fs.Args()
	if len(roots) == 0 {
		fs.Usage()
		os.Exit(2)
	}

	for _, root := range roots {
		info, err := os.Stat(root)
		if err != nil {
			fmt.Printf("[FAIL] Cannot stat %q: %v\n", root, err)
			failed++
			continue
		}
		if !info.IsDir() {
			if fixTrack && shared.IsMusicFile(root) {
				normalizeFilePlans([]string{root})
			} else if shared.IsArtifact(root) {
				skipf(root, shared.FileSkipReason(root))
			} else {
				skipf(root, "not a directory")
			}
			continue
		}
		normalizeRoot(root)
	}

	if dryRun {
		fmt.Printf("\nFinished. Would fix %d path(s) (%d skipped, %d failed).\n", wouldFix, skipped, failed)
	} else {
		fmt.Printf("\nFinished. Fixed %d path(s) (%d skipped, %d failed).\n", fixed, skipped, failed)
	}
	if failed > 0 {
		os.Exit(1)
	}
}

// collectCandidates returns the album directories to normalize.
// Without -r it is just the root itself. With -r it is every audio-bearing
// subdirectory (root excluded); when no audio-bearing subdirectory exists,
// the root itself is the fallback candidate if it directly holds audio
// (e.g. a lone album dir, or one with only a covers subfolder).
// A folder that contains audio directly AND has album subfolders beneath it
// is treated as a container (e.g. an artist folder with loose singles plus
// album dirs) and is skipped, never renamed.
func collectCandidates(root string) []string {
	if !recursive {
		return []string{root}
	}

	dirSet := map[string]bool{root: true}
	hasAudio := map[string]bool{}

	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			fmt.Printf("[FAIL] Cannot access %q: %v\n", path, err)
			failed++
			return nil
		}
		if d.IsDir() {
			dirSet[path] = true
			return nil
		}
		if shared.IsMusicFile(d.Name()) {
			parent := filepath.Dir(path)
			dirSet[parent] = true
			hasAudio[parent] = true
		} else if verbose {
			fmt.Printf("[SKIP] %q: %s\n", path, shared.FileSkipReason(d.Name()))
			skipped++
		}
		return nil
	})

	var subCandidates []string
	rootHasAudio := hasAudio[root]
	for dir := range dirSet {
		if dir == root {
			continue
		}
		if !hasAudio[dir] {
			if verbose {
				fmt.Printf("[SKIP] %q: no audio files inside\n", dir)
				skipped++
			}
			continue
		}
		if hasAlbumSubfolder(dir, hasAudio) {
			skipf(dir, "contains album subfolders; not renaming container")
			continue
		}
		subCandidates = append(subCandidates, dir)
	}
	if len(subCandidates) == 0 {
		// Fallback: root itself when nothing nested holds audio.
		if !rootHasAudio {
			if verbose {
				fmt.Printf("[SKIP] %q: no audio files inside\n", root)
				skipped++
			}
			return nil
		}
		if hasAlbumSubfolder(root, hasAudio) {
			skipf(root, "contains album subfolders; not renaming root (loose singles left as is)")
			return nil
		}
		return []string{root}
	}
	// Root excluded whenever audio-bearing subfolders exist.
	if verbose && rootHasAudio {
		fmt.Printf("[SKIP] %q: contains album subfolders; not renaming root (loose singles left as is)\n", root)
		skipped++
	}
	// Deepest directories first (depth, then lexical for determinism).
	sort.Slice(subCandidates, func(i, j int) bool {
		di, dj := shared.PathDepth(subCandidates[i]), shared.PathDepth(subCandidates[j])
		if di != dj {
			return di > dj
		}
		return subCandidates[i] < subCandidates[j]
	})
	return subCandidates
}

// hasAlbumSubfolder reports whether any other audio-bearing directory sits
// strictly beneath dir.
func hasAlbumSubfolder(dir string, hasAudio map[string]bool) bool {
	prefix := dir + string(os.PathSeparator)
	for other := range hasAudio {
		if hasAudio[other] && strings.HasPrefix(other, prefix) {
			return true
		}
	}
	return false
}

// renamePlan is a computed, not-yet-executed rename.
type renamePlan struct {
	oldPath string
	oldBase string
	newBase string
	newPath string
}

// normalizeRoot normalizes one root directory: track files first (inside
// every audio-bearing dir, including loose singles in container roots),
// then album directory renames deepest-first so child renames don't break
// parent paths.
func normalizeRoot(root string) {
	if fixTrack {
		normalizeFilePlans(collectTrackFiles(root))
	}
	if fixAlbum {
		normalizeDirPlans(collectCandidates(root))
	}
}

// collectTrackFiles returns every supported audio file to normalize.
// Without -r it is files directly inside root; with -r every nested audio
// file. Unlike collectCandidates there is no container exclusion: loose
// singles in a container root are still normalized.
func collectTrackFiles(root string) []string {
	if !recursive {
		return listAudioFiles(root)
	}
	var files []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			fmt.Printf("[FAIL] Cannot access %q: %v\n", path, err)
			failed++
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if shared.IsMusicFile(d.Name()) {
			files = append(files, path)
		} else if verbose {
			fmt.Printf("[SKIP] %q: %s\n", path, shared.FileSkipReason(d.Name()))
			skipped++
		}
		return nil
	})
	sort.Strings(files)
	return files
}

// listAudioFiles returns supported audio files directly inside dir, sorted.
func listAudioFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		fmt.Printf("[FAIL] Cannot access %q: %v\n", dir, err)
		failed++
		return nil
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if shared.IsMusicFile(e.Name()) {
			files = append(files, filepath.Join(dir, e.Name()))
		} else if verbose {
			fmt.Printf("[SKIP] %q: %s\n", filepath.Join(dir, e.Name()), shared.FileSkipReason(e.Name()))
			skipped++
		}
	}
	sort.Strings(files)
	return files
}

// normalizeFilePlans computes all desired file renames first, then executes
// only the collision-free ones. Files mapping to the same target (e.g. two
// files with identical TRACKNUMBER/TITLE tags) all fail together so no
// half-applied state is left behind.
func normalizeFilePlans(files []string) {
	var plans []*renamePlan
	for _, f := range files {
		plan, skipReason, failReason := computeFilePlan(f)
		if failReason != "" {
			failf(f, failReason)
			continue
		}
		if skipReason != "" {
			skipf(f, skipReason)
			continue
		}
		plans = append(plans, plan)
	}

	byTarget := map[string][]*renamePlan{}
	for _, p := range plans {
		byTarget[p.newPath] = append(byTarget[p.newPath], p)
	}
	var ready []*renamePlan
	for _, p := range plans {
		if len(byTarget[p.newPath]) > 1 {
			var sources []string
			for _, q := range byTarget[p.newPath] {
				sources = append(sources, fmt.Sprintf("%q", q.oldPath))
			}
			sort.Strings(sources)
			failf(p.oldPath, fmt.Sprintf("target %q claimed by multiple files (%s); refusing to rename any",
				p.newBase, strings.Join(sources, ", ")))
			continue
		}
		if _, err := os.Stat(p.newPath); err == nil {
			failf(p.oldPath, fmt.Sprintf("target %q already exists", p.newBase))
			continue
		}
		ready = append(ready, p)
	}

	sort.Slice(ready, func(i, j int) bool { return ready[i].oldPath < ready[j].oldPath })
	for _, p := range ready {
		renamePath(p.oldPath, p.newPath, p.oldBase, p.newBase)
	}
}

// normalizeDirPlans computes all desired dir renames first, then executes
// only the collision-free ones. Sibling folders mapping to the same target
// (e.g. Disc 1 / Disc 2 with identical ALBUM tags) all fail together so no
// half-applied state is left behind.
func normalizeDirPlans(candidates []string) {
	var plans []*renamePlan
	for _, dir := range candidates {
		plan, skipReason, failReason := computePlan(dir)
		if failReason != "" {
			failf(dir, failReason)
			continue
		}
		if skipReason != "" {
			skipf(dir, skipReason)
			continue
		}
		plans = append(plans, plan)
	}

	byTarget := map[string][]*renamePlan{}
	for _, p := range plans {
		byTarget[p.newPath] = append(byTarget[p.newPath], p)
	}
	var ready []*renamePlan
	for _, p := range plans {
		if len(byTarget[p.newPath]) > 1 {
			var sources []string
			for _, q := range byTarget[p.newPath] {
				sources = append(sources, fmt.Sprintf("%q", q.oldPath))
			}
			sort.Strings(sources)
			failf(p.oldPath, fmt.Sprintf("target %q claimed by multiple folders (%s); refusing to rename any",
				p.newBase, strings.Join(sources, ", ")))
			continue
		}
		if _, err := os.Stat(p.newPath); err == nil {
			failf(p.oldPath, fmt.Sprintf("target %q already exists", p.newBase))
			continue
		}
		ready = append(ready, p)
	}

	// Deepest first so child renames don't disturb parent paths.
	sort.Slice(ready, func(i, j int) bool {
		di, dj := shared.PathDepth(ready[i].oldPath), shared.PathDepth(ready[j].oldPath)
		if di != dj {
			return di > dj
		}
		return ready[i].oldPath < ready[j].oldPath
	})
	for _, p := range ready {
		renamePath(p.oldPath, p.newPath, p.oldBase, p.newBase)
	}
}

// computePlan derives the desired "YYYY - Album" name from metadata without
// touching the filesystem (other than reading tags).
func computePlan(dirPath string) (plan *renamePlan, skipReason, failReason string) {
	base := filepath.Base(dirPath)
	if base == "." || base == "/" || base == "" {
		return nil, "not a renamable directory", ""
	}

	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, "", fmt.Sprintf("cannot list directory: %v", err)
	}
	var audioFiles []string
	for _, e := range entries {
		if !e.IsDir() && shared.IsMusicFile(e.Name()) {
			audioFiles = append(audioFiles, filepath.Join(dirPath, e.Name()))
		}
	}
	if len(audioFiles) == 0 {
		return nil, "no audio files inside", ""
	}

	var albums, years []string
	readable := 0
	for _, f := range audioFiles {
		tags, err := taglib.ReadTags(f)
		if err != nil {
			if verbose {
				fmt.Printf("[SKIP] %q: could not read tags from %q: %v\n", dirPath, filepath.Base(f), err)
				skipped++
			}
			continue
		}
		readable++
		if album := strings.TrimSpace(shared.FirstTag(tags, taglib.Album)); album != "" {
			albums = append(albums, album)
		}
		if raw := strings.TrimSpace(shared.FirstTag(tags, taglib.Date, taglib.OriginalDate, taglib.ReleaseDate, "YEAR")); raw != "" {
			if y := shared.ParseYear(raw); y != "" {
				years = append(years, y)
			} else if verbose {
				fmt.Printf("[SKIP] %q: unparsable year %q in %q\n", dirPath, raw, filepath.Base(f))
				skipped++
			}
		}
	}
	if readable == 0 {
		return nil, "", "could not read tags from any audio file"
	}
	if len(albums) == 0 {
		return nil, "empty ALBUM tag", ""
	}

	album := shared.MostCommon(albums)
	if verbose && shared.DistinctCount(albums) > 1 {
		fmt.Printf("[SKIP] %q: inconsistent ALBUM tags, using most common %q\n", dirPath, album)
		skipped++
	}
	year := shared.MostCommon(years)
	if verbose && shared.DistinctCount(years) > 1 {
		fmt.Printf("[SKIP] %q: inconsistent year tags, using most common %q\n", dirPath, year)
		skipped++
	}

	var want string
	if year != "" {
		want = year + " - " + album
	} else {
		want = album
	}
	newBase := shared.SanitizeComponent(want)
	if newBase == "" || newBase == "." || newBase == ".." {
		return nil, "empty or unsafe target name", ""
	}
	if newBase == base {
		return nil, "already correct", ""
	}
	newPath := filepath.Join(filepath.Dir(dirPath), newBase)
	if newPath == dirPath {
		return nil, "already correct", ""
	}
	return &renamePlan{oldPath: dirPath, oldBase: base, newBase: newBase, newPath: newPath}, "", ""
}

// computeFilePlan derives the desired "NN. Title" file name from metadata
// without touching the filesystem (other than reading tags). The extension
// is preserved byte-for-byte; the number is always two digits (%02d).
func computeFilePlan(filePath string) (plan *renamePlan, skipReason, failReason string) {
	base := filepath.Base(filePath)
	ext := filepath.Ext(base)
	if base == "" || base == "." || base == "/" {
		return nil, "not a renamable file", ""
	}

	tags, err := taglib.ReadTags(filePath)
	if err != nil {
		return nil, "", fmt.Sprintf("could not read tags: %v", err)
	}
	title := strings.TrimSpace(shared.FirstTag(tags, taglib.Title))
	if title == "" {
		return nil, "empty TITLE tag", ""
	}
	rawNum := strings.TrimSpace(shared.FirstTag(tags, taglib.TrackNumber, "TRACKNUMBER", "TRACK"))
	num, ok := shared.ParseTrackNumber(rawNum)
	if !ok {
		if rawNum == "" {
			return nil, "empty TRACKNUMBER tag", ""
		}
		return nil, fmt.Sprintf("unparsable TRACKNUMBER %q", rawNum), ""
	}

	safeTitle := shared.SanitizeComponent(title)
	if safeTitle == "" || safeTitle == "." || safeTitle == ".." {
		return nil, "empty or unsafe target name", ""
	}
	newBase := fmt.Sprintf("%02d. %s", num, safeTitle) + ext
	if newBase == base {
		return nil, "already correct", ""
	}
	newPath := filepath.Join(filepath.Dir(filePath), newBase)
	if newPath == filePath {
		return nil, "already correct", ""
	}
	return &renamePlan{oldPath: filePath, oldBase: base, newBase: newBase, newPath: newPath}, "", ""
}

func renamePath(oldPath, newPath, oldBase, newBase string) {
	// Safety net: pre-checks in normalizeFilePlans/normalizeDirPlans should
	// have caught collisions, but re-check here to close the TOCTOU window.
	if _, err := os.Stat(newPath); err == nil {
		failf(oldPath, fmt.Sprintf("target %q already exists", newBase))
		return
	}
	if dryRun {
		fmt.Printf("[DRY] %q -> %q\n", oldPath, newPath)
		wouldFix++
		return
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		failf(oldPath, err.Error())
		return
	}
	fmt.Printf("[OK] %q -> %q\n", oldPath, newPath)
	fixed++
}

func skipf(path, reason string) {
	skipped++
	if verbose {
		fmt.Printf("[SKIP] %q: %s\n", path, reason)
	}
}

func failf(path, reason string) {
	failed++
	fmt.Printf("[FAIL] %q: %s\n", path, reason)
}
