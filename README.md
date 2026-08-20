# slackmoji

A small macOS CLI for managing custom Slack emoji through your existing Google Chrome Slack session.

It reads Chrome's Safe Storage secret from your Keychain, decrypts only the selected Slack workspace cookies in memory, and discovers Slack's in-browser request token from Chrome local storage. No cookies or tokens are written to disk or displayed.

## Install

```sh
go install github.com/peterldowns/slackmoji@latest
```

### Download a binary

Visit [the latest GitHub release](https://github.com/peterldowns/slackmoji/releases/latest), or download a platform binary directly:

- [darwin-arm64](https://github.com/peterldowns/slackmoji/releases/latest/download/slackmoji-darwin-arm64)
- [darwin-amd64](https://github.com/peterldowns/slackmoji/releases/latest/download/slackmoji-darwin-amd64)
- [linux-arm64](https://github.com/peterldowns/slackmoji/releases/latest/download/slackmoji-linux-arm64)
- [linux-amd64](https://github.com/peterldowns/slackmoji/releases/latest/download/slackmoji-linux-amd64)

Or, from this checkout:

```sh
go run . --help
```

## Commands

`--workspace` is optional. When omitted, `slackmoji` shows the Slack workspaces it finds in Chrome and asks you to choose one.

```sh
# Upload
slackmoji --workspace cloudexchange-inc add party-parrot ./party-parrot.gif

# List all custom emoji
slackmoji list

# Search. Additional terms are passed to Slack as additional search queries.
slackmoji list party parrot
slackmoji --workspace cloudexchange-inc --page 2 --count 50 list shellder

# Complete list response, including pagination and metadata
slackmoji --workspace cloudexchange-inc --json list shellder

# Inline previews are automatic in Ghostty and iTerm2.
slackmoji list --images none              # Disable previews
slackmoji list --image-width 8 --image-height 4
slackmoji list --images kitty             # Force a supported protocol

# Permanent delete; --yes is required deliberately.
slackmoji --workspace cloudexchange-inc delete party-parrot --yes
```

Use `--profile "Profile 1"` before the command if the signed-in workspace is in a non-default Chrome profile.

## Requirements

- macOS with Google Chrome and an active Slack browser session
- Go 1.25+
- Permission to read the `Chrome Safe Storage` Keychain item when macOS asks

The tool uses Slack's browser-facing emoji endpoints, so Slack may change the request format in the future.
