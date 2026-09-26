// Package restore implements `meltk restore`: surgical "_" placeholder repair from tags.
package restore

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

// Run executes the restore subcommand. args are the subcommand args
// (without the "restore" prefix).
func Run(args []string) {
	fixed, skipped, failed, wouldFix = 0, 0, 0, 0

	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	albumPtr := fs.Bool("album", false, "Restore \"_\" placeholders in directory names from album metadata")
	trackPtr := fs.Bool("track", false, "Restore \"_\" placeholders in file names from track title metadata")
	allPtr := fs.Bool("all", false, "Restore both album (dirs) and track (files) (default when no mode flag is given)")
	recursivePtr := fs.Bool("r", false, "Walk directories recursively and restore every nested file/dir")
	dryPtr := fs.Bool("dry-run", false, "Print what would be renamed without renaming")
	verbosePtr := fs.Bool("verbose", false, "Print skip reasons (off by default)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: meltk restore [-r] [-album] [-track] [-all] [-dry-run] [-verbose] <file|dir>...")
		fmt.Fprintln(os.Stderr, "  No mode flags = -all (restore both album dirs and track files).")
		fmt.Fprintln(os.Stderr, "  Restores \"_\" placeholders from tags (each \"_\" must align with a non-ASCII tag rune).")
		fmt.Fprintln(os.Stderr, "  Without -r each <dir> restores files directly inside, immediate child dirs, and the dir itself.")
		fmt.Fprintln(os.Stderr, "  With -r every audio-bearing subfolder is considered; the root itself is")
		fmt.Fprintln(os.Stderr, "  excluded when audio-bearing subfolders exist, and included as a fallback")
		fmt.Fprintln(os.Stderr, "  when it directly holds audio but no subfolder does.")
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
			if shared.IsArtifact(root) {
				skipf(root, shared.FileSkipReason(root))
			} else if fixTrack {
				restoreFilePlans([]string{root})
			} else if verbose {
				fmt.Printf("[SKIP] %q: file skipped (-album only)\n", root)
				skipped++
			}
			continue
		}
		walkRoot(root)
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

// renamePlan is a computed, not-yet-executed rename.
type renamePlan struct {
	oldPath string
	oldBase string
	newBase string
	newPath string
}

// walkRoot collects audio files and candidate dirs, restores files first,
// then directories deepest-first so child renames don't break parent paths.
func walkRoot(root string) {
	var files, dirs []string
	if recursive {
		files = collectRecursiveFiles(root)
		dirs = collectRecursiveDirs(root)
	} else {
		files, dirs = collectShallow(root)
	}
	restoreCollected(files, dirs, root)
}

// collectShallow returns audio files directly inside root and immediate
// child directories.
func collectShallow(root string) (files, dirs []string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		fmt.Printf("[FAIL] Cannot access %q: %v\n", root, err)
		failed++
		return nil, nil
	}
	for _, e := range entries {
		p := filepath.Join(root, e.Name())
		if e.IsDir() {
			dirs = append(dirs, p)
			continue
		}
		if shared.IsMusicFile(e.Name()) {
			files = append(files, p)
		} else if verbose {
			fmt.Printf("[SKIP] %q: %s\n", p, shared.FileSkipReason(e.Name()))
			skipped++
		}
	}
	return files, dirs
}

// collectRecursiveFiles returns all nested audio files under root, sorted.
func collectRecursiveFiles(root string) []string {
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

// collectRecursiveDirs returns audio-bearing subdirectories under root
// (root excluded); when no audio-bearing subdirectory exists, the root
// itself is the fallback candidate if it directly holds audio.
// Container folders (audio directly inside plus album subfolders beneath)
// are skipped, never renamed: their "_" names cannot be safely attributed
// to a single album.
func collectRecursiveDirs(root string) []string {
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
		}
		return nil
	})

	var subs []string
	rootHasAudio := hasAudio[root]
	for dir := range dirSet {
		if dir == root {
			continue
		}
		if !hasAudio[dir] {
			continue
		}
		if hasAlbumSubfolder(dir, hasAudio) {
			skipf(dir, "contains album subfolders; not renaming container")
			continue
		}
		subs = append(subs, dir)
	}
	if len(subs) == 0 {
		if !rootHasAudio {
			return nil
		}
		if hasAlbumSubfolder(root, hasAudio) {
			skipf(root, "contains album subfolders; not renaming root (loose singles left as is)")
			return nil
		}
		return []string{root}
	}
	if verbose && rootHasAudio {
		fmt.Printf("[SKIP] %q: contains album subfolders; not renaming root (loose singles left as is)\n", root)
		skipped++
	}
	sort.Slice(subs, func(i, j int) bool {
		di, dj := shared.PathDepth(subs[i]), shared.PathDepth(subs[j])
		if di != dj {
			return di > dj
		}
		return subs[i] < subs[j]
	})
	return subs
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

