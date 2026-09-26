# media-library-toolkit (`meltk`)

Single-binary media toolkit for audio metadata and file/directory name
manipulation. It merges three former standalone tools into one binary:

| Command     | Former tool        | What it does                                              |
| ----------- | ------------------ | --------------------------------------------------------- |
| `set-meta`  | `music-meta-fixer` | Batch-write `ALBUM` / `ARTIST` / `YEAR` tags              |
| `normalize` | `path-normalizer`  | Canonical rename from tags (`YYYY - Album`, `NN. Title`)  |
| `restore`   | `pathname-fixer`   | Restore `_` placeholders in names from tags               |

Supported audio formats: `mp3`, `flac`, `aac`, `m4a`, `opus`, `ogg`, `wav`.

## Build

Requires Go (see `go.mod` for the minimum version).

```sh
go build -o meltk .
```

Run `./meltk -h` for the command list, `./meltk <command> -h` for
per-command help.

## Typical workflow

For a directory copied from an old collection with mangled (`_` instead of
diacritics) names:

```sh
# 1. Restore "_" placeholders from TITLE/ALBUM tags (dry run first)
./meltk restore -dry-run -verbose "Artist - Album"
./meltk restore "Artist - Album"

# 2. Fix missing/wrong tags
./meltk set-meta -album "Your Album!" -artist "Your Artist" -year 2021 "Artist - Album"

# 3. Canonical rename from tags (dry run first)
./meltk normalize -dry-run -verbose "Artist - Album"
./meltk normalize "Artist - Album"
```

## Commands

### `meltk set-meta [-album NAME] [-artist NAME] [-year YYYY] [-dry-run] [-verbose] <dir>...`

Writes tags to the audio files directly inside each `<dir>`.

* `<dir>` is obligatory; use `"."` for the current directory. Multiple
  directories are allowed.
* At least one of `-album`, `-artist`, `-year` is required.
* `-artist NAME` writes both `ARTIST` and `ALBUMARTIST`.
* `-year` accepts a plain year or a fuller value (`2009-05-01`,
  `2021 Remaster`); the year (`1000`–`2999`) is extracted and stored in
  `DATE` (plus a `YEAR` alias). Values without a valid year are rejected.
* `-dry-run` previews writes; `-verbose` shows skip reasons.
* Existing tags are preserved (only the requested fields are overwritten).

```sh
./meltk set-meta -album "Your Album" -year 2021 .
./meltk set-meta -artist "Your Artist" -dry-run ./SomeAlbum
```

### `meltk normalize [-album] [-track] [-all] [-r] [-dry-run] [-verbose] <dir>...`

Renames from metadata to canonical form:

* album directories → `YYYY - Album` (bare `Album` when no year tag);
* track files → `NN. Title` (two-digit number, original extension kept).

Flags:

* `-album` / `-track` select what to normalize; `-all` selects both.
  With no mode flag, both are assumed (a notice is printed to stderr).
* `-r` walks recursively: every audio-bearing subfolder is a candidate.
  The root itself is excluded when audio-bearing subfolders exist, and used
  as a fallback when it directly holds audio but no subfolder does (e.g. a
  lone album dir, or one with only a `covers/` subfolder). Container folders
  (loose tracks plus album subfolders) are never renamed, but their loose
  tracks are still normalized.
* `-dry-run` prints `[DRY] old -> new` without renaming; `-verbose` shows
  skip reasons.
* Renames are planned first and executed only when collision-free: files or
  folders mapping to the same target all fail together, leaving no
  half-renamed state.

```sh
./meltk normalize -dry-run -verbose ./Music
./meltk normalize -r -all ./Music
./meltk normalize -track ./SingleAlbum
```

### `meltk restore [-album] [-track] [-all] [-r] [-dry-run] [-verbose] <file|dir>...`

Surgically restores `_` placeholders from tags: each `_` in a file/dir name
must align positionally with a non-ASCII rune in the `TITLE`/`ALBUM`
(respectively `ALBUMARTIST`/`ARTIST` for the artist part of
`Artist - Album` folders). Anything else must match exactly, so genuine
underscores are never clobbered. The `NN. ` track prefix and file extension
are preserved.

Mode (`-album`/`-track`/`-all`), recursion (`-r`), `-dry-run`, and `-verbose`
work as in `normalize`. Without `-r`, each `<dir>` covers files directly
inside, immediate child dirs, and the dir itself.

```sh
./meltk restore -dry-run -verbose "Y_u_ Alb_m"
./meltk restore -r ./Music
```

## Filename sanitizing

Names derived from tags are sanitized in `normalize` and `restore` so results
stay portable (Android SD cards, MTP, Windows/FAT/exFAT):

* `/` and `\` become `-`;
* `< > : " | ? *` and control characters are stripped entirely;
* surrounding spaces and leading/trailing dots are trimmed (leading dots
  would create hidden files);
* `_` is never introduced (it is `restore`'s placeholder mechanism).

macOS artifacts (`.DS_Store`, `.LSOverride`, `._*` AppleDouble files) are
always skipped and never treated as music, even when they carry an audio
extension.

## Exit codes

* `0` — success (no failures);
* `1` — completed with one or more failures (see `[FAIL]` lines);
* `2` — usage error (bad flags, missing required arguments).

`[SKIP]` lines (e.g. already-correct names, missing tags) are shown only
with `-verbose` but always counted in the final summary.

## Layout

```
main.go               # subcommand dispatcher
go.mod / go.sum
internal/
  shared/             # extensions, tag helpers, sanitizing, artifact filter
  setmeta/            # `meltk set-meta`
  normalize/          # `meltk normalize`
  restore/            # `meltk restore`
```
