# OpenPeats — desktop SoundPEATS control

Desktop application (Wails + Go + Svelte) driving SoundPEATS earbuds through
the proprietary protocol reconstructed in [`../PROTOCOL.md`](../PROTOCOL.md).

Developed and validated on a **SOUNDPEATS H3**, firmware `S5_20250605_V0.4.9`,
on Ubuntu 24.04, then on Windows 11.

## Build

Ubuntu 24.04 no longer ships `webkit2gtk-4.0`. The build tag is mandatory
there:

```bash
wails build -tags webkit2_41
./build/bin/openpeats
```

Development: `wails dev -tags webkit2_41`. On Windows and macOS the tag does
not apply.

## Tests

```bash
go test ./internal/... -v                          # unit tests, no hardware
PEATS_MAC=98:80:BB:40:D4:ED go test ./internal/peats -run TestLive -v
```

The unit tests decode frames **actually captured** on the H3: battery reply,
ANC sub-mode, touch mapping, encoding of one EQ band. The i18n tests check the
translation catalogues against the English source.

On Linux, `go vet ./...` needs the GTK/WebKit headers because the root package
reaches the Wails runtime through cgo. `go vet ./internal/... ./cmd/...` covers
the protocol and the CLI with no system dependency, and is what CI runs.

`frontend/dist/.gitkeep` is tracked so the `//go:embed all:frontend/dist`
directive resolves on a fresh clone: Wails generates its bindings by loading
that package *before* compiling the frontend.

## peatsctl — validating a transport without a GUI

```bash
go build ./cmd/peatsctl
./peatsctl -addr 98:80:BB:40:D4:ED           # read only
./peatsctl -addr 98:80:BB:40:D4:ED -anc 1    # write the ANC mode, then read back
./peatsctl -addr 98:80:BB:40:D4:ED -lang fr  # French output
./peatsctl                                   # no address: lists the paired audio devices
```

This is the tool to use when bringing up a platform whose transport has never
run: it exercises connection, reads and writes without depending on Wails.

## Architecture

```
internal/peats/protocol.go     frames, opcodes, EQ encoding and limits — pure, no I/O
internal/peats/client.go       exchanges, retry, callbacks — no OS
internal/peats/transport.go    rfcommConn interface, var dialRFCOMM, channel sweep
internal/peats/transport_*.go  one implementation per OS, chosen by build tag
internal/peats/devices*.go     paired audio devices to pick from, one lister per OS
internal/i18n/                 catalogue for what Go renders itself
internal/freedesktop/          D-Bus: control service, panel icon + menu, BlueZ battery
internal/launch/               the command line: --background, --anc=N
message.go                     Message: the {key, args} pairs Go emits
app.go                         application state, lifecycle, language
session.go                     paired devices, connect, disconnect, recovery after a reboot
features.go                    feature table: refresh, writes, EQ, raw frames
battery.go                     level polling and low-battery alerts
settings.go                    what survives a restart (the last address)
desktop*.go                    the desktop around the window (Linux: desktop_linux.go)
integration*.go                installing that desktop integration for the user
gnome-extension/               Quick Settings toggle for GNOME Shell
frontend/src/                  Svelte interface
frontend/src/locales/          UI catalogues (source of truth: en.json)
```

`dialRFCOMM` is a global variable each `transport_*.go` fills in from its
`init()`. Adding a platform means adding a file with the right build tag and
implementing `rfcommConn` + `isTimeout`; nothing else moves.
`transport_stub.go` (`//go:build !linux && !windows`) returns `ErrUnsupported`.

`listPaired` follows the same pattern for the devices the window offers when
nothing is connected: Windows reads the system's paired list from
`bthprops.cpl`, Linux asks BlueZ over the system D-Bus. Where no file fills it
in, the list reports `ErrUnsupported` and the address field remains the way in.

## Linux desktop integration

Around the window, on Linux:

- **An icon in the top bar** with a menu: the batteries, the three noise
  control modes, connect or disconnect, open the window, quit. The lowest
  earbud's level sits next to the icon. GNOME draws it through the AppIndicator
  extension Ubuntu enables by default; KDE draws it natively.
- **A toggle in GNOME's Quick Settings**, from the extension in
  `gnome-extension/`: a click goes to the next mode, as the Android tile does,
  and its menu lists the modes and the batteries. When OpenPeats is not
  running, the first click starts it in the background through D-Bus
  activation.
- **The launcher's actions**: a right click on the OpenPeats icon in the dock
  switches the mode without opening the window.
- **The battery in GNOME's own menus**, through BlueZ, for earbuds that do not
  report it themselves. Most do, over the HFP audio link — the H3 does — and
  GNOME shows that already; OpenPeats then leaves it alone.

Closing the window keeps OpenPeats running in the panel, with the link up, when
the panel icon or the extension is there to bring it back; the first time, a
notification says so. Otherwise closing quits, as before. Quit is in the
panel menu.

All of these talk to one D-Bus service on the session bus,
`io.github.wasabules.OpenPeats`, which also keeps OpenPeats to a single
instance: launching it again shows the window of the one running.

```bash
gdbus introspect --session --dest io.github.wasabules.OpenPeats \
    --object-path /io/github/wasabules/OpenPeats
openpeats --anc=2          # transparency, through the running instance if any
openpeats --background     # no window; connects to the remembered earbuds
```

The `.deb` installs all of it system-wide. Otherwise the application installs
it for the current user: the **Desktop integration** card in the window (Linux
only), or the command line —

```bash
openpeats --install-integration   # install, update, and switch the extension on
openpeats --remove-integration    # take down what the line above put in place
```

Both write under `~/.local/share` only — the launcher entry with its actions,
its icon, the D-Bus activation file and the extension — pointing at the binary
that runs them, so a build run from its own directory integrates like a
package. Running it again after a rebuild elsewhere updates the files. What the
`.deb` installed is left alone. On Wayland, GNOME loads a newly installed
extension at the next login; the card says so.

The files themselves are the ones the `.deb` ships, embedded in the binary:
`build/linux/` for the launcher entry and the D-Bus service file,
`gnome-extension/` for the extension.

## Localisation

Three catalogues, split by **who renders the text**, so no string is ever
translated twice in places that could drift apart:

| Catalogue | Covers | Rendered by |
|---|---|---|
| `frontend/src/locales/*.json` | Everything in the window, including log lines and connection errors | The frontend |
| `internal/i18n/locales/*.json` | Desktop notifications, the panel icon's menu, `peatsctl` output | Go |
| `gnome-extension/locales/*.json` | The Quick Settings toggle | The extension, in the shell's language |

Go **never sends text to the frontend**. It sends `Message{Key, Args}` values
(see `message.go`); the frontend renders them at display time. Two consequences
worth knowing:

- Switching language re-renders the whole log, retroactively — the lines were
  never text to begin with.
- An argument whose value starts with `@:` is itself a catalogue key (the
  linked-message convention), which is how a nested reason stays translated
  inside a sentence Go assembled: `Reconnect 2/10: control channel busy`.

The frontend reports the chosen language to Go through `App.SetLanguage`, so a
desktop notification never arrives in a different language from the window that
triggered it.

## Protocol traps (learned the hard way)

These are not implementation details: they silently break any client that
ignores them.

**Never retry a write.** Several `set` commands take effect *without*
acknowledging (ANC mode, multipoint). Reading the absence of a reply as a
failure and resending the command replays the side effect — observed as three
voice prompts for a single intended change. To check a `set` landed: **read the
state back**.

**The acknowledgement of a `set` carries the reply opcode of the `get`.**
Writing `03 11` produces a `83 10` reply, not `83 11`. Waiting on `83 11` hangs
forever.