func restoreCollected(files, dirs []string, root string) {
	if fixTrack {
		restoreFilePlans(files)
	} else if verbose {
		for _, f := range files {
			fmt.Printf("[SKIP] %q: file skipped (-album only)\n", f)
			skipped++
		}
	}

	if fixAlbum {
		var plans []*renamePlan
		// Deepest directories first for plan computation; final execution
		// re-sorts deterministically in restoreDirPlans.
		sort.Slice(dirs, func(i, j int) bool {
			di, dj := shared.PathDepth(dirs[i]), shared.PathDepth(dirs[j])
			if di != dj {
				return di > dj
			}
			return dirs[i] < dirs[j]
		})
		for _, d := range dirs {
			plan, skipReason, failReason := computeDirPlan(d)
			if failReason != "" {
				failf(d, failReason)
				continue
			}
			if skipReason != "" {
				skipf(d, skipReason)
				continue
			}
			plans = append(plans, plan)
		}
		// The root itself may be an album folder passed on the command line.
		if !recursive && root != "." && root != "./" && filepath.Base(root) != "/" {
			if plan, skipReason, failReason := computeDirPlan(root); failReason != "" {
				failf(root, failReason)
			} else if skipReason != "" {
				skipf(root, skipReason)
			} else {
				plans = append(plans, plan)
			}
		}
		restoreDirPlans(plans)
	}
}

// restoreFilePlans computes all desired file renames first, then executes
// only the collision-free ones.
func restoreFilePlans(files []string) {
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
	executePlans(plans, "file")
}

// restoreDirPlans executes precomputed dir plans collision-free,
// deepest-first.
func restoreDirPlans(plans []*renamePlan) {
	executePlans(plans, "folder")
}

// executePlans runs collision detection (multi-source same target fails
// together; existing targets fail) then renames deepest-first.
func executePlans(plans []*renamePlan, kind string) {
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
			failf(p.oldPath, fmt.Sprintf("target %q claimed by multiple %ss (%s); refusing to rename any",
				p.newBase, kind, strings.Join(sources, ", ")))
			continue
		}
		if _, err := os.Stat(p.newPath); err == nil {
			failf(p.oldPath, fmt.Sprintf("target %q already exists", p.newBase))
			continue
		}
		ready = append(ready, p)
	}

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

// computeFilePlan restores "_" placeholders in a file name from the TITLE tag.
// The leading track-number prefix ("01. ") and extension are preserved;
// only "_" runes are replaced, each must align with a non-ASCII tag rune.
func computeFilePlan(path string) (plan *renamePlan, skipReason, failReason string) {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)

	if !strings.Contains(stem, "_") {
		return nil, "no underscores in name", ""
	}

	tags, err := taglib.ReadTags(path)
	if err != nil {
		return nil, "", fmt.Sprintf("could not read tags: %v", err)
	}
	title := shared.FirstTag(tags, taglib.Title)
	if title == "" {
		return nil, "empty TITLE tag", ""
	}

	prefix, body := splitTrackPrefix(stem)
	fixedBody, reason, changed := swapUnderscores(body, title)
	if !changed {
		return nil, reason, ""
	}

	newBase := shared.SanitizeComponent(prefix+fixedBody) + ext
	if newBase == base {
		return nil, "already correct", ""
	}
	return &renamePlan{oldPath: path, oldBase: base, newBase: newBase, newPath: filepath.Join(filepath.Dir(path), newBase)}, "", ""
}

