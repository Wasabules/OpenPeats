# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with
code in this repository.

## Language

The repository is **English first**: comments, error messages, test names,
documentation and commit messages are all English. Code identifiers are
English too.

User-facing strings are **never written inline**. They live in translation
catalogues, with English as the source language and French shipped alongside:

| Surface | Catalogue |
|---|---|
| Desktop UI | `peatsaudio/frontend/src/locales/*.json` |
| Desktop notifications, `peatsctl` | `peatsaudio/internal/i18n/locales/*.json` |
| Android | `android/app/src/main/res/values*/strings.xml` |

Adding a language must never require touching application code. English is the
fallback for any key a translation misses. Tests enforce catalogue parity —
missing keys, extra keys, mismatched `{placeholders}` — so a drifting
translation fails CI rather than shipping a raw key.

`README.fr.md` is the only translated document. The technical documents
(`PROTOCOL.md`, `peatsaudio/README.md`) are English only, deliberately:
duplicating a reference means maintaining two that eventually disagree.

## Commands

### Desktop application (`peatsaudio/`, Wails + Go + Svelte)

```bash
cd peatsaudio
wails build -tags webkit2_41   # Ubuntu 24.04: the tag is mandatory (no more webkit2gtk-4.0)
wails dev -tags webkit2_41     # development
```

On Windows and macOS the `webkit2_41` tag does not apply: `wails build` is
enough.

### Tests

```bash
cd peatsaudio
go test ./internal/... -v                          # unit tests, no hardware
go test ./internal/peats -run TestEncodeEQBand -v  # a single test
PEATS_MAC=98:80:BB:40:D4:ED go test ./internal/peats -run TestLive -v   # needs the earbuds
```

`frontend/dist/.gitkeep` is tracked on purpose: `main.go` does
`//go:embed all:frontend/dist`, and Wails loads that package to generate its
bindings *before* it compiles the frontend. With no directory the embed
pattern matches nothing and `wails build` fails before it starts, on a fresh
clone. The `all:` prefix is what makes a lone dotfile enough. A vite plugin
puts the file back after each build, since vite empties `outDir`.

On Linux `go vet ./...` still needs the GTK/WebKit headers, because the root
package reaches the Wails runtime through cgo. `go vet ./internal/... ./cmd/...`
covers the protocol and the CLI with no system dependency, which is what CI
runs.

The frontend needs the Wails bindings (`frontend/wailsjs/`, git-ignored):
`wails generate module` produces them without a full build.

### peatsctl — validating a transport with no GUI

```bash
cd peatsaudio
go build ./cmd/peatsctl
./peatsctl -addr 98:80:BB:40:D4:ED           # read only
./peatsctl -addr 98:80:BB:40:D4:ED -anc 1    # write the ANC mode, then read back
```

This is the tool for bringing up a platform whose transport has never run: it
exercises connection, reads and writes without depending on Wails.

### Android (`android/`, Kotlin + Compose)

```bash
cd android
ANDROID_HOME=$HOME/Android/Sdk ./gradlew :app:assembleDebug
adb install -r app/build/outputs/apk/debug/app-debug.apk
```

### Exploration probe (`probe/`, Linux only)

Standalone Go module, no dependencies, raw `AF_BLUETOOTH` sockets, carrying a
`//go:build linux` constraint. Read-only by default: any command outside the
allow-list requires `-allow-write`. It exists to explore the protocol, not to
drive the earbuds — its output is developer-facing and stays English, with no
i18n.

### CI/CD (`.github/workflows/`)

`ci.yml` runs on every push and pull request: Go vet and tests on Linux and
Windows, catalogue parity, frontend build, Android debug build.

`release.yml` runs on a `v*` tag and produces the Linux, Windows and Android
artefacts plus `SHA256SUMS`. Version numbers come from the tag; nothing is
hard-coded in the build files.

## Architecture

### A deliberate double implementation

The protocol is written **twice** — in Go (`peatsaudio/internal/peats/`) and in
Kotlin (`android/app/src/main/kotlin/com/openpeats/protocol/`) — from the same
specification. This is not debt: ~350 lines that are cheap to port, and each
platform keeps its native transport with no bridging layer.

**`PROTOCOL.md` is the single reference.** Any protocol correction must land
there first, then be carried into both implementations. It consistently
separates what is *confirmed on the wire* from what remains *inferred* — keep
that distinction: it is what makes the document trustworthy.

### Layers (Go)

