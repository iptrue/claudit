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

## Run

In your project:

```sh
claudit
```

Arguments go on to claude, so `claudit --model haiku` works too.

The rest you'll figure out.
