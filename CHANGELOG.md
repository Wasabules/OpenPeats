# Changelog

Notable changes, newest first. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and this project
follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

The release workflow reads the section matching the tag it is building and
publishes it as the release body, so every version needs one before it is
tagged. Changes are recorded under Unreleased as they land; cutting a release
renames that heading.

## [Unreleased]

### Added

- The Windows installer is in French as well as English, following the
  Windows display language.

### Changed

- **The desktop app is called OpenPeats**, like the repository and the Android
  app. It used to ship as "PeatsAudio", which is the name of the manufacturer's
  own app: the two were easy to confuse, and the name was not ours to use. The
  program, its window, the Linux package and menu entry, the Windows installer
  and the release files all carry the new name, and the source moved from
  `peatsaudio/` to `desktop/`.
- Upgrading needs nothing by hand. The `.deb` replaces the old `peatsaudio`
  package, the Windows installer removes the PeatsAudio installation first, and
  the remembered earbuds address is read from the old settings folder. The
  interface language may need picking again once.

### Fixed

- Desktop: counts follow the plural rules of the interface language — "1
  bande", "10 bands" — instead of "band(s)".
- Desktop: an unexpected error is shown inside a translated sentence rather
  than as bare technical text, and a reconnection attempt failing for such a
  reason no longer logs a literal "{error}".

## [0.3.0] - 2026-10-13

The Android app installs at last: earlier releases shipped an unsigned APK,
which Android refuses. On both platforms the connection now survives the
earbuds rebooting — which LDAC, multipoint and LE Audio all make them do — and
closing it in the middle of an exchange.

These changes are covered by unit tests, including the client's exchange rules
run against a simulated link. The rewritten reconnection paths and the Android
permission flow have not yet been re-checked against real earbuds; reports are
welcome.

### Added

- **Android reconnects on its own** when the earbuds reboot, as the desktop
  app already did, instead of staying "connected" to a dead link. A Stop button
  ends the attempts.
- The desktop app **remembers the address** of the last earbuds it connected
  to.
- The raw frame console can send a frame under the equaliser's vendor id
  (`00 1D`).
- A contributing guide, a security policy with private vulnerability
  reporting, and issue forms that ask for the model, firmware and `peatsctl`
  output a protocol bug cannot be reproduced without.

### Changed

- **The APK is signed with the project's release key** (certificate SHA-256
  `8f33facd0fedbdce7ff1d399590c9bbc91c1114590cdb8d75a68b0b66be85f8f`), so every
  later release installs over this one.
- No earbuds address is built in any more. The desktop field used to default to
  the developer's own earbuds, so an empty field connected to them; it now
  starts empty and says where to find the address.
- Equaliser values are kept within the ranges the interface offers — 20 to
  20 000 Hz, ±12 dB, Q from 0.3 to 6 — on both platforms, right before they are
  encoded.
- The list of features absent from a model shows their names, translated,
  rather than internal identifiers. Equaliser values follow the interface
  language (1,5 in French).
- A release is only built once the whole CI passes on the tagged commit.

### Fixed

- **The release APK installs.** Without a signing key the build now falls back
  to the debug key instead of producing an unsigned file, so a fork's release
  installs too — thanks to @bukowa.
- Desktop: a link that dropped during the first 700 ms after connecting went
  unnoticed — the app showed it as connected and never reconnected.
- Desktop: a Disconnect clicked while an automatic reconnection was dialing was
  undone a few seconds later, and a Connect at that moment could leave two
  connections open, the stale one later cutting the good one.
- Desktop: a refresh on a model with missing features froze every other
  control for several seconds per missing feature, and a link drop during a
  refresh dimmed every card as unsupported.
- Desktop: a Q typed above 8 in an equaliser band went to the earbuds as a
  negative value.
- Desktop: a battery that ran low while notifications were off never raised
  the alert once they were turned back on.
- Linux: earbuds in their case or out of range are reported as unreachable
  within seconds, instead of after a sweep of every channel lasting up to
  90 seconds. Not yet observed on hardware.
- Android: tapping Close while the app waited for a reply crashed it.
- Android: a failed connection leaked its socket, and the fallback to RFCOMM
  channel 11 could never run.
- Android 12 and later: after a permission was refused for good, the app
  offered no way forward; it now offers to ask again or to open the settings.
  With Bluetooth off it says so instead of listing no device, and it no longer
  asks for the scan permission it never used.

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

[0.3.0]: https://github.com/Wasabules/OpenPeats/releases/tag/v0.3.0
[0.2.0]: https://github.com/Wasabules/OpenPeats/releases/tag/v0.2.0
[0.1.0]: https://github.com/Wasabules/OpenPeats/releases/tag/v0.1.0