```
internal/peats/protocol.go     frames, opcodes, EQ encoding — pure, no I/O
internal/peats/client.go       exchanges, retry, channel discovery — no OS
internal/peats/transport.go    rfcommConn interface + var dialRFCOMM
internal/peats/transport_*.go  one implementation per OS, chosen by build tag
internal/i18n/                 catalogue for what Go renders itself
message.go                     Message: the {key, args} pairs Go emits
app.go                         application state + Wails-bound methods
frontend/src/                  Svelte interface
```

`dialRFCOMM` is a global variable each `transport_*.go` fills in from its
`init()`. Adding a platform means adding a file with the right build tag and
implementing `rfcommConn` + `isTimeout`; nothing else moves.
`transport_stub.go` (`//go:build !linux && !windows`) returns `ErrUnsupported`.

### The i18n boundary between Go and the frontend

Go **never sends display text across the Wails boundary**. `app.go` emits
`Message{Key, Args}` values and the frontend renders them from its own
catalogue. Keeping one catalogue per renderer means a string is never
translated twice in two places that could drift apart.

An argument value starting with `@:` is itself a catalogue key (the
linked-message convention), which is how a nested reason stays translated
inside a sentence Go assembled.

Consequence to preserve: switching language re-renders the whole log
retroactively, because the lines were never text to begin with.

### Transport status

| System | Transport | Status |
|---|---|---|
| Linux | `AF_BLUETOOTH` / `BTPROTO_RFCOMM` | validated on hardware |
| Android | `BluetoothSocket` (Kotlin) | validated on hardware |
| Windows | Winsock `AF_BTH` | validated on hardware (reads + writes) |
| macOS | IOBluetooth | to do — no socket API, needs a cgo bridge |
| iOS | — | impossible (SPP reserved for MFi) |

Transports cannot be derived from one another: writing the Windows transport as
a mirror of Linux introduced two bugs there (an aligned struct instead of a
packed one, `select` without `exceptfds`). The per-platform traps are
documented in `peatsaudio/README.md`.

## Protocol traps

These silently break any client that ignores them. They are detailed in
`peatsaudio/README.md` and `PROTOCOL.md` section 7.

- **Never retry a write.** Several `set` commands take effect *without*
  acknowledging (ANC mode, multipoint). Resending one replays the side effect —
  three voice prompts for a single intended change. To check a `set` landed:
  **read back**. `get` commands, by contrast, are retried (3 times): they have
  no side effects and replies are intermittent.
- **The ack of a `set` carries the reply opcode of the `get`.** `03 11` answers
  `83 10`, not `83 11`. Waiting on `83 11` hangs forever.
- **The EQ escapes the general conventions**: `0E 02` answers `0F 82` (not
  `8E 02`), and uses `vendorId 001D` instead of `000A`.
- **Asymmetric payloads**: replies encode scalars as two big-endian bytes
  (`00 64` = 100), writes take one byte.
- **A ~700 ms settle delay** after the link comes up, otherwise the first
  commands get no reply.
- **Do not close then reopen a channel**: the earbuds refuse an immediate
  reconnection. `dialAny` therefore keeps the socket of the first successful
  connection.
- **LDAC and multipoint are exclusive** (the firmware drops LDAC itself); the
  UI presents them as a choice, not as two toggles.

## Conventions

**Feature detection rather than a hard-coded list.** `App.Refresh()` queries
every feature and fills `State.Supported`; the frontend dims whatever does not
answer. The H3 only exposes 16 commands out of 31 — other models will expose
others. Never hard-code a model's capabilities.

**Mark what is not verified.** The code explicitly flags the unconfirmed
(writing the touch mapping, `CUSTOMIZE_TOUCH_DEMO`), and the UI carries that
through. Keep those warnings until hardware settles them.

**Destructive commands** (`CmdReset`, `CmdReboot`): never sent without explicit
user confirmation.

## Legal constraint — strict

This project falls under interoperability (EU directive 2009/24/EC art. 6;
17 U.S.C. §1201(f)). The repository **must never contain**: the official APK or
any part of it, decompiled code or smali, or manufacturer resources and
trademarks. Those are excluded by `.gitignore` (`*.apk`, `jadx-out/`,
`smali-out/`, `*.dex`, `*.btsnoop`) and must stay local. All published code is
written independently from the observed behaviour.

Note the interaction with CI: `.gitignore` excludes `*.apk`, which is about
*the manufacturer's* APK. The release workflow uploads its own build artefacts
straight from the runner and never commits them, so the rule holds.