// computeDirPlan restores "_" placeholders in a directory name from album metadata.
// Supports bare "<Album>" folders and "<Artist> - <Album>" folders; only the
// album portion (and artist portion, if also underscored) is repaired.
func computeDirPlan(dirPath string) (plan *renamePlan, skipReason, failReason string) {
	base := filepath.Base(dirPath)
	if base == "." || base == "/" || base == "" {
		return nil, "not a renamable directory", ""
	}
	if !strings.Contains(base, "_") {
		return nil, "no underscores in name", ""
	}

	rep, err := representativeFile(dirPath)
	if err != nil {
		return nil, err.Error(), ""
	}
	tags, err := taglib.ReadTags(rep)
	if err != nil {
		return nil, "", fmt.Sprintf("could not read tags from %q: %v", filepath.Base(rep), err)
	}
	album := shared.FirstTag(tags, taglib.Album)
	if album == "" {
		return nil, "empty ALBUM tag", ""
	}

	var newBase string
	if idx := strings.LastIndex(base, " - "); idx >= 0 {
		left, right := base[:idx], base[idx+len(" - "):]
		artist := shared.FirstTag(tags, taglib.AlbumArtist, taglib.Artist)
		newLeft, leftChanged, leftOK := optionalPart(left, artist, "artist")
		if !leftOK {
			return nil, leftChanged, "" // leftChanged carries the reason here
		}
		fixedRight, reason, changed := swapUnderscores(right, album)
		if !changed {
			return nil, "album portion: " + reason, ""
		}
		newBase = shared.SanitizeComponent(newLeft + " - " + fixedRight)
	} else {
		fixed, reason, changed := swapUnderscores(base, album)
		if !changed {
			return nil, reason, ""
		}
		newBase = shared.SanitizeComponent(fixed)
	}

	if newBase == base || newBase == "" {
		return nil, "already correct", ""
	}
	return &renamePlan{oldPath: dirPath, oldBase: base, newBase: newBase, newPath: filepath.Join(filepath.Dir(dirPath), newBase)}, "", ""
}

// optionalPart fixes an artist portion only if it contains "_".
// Returns (fixedOrOriginal, reasonIfSkipped, ok).
func optionalPart(part, tag, what string) (string, string, bool) {
	if !strings.Contains(part, "_") {
		return part, "", true
	}
	if tag == "" {
		return "", fmt.Sprintf("empty %s tag for %q", what, part), false
	}
	fixed, reason, changed := swapUnderscores(part, tag)
	if !changed {
		return "", fmt.Sprintf("%s portion: %s", what, reason), false
	}
	return fixed, "", true
}

// swapUnderscores replaces each "_" in broken with the rune-aligned character
// from reference. Anything else must match exactly. A "_" is only replaced by
// a non-ASCII reference rune, so genuine underscores are never clobbered.
func swapUnderscores(broken, reference string) (fixed, reason string, changed bool) {
	rb, rr := []rune(broken), []rune(reference)
	if len(rb) != len(rr) {
		return "", fmt.Sprintf("length mismatch (name %d runes, tag %d runes)", len(rb), len(rr)), false
	}
	out := make([]rune, 0, len(rr))
	touched := false
	for i := range rb {
		switch {
		case rb[i] == '_' && rr[i] == '_':
			out = append(out, '_')
		case rb[i] == '_':
			if rr[i] < 128 {
				return "", fmt.Sprintf("tag has ASCII %q at underscore position %d", string(rr[i]), i), false
			}
			out = append(out, rr[i])
			touched = true
		case rb[i] != rr[i]:
			return "", fmt.Sprintf("mismatch at position %d (%q vs tag %q)", i, string(rb[i]), string(rr[i])), false
		default:
			out = append(out, rb[i])
		}
	}
	if !touched {
		return "", "no underscores to fix", false
	}
	return string(out), "", true
}

// splitTrackPrefix splits a "01. Title" style stem into its numeric prefix
// and the title body. Prefix = 1-3 digits followed by ".", "-" or spaces.
func splitTrackPrefix(stem string) (prefix, body string) {
	i := 0
	for i < len(stem) && stem[i] >= '0' && stem[i] <= '9' {
		i++
	}
	if i == 0 || i > 3 {
		return "", stem
	}
	j := i
	for j < len(stem) && (stem[j] == '.' || stem[j] == '-' || stem[j] == ' ') {
		j++
	}
	if j == i {
		return "", stem
	}
	return stem[:j], stem[j:]
}

// representativeFile returns the first supported audio file directly inside dir.
func representativeFile(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("cannot list directory: %v", err)
	}
	for _, e := range entries {
		if !e.IsDir() && shared.IsMusicFile(e.Name()) {
			return filepath.Join(dir, e.Name()), nil
		}
	}
	return "", fmt.Errorf("no audio files inside")
}

func renamePath(oldPath, newPath, oldBase, newBase string) {
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