**The equaliser follows a different convention.** `0E 02` answers `0F 82` —
not the "bit 7 on the first byte" rule the rest of the protocol uses. And the
EQ uses `vendorId 001D` instead of `000A`.

**Payloads are asymmetric.** Read replies encode scalars as two big-endian
bytes (`00 64` = 100); writes take one byte.

**A settle delay is mandatory.** A command sent less than ~600 ms after the
RFCOMM link comes up gets no reply. Without that delay, reads look random.

**Do not close then reopen a channel.** The earbuds refuse an immediate
reconnection on the same channel (`EBUSY`). Channel discovery therefore keeps
the socket of the first successful connection instead of closing it.

**LDAC and multipoint are exclusive.** The firmware drops LDAC by itself when
multipoint comes on — there is not enough bandwidth for two links. The UI
presents them as a choice, not as two independent toggles.

**Multipoint can confiscate the control channel.** Once multipoint is on, the
second device (a phone with the official app open) monopolises SPP and every
channel disappears from the scan. The application detects this and explains it
instead of reporting a failure.

## Windows-specific traps

**`SOCKADDR_BTH` is packed to 1 byte.** `ws2bth.h` includes `pshpack1.h`: the
struct is 30 bytes and `btAddr` starts at offset 2, right after
`addressFamily`. An equivalent Go struct would be naturally aligned and put
`btAddr` at offset 8 — Windows then reads the address from the wrong place and
rejects every `connect` with `WSAEADDRNOTAVAIL` (10049), on every channel and
whatever the addressing mode. It is a silent, total failure that looks like an
absent device. The transport therefore serialises the struct by hand.

**A failed non-blocking `connect` reports through `exceptfds`.** On POSIX the
kernel makes the socket writable either way, so watching `writefds` is enough;
Winsock only signals the failure in `exceptfds`. Watching `writefds` alone
makes every closed channel wait out the full timeout — 90 s for one sweep —
before wrongly concluding a timeout.

**Error codes, measured against a reachable H3.** `WSAEHOSTDOWN` (10064) means
a closed channel, not an absent device: channels 1 and 12 return it while
channel 11 accepts the connection. `WSAEADDRINUSE` (10048) is the equivalent of
Linux's `EBUSY` — same busy channel 10. `WSAEADDRNOTAVAIL` (10049) on *every*
channel means the device is paired but not connected: "paired" is not enough,
Windows must have established the link.

**The Bluetooth API structs are not packed.** Unlike `SOCKADDR_BTH`,
`BLUETOOTH_DEVICE_SEARCH_PARAMS` and `BLUETOOTH_DEVICE_INFO` use natural
alignment — 40 and 560 bytes on 64-bit Windows, as Go lays them out. The API
checks `dwSize`, so a wrong layout fails outright rather than misreading; a
test pins both sizes, and the listing was checked against this machine's
paired devices.

**Low-battery notifications do not work.** `notifyLowBattery` calls
`notify-send`, which does not exist on Windows; the alert stays visible inside
the application only.

## Verification status

Everything is confirmed on the wire **except** what the interface marks
"unverified":

- **writing** the touch mapping (reading it is validated).

The touch debug mode (`CUSTOMIZE_TOUCH_DEMO`) was tested and dropped: the H3
acknowledges it but never reports a tap.

The rest — ANC, sub-modes, transparency, batteries, firmware, LDAC, multipoint,
LE Audio, game mode, touch lockout, voice prompts, equaliser — was exercised on
reads and, where applicable, on writes with audible verification.

## Features absent from the H3

Detected automatically on refresh and dimmed in the interface: in-ear
detection, light effect, movie mode, panoramic sound, dynamic EQ, dynamic bass,
Dolby, spatial effect, per-ear volume, personalised volume, adaptive volume,
water drain, fall detection, privacy mode.

Other SoundPEATS models sharing this protocol may expose them. The
application queries every feature it has a control for and only shows what
answers; supporting one of these means adding it to the feature table in
`features.go`, plus its UI.
