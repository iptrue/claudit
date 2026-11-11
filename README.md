# ClaudIt

ClaudIt is a terminal UI around [Claude Code](https://github.com/anthropics/claude-code). You talk to
claude as usual, and above the conversation every agent — the main one and its helpers — visibly works
in its own editor and terminal.

![ClaudIt: the main agent fixes slugify while two subagents write the tests and the docs side by side](docs/demo.gif)

## Install

You need Claude Code installed as `claude` and logged in. Then, on macOS or Linux:

```sh
brew install pixelbender/tap/claudit
```

Or with Go 1.27 or newer, into `$(go env GOPATH)/bin`:

```sh
go install github.com/pixelbender/claudit@latest
```

On Windows, use Windows Terminal and the native Claude Code installer so `claude.exe` is on your
`PATH` (the npm `claude.cmd` launcher is not supported). Install ClaudIt with `go install` above,
or download the Windows amd64 or arm64 ZIP from [Releases](https://github.com/pixelbender/claudit/releases), extract `claudit.exe`
and add its directory to `PATH`.

On Windows, usage limits come from Claude Code's standard rate-limit events. The Unix debug-log
fallback is not enabled automatically.

## Run

In your project:

```sh
claudit
```

Arguments go on to claude, so `claudit --model haiku` works too.

`/open 119816` finds an OpenSpec change or spec by name or card number, or a change by the
`domain/capability` of one of its deltas, in the copies of a specs repository under the project
folder, up to three levels down: the changes of your own copies (`specs-DEV-<card>`) first, newest
edit first, then the others and the main specs, those of the copy on main or master. A file you
open there, or click in the explorer, stays on screen while the agents work on; alt+0 or esc goes
back to them.

The rest you'll figure out.

## Build and test

```sh
go vet ./...
go test ./...
go build .
```

CI runs these checks on Windows, macOS and Linux. To prepare release archives on macOS or Linux
(with Go, Make, tar and zip installed), run `make dist VERSION=0.1.0`. It creates tar.gz archives
for macOS/Linux and ZIP archives containing `claudit.exe` for Windows, each for amd64 and arm64.
