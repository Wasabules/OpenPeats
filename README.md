# OpenPeats

Open control for **SoundPEATS** earbuds — ANC, equaliser, multipoint,
batteries — without going through the manufacturer's mobile app.

*Read this in [français](README.fr.md).*

Developed and validated on a **SOUNDPEATS H3** (firmware `S5_20250605_V0.4.9`)
on Ubuntu 24.04, then on Windows 11 and Android.

> **Unofficial project.** No affiliation with SoundPEATS. Provided without
> warranty.

## Contents

| Directory | Role |
|---|---|
| [`PROTOCOL.md`](PROTOCOL.md) | Protocol specification, with the verification status of every point |
| [`peatsaudio/`](peatsaudio/) | Desktop application (Wails + Go + Svelte) |
| [`android/`](android/) | Android application (Kotlin + Jetpack Compose) |
| [`probe/`](probe/) | Command line probe for exploring the protocol |

The protocol is implemented twice — in Go and in Kotlin — from the same
specification. That is deliberate: the ~350 lines of logic are cheap to port,
and each platform keeps its native transport with no bridging layer.
`PROTOCOL.md` remains the single reference.

The reusable core is [`peatsaudio/internal/peats`](peatsaudio/internal/peats):
pure protocol (frames, opcodes, equaliser encoding) and the RFCOMM/SPP
transport, with no dependency on the interface.

## Features

- Noise control: off / ANC / transparency, plus 4 sub-modes (adaptive, indoor,
  outdoor, traffic) and 2 transparency levels
- Parametric equaliser with 5/8/10 bands, a draggable curve and per-band Q
- LDAC and multipoint — presented as an exclusive choice, because the firmware
  drops LDAC the moment multipoint comes on
- Left / right / case batteries, with a desktop notification below a threshold
- Game mode, LE Audio, touch lockout, voice prompts
- Reading of the touch control mapping
- Raw frame console for exploration

The application queries every feature it has a control for and dims what does
not answer, so it adapts to models exposing a different subset.

## Install

Prebuilt binaries for Linux, Windows and Android are attached to every
[release](../../releases). Each one ships with a `SHA256SUMS` file.

| Platform | Artefact |
|---|---|
| Linux | `.deb`, `.tar.gz`, plus the `peatsctl` command line tool |
| Windows | NSIS installer, portable `.zip`, plus `peatsctl.exe` |
| Android | `.apk` (and `.aab` for store submission) |

## Build

### Desktop

```bash
cd peatsaudio
wails build -tags webkit2_41   # Ubuntu 24.04: webkit2gtk-4.1
./build/bin/peatsaudio
```

On Windows and macOS the `webkit2_41` tag does not apply: `wails build` is
enough. The earbuds must be paired and out of their case.

### Android

```bash
cd android
ANDROID_HOME=$HOME/Android/Sdk ./gradlew :app:assembleDebug
adb install -r app/build/outputs/apk/debug/app-debug.apk
```

## Languages

The interface is **English first** and translated at runtime; French ships in
the box. The desktop app picks the language up from the system and lets you
override it in the top bar, remembering the choice. Android follows the system
language through standard resource qualifiers.

Adding a language means adding catalogues, never touching application code:

| Where | Add |
|---|---|
| Desktop UI | `peatsaudio/frontend/src/locales/<lang>.json`, then list it in `frontend/src/lib/i18n.js` |
| Desktop notifications and `peatsctl` | `peatsaudio/internal/i18n/locales/<lang>.json` |
| Android | `android/app/src/main/res/values-<lang>/strings.xml` |

English is the source language everywhere; a missing key falls back to English
rather than showing a raw identifier. CI fails if a translation drifts from the
English catalogue — missing keys, extra keys, or mismatched `{placeholders}`.

Documentation is English only, except this README which is also available in
French. Code identifiers, commit messages and comments are English.

## Platforms

The protocol runs over RFCOMM (Bluetooth Classic, SPP profile). Only the
transport layer changes between systems; the logic is shared.

| System | Transport | Status |
|---|---|---|
| Linux | `AF_BLUETOOTH` / `BTPROTO_RFCOMM` | ✅ reads and writes validated |
| Android | `BluetoothSocket` (Kotlin) | ✅ reads and writes validated |
| Windows | Winsock `AF_BTH` | ✅ reads and writes validated |
| macOS | IOBluetooth | ❌ to do — see below |
| iOS | — | 🚫 impossible |

On Android, validation covers a full state read, an ANC mode change and
applying an equaliser curve, all checked on an H3.

On Windows 11, validation was done with `peatsctl` on an H3: connection on
RFCOMM channel 11, 9 reads out of 9, then an ANC mode change confirmed by
reading back and by ear. One trap surfaced there: `ws2bth.h` includes
`pshpack1.h`, so `SOCKADDR_BTH` is **packed to 1 byte** — 30 bytes, with
`btAddr` at offset 2. A naturally aligned struct puts `btAddr` at offset 8 and
makes Windows reject every `connect` with `WSAEADDRNOTAVAIL`, on every channel.

**macOS** is feasible but needs a cgo bridge to IOBluetooth, an Objective-C
framework with an asynchronous delegate API: there is no socket API like on
Linux and Windows, so the code cannot be written as a mirror of the other two
transports.

**Android** runs on a Linux kernel that does support `AF_BLUETOOTH`, but
SELinux and the permission model block raw Bluetooth sockets from the NDK. The
supported path is `android.bluetooth.BluetoothSocket`, on the Kotlin side.

**iOS** is out of reach whatever the framework: SPP there is reserved for
MFi-certified accessories, which these earbuds are not.

## Reverse-engineering status

The protocol was reconstructed by analysing the official Android application,
then **verified frame by frame** on real hardware. `PROTOCOL.md` consistently
separates what is confirmed on the wire from what remains inferred.

Two points are still unvalidated and are flagged as such in the interface:

- **writing** the touch mapping (reading it does work);
- the Android app's touch demo mode: the command is acknowledged, but taps
  never come back — touch is handled locally by the firmware.

## Legal

This project is about **interoperability**: it lets legally purchased hardware
work with an operating system the manufacturer does not support. Reverse
engineering for that purpose is explicitly permitted by EU directive 2009/24/EC
(art. 6) and, in the United States, by 17 U.S.C. §1201(f).

Accordingly, this repository **does not and will not contain**:

- the official application's APK or any part of it;
- decompiled code, smali, or extracted resources;
- manufacturer trademarks, icons or artwork.

All code published here was written independently from a description of the
observed behaviour. Those materials are excluded by [`.gitignore`](.gitignore).

## Licence

MIT — see [`LICENSE`](LICENSE).
