# slackmoji

A small macOS CLI for managing custom Slack emoji. Use it to upload, search, and download custom Slack emojis.

The idea is it's an easy way to add new emojis programmatically. Use it to bulk-import.

## Authentication

The CLI uses your Google Chrome's Safe Storage secret from the macOS keychain in order to authenticate to the Slack API. It decrypts only the selected Slack workspace cookies in memory. No cookies or tokens or auth state is written to disk, displayed, or preserved outside of your existing keychain.

In order for this to work, you'll need:
- macOS with Google Chrome and an active Slack browser session
- Permission to read the `Chrome Safe Storage` Keychain item when macOS asks

## Install

#### Homebrew:
```bash
# install it
brew install peterldowns/tap/slackmoji
```

#### Download a binary:
Visit [the latest Github release](https://github.com/peterldowns/slackmoji/releases/latest) and pick the appropriate binary. Or, click one of the shortcuts here:
- [darwin-amd64](https://github.com/peterldowns/slackmoji/releases/latest/download/slackmoji-darwin-amd64)
- [darwin-arm64](https://github.com/peterldowns/slackmoji/releases/latest/download/slackmoji-darwin-arm64)
- [linux-amd64](https://github.com/peterldowns/slackmoji/releases/latest/download/slackmoji-linux-amd64)
- [linux-arm64](https://github.com/peterldowns/slackmoji/releases/latest/download/slackmoji-linux-arm64)

#### Golang:
I recommend installing a different way, since the installed binary will not
contain version information.

```bash
# run it
go run github.com/peterldowns/slackmoji@latest --help
# install it
go install github.com/peterldowns/slackmoji@latest
```

## Commands

```sh
# Upload
slackmoji --workspace cloudexchange-inc add party-parrot ./party-parrot.gif

# List/Search all custom emoji
slackmoji list

# Search. Additional terms are passed to Slack as additional search queries.
slackmoji list party parrot
slackmoji --workspace cloudexchange-inc --page 2 --count 50 list shellder

# Download an emoji's original image. The default filename uses the emoji name
# and the extension Slack provides; choose a path explicitly to override it.
slackmoji download party-parrot
slackmoji download party-parrot ./images/party-parrot.gif
slackmoji download party-parrot --force  # Replace an existing file

# Complete list response, including pagination and metadata
slackmoji --workspace cloudexchange-inc --json list shellder

# Inline previews are automatic in Ghostty and iTerm2.
slackmoji list --images none              # Disable previews
slackmoji list --image-width 8 --image-height 4
slackmoji list --images kitty             # Force a supported protocol

# Permanent delete; --yes is required deliberately.
slackmoji --workspace cloudexchange-inc delete party-parrot --yes
```

The `--workspace` argument is optional, and inferred automatically if not passed explicitly. If you're logged in to more than one Slack workspace, you'll be asked to choose which one.

Use `--profile "Profile 1"` before the command if the signed-in workspace is in a non-default Chrome profile.

## Scripting

The cli is scriptable, and has a `--json` flag for outputting JSON.

## Images

If you're in iTerm2 or Ghostty, listing/searching will show previews of the slack emojis in your terminal. Animations won't work but otherwise should be good to go.
