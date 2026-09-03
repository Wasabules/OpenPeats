# Changelog

Notable changes, newest first. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and this project
follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

The release workflow reads the section matching the tag it is building and
publishes it as the release body, so every version needs one before it is
tagged.

## [0.2.0] - 2026-09-03

### Changed

- The desktop interface is now built with **Svelte 5** and **Vite 8**, up from
  Svelte 3 and Vite 3. Every component was rewritten against the runes API:
  `export let` became `$props()`, reactive statements became `$derived`, and the
  `createEventDispatcher` / `on:apply` pairs became plain callback props. The
  translation store needed no change — Svelte 5 still supports stores, so
  `src/lib/i18n.js` and the whole `{key, args}` contract with Go are untouched.
- **Wails 2.15** and **Go 1.25**, matching the toolchains CI now installs
  (Go 1.25, Node 22).
- EQ curve handles carry `role="slider"` with their real value range instead of
  being anonymous circles, so the graph is no longer a black box to a screen
  reader. The faders below remain the keyboard-accessible control.

### Fixed

- Values crossing into Go — equaliser bands, touch mappings — are snapshotted
  before the call, so a reactive proxy is never handed to the Wails boundary.
- `jsconfig.json` no longer sets `importsNotUsedAsValues`, which TypeScript 5.5
  removed and which made editors report `TS5101` on every file.

### Removed

- `probe/peatsprobe`, a 2.3 MB compiled Linux binary that had been committed:
  the ignore rule meant to exclude it was misspelled, so an ordinary
  `go build ./...` inside `probe/` dirtied the working tree.
- `frontend/package.json.md5`, a Wails build cache rewritten on every build, and
  a dead `replace` directive in `go.mod` that leaked a local filesystem path.

## [0.1.0] - 2026-07-29

First release.

### Added

- The SoundPEATS control protocol over RFCOMM/SPP, written independently from
  the observed behaviour and documented in `PROTOCOL.md`, with the confirmed
  and the inferred kept apart.
- **PeatsAudio**, a Wails desktop application: ANC and transparency modes,
  codec and multipoint, game mode, touch mapping, voice prompts, low-battery
  notifications, a parametric equaliser and a raw frame console. Features are
  detected rather than hard-coded — whatever the model does not answer is
  dimmed.
- **peatsctl**, a command line tool for bringing up a transport with no GUI.
- An **Android** application in Kotlin and Compose, with its own implementation
  of the same protocol.
- Validated transports: Linux (`AF_BLUETOOTH`), Windows (Winsock `AF_BTH`) and
  Android (`BluetoothSocket`).
- An English-first interface translated at runtime, shipping English and
  French, with catalogue parity enforced in CI.
- CI on Linux and Windows, and a release workflow producing the Linux, Windows
  and Android artefacts plus `SHA256SUMS`.

[0.2.0]: https://github.com/Wasabules/OpenPeats/releases/tag/v0.2.0
[0.1.0]: https://github.com/Wasabules/OpenPeats/releases/tag/v0.1.0
