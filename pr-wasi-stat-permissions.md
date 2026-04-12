# Synthesize permission bits in WASI `os.Stat`/`os.Lstat` results

## Problem

TinyGo's `os.Stat` / `os.Lstat` on `wasip1` and `wasip2` return `FileMode` values with all permission bits set to zero (i.e. the low `0o777` of `Mode.Perm()` is `0`).

Mainline Go's wasip1 implementation deliberately synthesizes a default (directory → `0o700`, other → `0o600`). This divergence silently breaks real Go programs that check permission bits when ported from `GOOS=wasip1 go build` to `tinygo build -target=wasip1`.

Concrete repro: `mvdan/sh` (the Go-native shell interpreter)'s non-Unix `access()` implementation — the build path selected for wasip1 — checks `mode & 0o100` for `access_X_OK`, which is always false under TinyGo. Every `cd` into any directory inside the interpreter fails with "permission denied: …". This is reproducible with any TinyGo-built shell binary today.

## Verified sources (pinned to commits)

All references below use commit-pinned GitHub URLs so reviewers can confirm the asserted behaviour without chasing moving `HEAD`s.

- **Mainline Go** `src/os/stat_wasip1.go`, pinned to Go master [`027a5bf2`](https://github.com/golang/go/commit/027a5bf27f8a826d83c52e21f8adf7ec845bc59d) (2026-04-09). File contents are stable since [a1b5394d](https://github.com/golang/go/commit/a1b5394dba07cbb61c1f23c2610ef1a3be4c567b) (2024-11-19); the synthesis itself has been present since the wasip1 port landed in [66cac9e1](https://github.com/golang/go/commit/66cac9e1e4877ac7e66f888b0599a7a4a5787b76) (2023-03-25).
- **TinyGo** `src/os/stat_linuxlike.go`, pinned to upstream `dev` [`f4a6e037`](https://github.com/tinygo-org/tinygo/commit/f4a6e0373bd7b72509aefb1423cec67bbdb18eaa). File contents are stable since [3eee6869](https://github.com/tinygo-org/tinygo/commit/3eee686932d9b04534ea83bdbed7a7faf6f6b910) (2024-12-02, a nintendoswitch-only change). wasip1 was rolled into the Linux file in [a545f17d](https://github.com/tinygo-org/tinygo/commit/a545f17d2ea55807de4b97cf4f52f01d5d4f1815) (2023-08-10 "wasm: add support for GOOS=wasip1") and wasip2 was added in [9cb26347](https://github.com/tinygo-org/tinygo/commit/9cb263479c4b98f2d28889ca1acc297454e0d875) (2024-07-02).
- **TinyGo** `src/syscall/syscall_libc_wasi.go`, pinned to the same `dev` [`f4a6e037`](https://github.com/tinygo-org/tinygo/commit/f4a6e0373bd7b72509aefb1423cec67bbdb18eaa).
- **wasi-libc** `libc-bottom-half/cloudlibc/src/libc/sys/stat/stat_impl.h`, pinned to `main` [`aa9b5c08`](https://github.com/WebAssembly/wasi-libc/commit/aa9b5c0820fc32905198c6b38759092e1eb28380).
- **mvdan/sh** `interp/os_notunix.go`, pinned to `master` [`88fac5cb`](https://github.com/mvdan/sh/commit/88fac5cb7aed43534fe6239867b998181c52caf8). File itself last touched in [e97b2b0b](https://github.com/mvdan/sh/commit/e97b2b0b78d60a7f6566c85dfff2ab9a19437bcd) (2026-04-02).

## Evidence the asymmetry is on TinyGo's side

### Mainline Go explicitly synthesizes permission bits

[`src/os/stat_wasip1.go` L15-L43 @ `027a5bf2`](https://github.com/golang/go/blob/027a5bf27f8a826d83c52e21f8adf7ec845bc59d/src/os/stat_wasip1.go#L15-L43):

```go
func fillFileStatFromSys(fs *fileStat, name string) {
	fs.name = filepathlite.Base(name)
	fs.size = int64(fs.sys.Size)
	fs.modTime = time.Unix(0, int64(fs.sys.Mtime))

	switch fs.sys.Filetype {
	case syscall.FILETYPE_BLOCK_DEVICE:
		fs.mode |= ModeDevice
	// … other filetype cases …
	case syscall.FILETYPE_DIRECTORY:
		fs.mode |= ModeDir
	// …
	}

	// WASI does not support unix-like permissions, but Go programs are likely
	// to expect the permission bits to not be zero so we set defaults to help
	// avoid breaking applications that are migrating to WASM.
	if fs.sys.Filetype == syscall.FILETYPE_DIRECTORY {
		fs.mode |= 0700
	} else {
		fs.mode |= 0600
	}
}
```

The comment is the explicit design rationale we're arguing TinyGo should match.

### wasi-libc never fills the 0777 portion of `st_mode`

[`libc-bottom-half/cloudlibc/src/libc/sys/stat/stat_impl.h` L22-L67 @ `aa9b5c08`](https://github.com/WebAssembly/wasi-libc/blob/aa9b5c0820fc32905198c6b38759092e1eb28380/libc-bottom-half/cloudlibc/src/libc/sys/stat/stat_impl.h#L22-L67) (wasip1 path):

```c
#if defined(__wasip1__)
static inline void to_public_stat(const __wasi_filestat_t *in,
                                  struct stat *out) {
  // … asserts …
  *out = (struct stat){
      .st_dev = in->dev,
      .st_ino = in->ino,
      .st_nlink = in->nlink,
      .st_size = in->size,
      .st_atim = timestamp_to_timespec(in->atim),
      .st_mtim = timestamp_to_timespec(in->mtim),
      .st_ctim = timestamp_to_timespec(in->ctim),
  };
  // Convert file type to legacy types encoded in st_mode.
  switch (in->filetype) {
    case __WASI_FILETYPE_BLOCK_DEVICE:       break;
    case __WASI_FILETYPE_CHARACTER_DEVICE:   out->st_mode |= S_IFCHR; break;
    case __WASI_FILETYPE_DIRECTORY:          out->st_mode |= S_IFDIR; break;
    case __WASI_FILETYPE_REGULAR_FILE:       out->st_mode |= S_IFREG; break;
    case __WASI_FILETYPE_SOCKET_DGRAM:
    case __WASI_FILETYPE_SOCKET_STREAM:      out->st_mode |= S_IFSOCK; break;
    case __WASI_FILETYPE_SYMBOLIC_LINK:      out->st_mode |= S_IFLNK; break;
  }
}
```

The `*out = (struct stat){…}` compound literal zero-initializes every unnamed field, including `st_mode`; the subsequent switch OR's only filetype bits. The `0o777` portion is therefore always zero. The wasip2/wasip3 path (L102-L126 of the same file) has the same structure.

### TinyGo reads `st_mode & 0777` — which is always zero on WASI

[`src/os/stat_linuxlike.go` L7-L48 @ `f4a6e037`](https://github.com/tinygo-org/tinygo/blob/f4a6e0373bd7b72509aefb1423cec67bbdb18eaa/src/os/stat_linuxlike.go#L7-L48):

```go
// Note: this file is used for both Linux and WASI.
// Eventually it might be better to spit it up, and make the syscall constants
// match the typical WASI constants instead of the Linux-equivalents used here.

package os

import (
	"syscall"
	"time"
)

func fillFileStatFromSys(fs *fileStat, name string) {
	fs.name = basename(name)
	fs.size = fs.sys.Size
	fs.modTime = timespecToTime(fs.sys.Mtim)
	fs.mode = FileMode(fs.sys.Mode & 0777)    // ← always 0 on WASI
	switch fs.sys.Mode & syscall.S_IFMT {
	case syscall.S_IFBLK:
		fs.mode |= ModeDevice
	// … filetype cases …
	case syscall.S_IFDIR:
		fs.mode |= ModeDir
	// …
	}
	if fs.sys.Mode&syscall.S_ISGID != 0 {
		fs.mode |= ModeSetgid
	}
	// … ISUID, ISVTX …
}
```

The header comment at L7-L9 ("Eventually it might be better to spit it up, and make the syscall constants match the typical WASI constants") is an existing acknowledgement from a TinyGo contributor that sharing this body with WASI is suboptimal.

### TinyGo's `Lstat` goes through wasi-libc

[`src/syscall/syscall_libc_wasi.go` L248-L265 @ `f4a6e037`](https://github.com/tinygo-org/tinygo/blob/f4a6e0373bd7b72509aefb1423cec67bbdb18eaa/src/syscall/syscall_libc_wasi.go#L248-L265) defines `Stat_t` with a `uint32 Mode` field.

[`src/syscall/syscall_libc_wasi.go` L391-L398 @ `f4a6e037`](https://github.com/tinygo-org/tinygo/blob/f4a6e0373bd7b72509aefb1423cec67bbdb18eaa/src/syscall/syscall_libc_wasi.go#L391-L398):

```go
func Lstat(path string, p *Stat_t) (err error) {
	data := cstring(path)
	n := libc_lstat(&data[0], unsafe.Pointer(p))
	if n < 0 {
		err = getErrno()
	}
	return
}
```

So TinyGo's `Lstat` → `libc_lstat` → wasi-libc `to_public_stat` → `st_mode` with filetype bits only. TinyGo then reads the already-zero `0o777` portion and returns it to callers.

### Downstream breakage surface

[`mvdan/sh/interp/os_notunix.go` L19-L43 @ `88fac5cb`](https://github.com/mvdan/sh/blob/88fac5cb7aed43534fe6239867b998181c52caf8/interp/os_notunix.go#L19-L43):

```go
// access attempts to emulate [unix.Access] on Windows.
// Windows seems to have a different system of permissions than Unix,
// so for now just rely on what [io/fs.FileInfo] gives us.
func (r *Runner) access(ctx context.Context, path string, mode uint32) error {
	info, err := r.lstat(ctx, path)
	if err != nil {
		return err
	}
	m := info.Mode()
	switch mode {
	case access_R_OK:
		if m&0o400 == 0 {
			return fmt.Errorf("file is not readable")
		}
	case access_W_OK:
		if m&0o200 == 0 {
			return fmt.Errorf("file is not writable")
		}
	case access_X_OK:
		if m&0o100 == 0 {
			return fmt.Errorf("file is not executable")
		}
	}
	return nil
}
```

The `//go:build !unix` file covers wasip1. `access(X_OK)` checks `m & 0o100`. Under TinyGo this is always zero → every `cd` into a directory fails with "file is not executable".

Under mainline Go the same code path returns nonzero because of the synthesized `0o700` for directories — I verified this end-to-end with a `GOOS=wasip1 GOARCH=wasm` probe (`os.Lstat("/work")` returned `mode=20000000700`, i.e. `ModeDir | 0o700`).

## Proposed change

Split `src/os/stat_linuxlike.go` into two files along Go's existing line:

1. `src/os/stat_linux.go`: Linux-only build tag (the current Linux-relevant cases), unchanged behaviour.
2. `src/os/stat_wasi.go`: `wasip1 || wasip2` only, synthesizing permission bits to match mainline Go.

This keeps the Linux path untouched and turns the header comment's "eventually split it up" aspiration into the present. Use `git mv` so history follows the Linux content.

### `src/os/stat_wasi.go` (new)

```go
//go:build wasip1 || wasip2

// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package os

import (
	"syscall"
	"time"
)

func fillFileStatFromSys(fs *fileStat, name string) {
	fs.name = basename(name)
	fs.size = fs.sys.Size
	fs.modTime = timespecToTime(fs.sys.Mtim)
	switch fs.sys.Mode & syscall.S_IFMT {
	case syscall.S_IFBLK:
		fs.mode |= ModeDevice
	case syscall.S_IFCHR:
		fs.mode |= ModeDevice | ModeCharDevice
	case syscall.S_IFDIR:
		fs.mode |= ModeDir
	case syscall.S_IFIFO:
		fs.mode |= ModeNamedPipe
	case syscall.S_IFLNK:
		fs.mode |= ModeSymlink
	case syscall.S_IFREG:
		// nothing to do
	case syscall.S_IFSOCK:
		fs.mode |= ModeSocket
	}
	// WASI does not expose unix-style permission bits. Go programs that
	// migrate from other platforms commonly check Mode.Perm() (including
	// the standard library's own path-walking helpers and ecosystem code
	// that gates on the executable bit), and observing all-zero perms
	// produces spurious "permission denied"-style failures. Match mainline
	// Go's src/os/stat_wasip1.go fallback: 0700 for directories, 0600 for
	// everything else.
	if fs.sys.Mode&syscall.S_IFMT == syscall.S_IFDIR {
		fs.mode |= 0700
	} else {
		fs.mode |= 0600
	}
}

func timespecToTime(ts syscall.Timespec) time.Time {
	return time.Unix(int64(ts.Sec), int64(ts.Nsec))
}

// For testing.
func atime(fi FileInfo) time.Time {
	return timespecToTime(fi.Sys().(*syscall.Stat_t).Atim)
}
```

### `src/os/stat_linux.go` (renamed + build tag tightened)

Existing file contents with the build tag changed from

```
//go:build (linux && !baremetal && !wasm_unknown && !nintendoswitch) || wasip1 || wasip2
```

to

```
//go:build linux && !baremetal && !wasm_unknown && !nintendoswitch
```

No functional change on Linux; the `fs.mode = FileMode(fs.sys.Mode & 0777)` line stays because Linux does populate real perm bits. The obsolete "eventually it might be better to split it up" file-header comment is removed.

### Why the setuid/setgid/sticky handling is dropped on the WASI path

wasi-libc doesn't populate `S_ISGID`/`S_ISUID`/`S_ISVTX` in `st_mode` — see the `to_public_stat` switch linked above, which only OR's filetype bits. The current branches on those flags are dead on WASI; omitting them keeps the WASI code focused on what WASI actually provides.

## Test plan

### In-tree tests

- Run `make test TARGET=wasip1` and `make test TARGET=wasip2`; expect no regressions. [`src/os/os_chmod_test.go` L1 @ `f4a6e037`](https://github.com/tinygo-org/tinygo/blob/f4a6e0373bd7b72509aefb1423cec67bbdb18eaa/src/os/os_chmod_test.go#L1) is already gated `!wasip1 && !wasip2` and is unaffected. Other mode-related assertions in `os_anyos_test.go` use `IsDir()` / filetype checks, not specific permission bits.
- Add a new test (gated to `wasip1 || wasip2`) asserting `stat(tempdir).Mode().Perm() == 0700` and `stat(tempfile).Mode().Perm() == 0600`. This locks in the parity contract.

### Out-of-tree verification

Rebuild `shell.wasm` (mvdan/sh) with the patched TinyGo and confirm `cd /somepath && pwd` inside the guest no longer fails with "permission denied". This is the concrete downstream regression.

## Risks

- **Upstream might prefer a different structure** (single file with a build-tag-guarded helper). If so, rework and resubmit; the behaviour change is small enough that the file split is negotiable.
- **Silent observable change**: downstream code could have been structured around the current `Perm() == 0`. Since mainline Go returns nonzero perms and TinyGo advertises Go compatibility, this is unlikely to be a principled reliance. We should still call it out in the PR description.

## Non-goals

- Mirror Go's stdlib `src/os/stat_wasip1.go` verbatim. TinyGo's `syscall.Stat_t` on WASI uses Linux-style `S_IFMT` constants via wasi-libc musl, not mainline Go's `syscall.FILETYPE_DIRECTORY` enum, so filetype classification stays on the Linux-style switch.
- Touch `syscall.Chmod` or persist any permissions. `syscall.Chmod` on WASI already no-ops the mode argument; this change only reports a non-zero default, not stores one.

## Upstream context

No existing issue or PR covers this specific gap:

- `tinygo-org/tinygo` — searched `wasip1 stat`, `fillFileStatFromSys`, `wasi permission`, `FileMode`, `access X_OK`, and other permutations; nothing matches.
- `WebAssembly/wasi-libc` — no `st_mode` / `synthesize permissions` discussions.
- Closest adjacent: [`mvdan/sh#1318`](https://github.com/mvdan/sh/issues/1318) (2026-04-06, open) — `Runner.access bypasses StatHandler for virtual filesystems`. Same symptom surface for a different root cause (Unix path). The issue's author explicitly notes the non-Unix path *should* work correctly via `r.lstat`; TinyGo's zero-perm `Lstat` is what breaks that assumption in practice.

## Implementation steps

1. `git mv src/os/stat_linuxlike.go src/os/stat_linux.go`; tighten the build tag; remove the obsolete header comment.
2. Create `src/os/stat_wasi.go` with the `wasip1 || wasip2` build tag and the synthesizing `fillFileStatFromSys`.
3. Add a small wasi-gated test covering the directory / regular-file perm values.
4. Run the TinyGo wasip1 + wasip2 test suites locally.
5. Rebuild `mvdan/sh` against the patched TinyGo and verify `cd` inside a preopen-rooted path works.
6. Open a PR against `tinygo-org/tinygo` referencing this plan and the mvdan/sh repro.

## Downstream impact

- `apple-mcp` currently carries a workaround that avoids `cd`-into-preopen in the Wasm shell sandbox (documented in `WasmSandbox.swift`). Once this TinyGo fix lands and the vendored TinyGo toolchain picks it up, that workaround can be reverted and the `cd '/work' && <command>` wrap restored.
- The in-tree sh-wasi instrumentation added during diagnosis (extra error text in `changeDir`) is independent and should be reverted regardless — it was only there to surface the `aerr` value while we were chasing the wrong root cause.
