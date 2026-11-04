# Native Windows support

Goal: run ClaudIt natively on Windows 10/11 with Claude Code, preserving macOS/Linux behavior.

The implementation follows the Windows port outline approved in this conversation. Git operations
and publishing are out of scope. Existing Go UI and JSON protocol remain shared; OS-specific process
and clipboard operations use platform files. Native Windows execution is assigned to the added CI
workflow because this workspace runs macOS; that workflow has not been run from this session.

- [x] Process lifecycle: extract Unix process setup, implement Windows job-based child cleanup,
  adapt terminal-close signals and shell environment. Check Windows amd64/arm64 cross-builds and
  portable helper-process tests for arguments, stream output, cancellation, resume and descendants.
- [x] Clipboard and media: Windows text/image clipboard, portable image resizing, Windows pasted
  paths. Check real image decoding/size limits, Unicode text and quoted paths; preserve Unix behavior.
- [x] Paths and distribution: normalize separators and drive paths, adapt platform-dependent tests,
  add Windows ZIP releases and native Windows CI. Check package tests and release dry runs.
- [x] Integration: run formatting, vet, host tests without Git side effects, Windows test compilation,
  Linux/Windows cross-builds, and an independent review. Report native Windows checks as pending
  until actually run on Windows.

Review focus: immediate child spawning during cancellation; inherited stdout/stderr keeping a process
alive; paths containing spaces/non-ASCII text; clipboard encoding; preserving current macOS/Linux
shutdown and CLI environment behavior. Local external-Git integration tests may be skipped to honor
the user's instruction not to touch Git.

Verification: host `make dist VERSION=0.0.0 DIST=/tmp/claudit-release-check` passed vet, formatting,
tests and all six builds. `go test -race ./internal/claude` passed. Windows amd64 vet and compilation
of every package's tests passed. Windows amd64/arm64 ZIPs were inspected locally. The Git-dependent
explorer integration test was skipped behind a no-op Git shim.

Review fixes: bounded decoded image size before allocation; preserved EXIF orientation during JPEG
resizing; compared Windows plugin project paths without case sensitivity. Windows clipboard and
terminal interaction still require an actual Windows session. Native Claude Code (`claude.exe`) is
required; npm `.cmd` launchers are not supported. Windows uses protocol rate-limit events rather than
the Unix debug-log fallback.
