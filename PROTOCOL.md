# PeatsAudio protocol — reverse engineering

Target: SOUNDPEATS H3 (`98:80:BB:40:D4:ED`)
Source: `com.xingkeqi.peats` v1.9.39 (versionCode 9921088), pulled over ADB root
from a Redmi Note 10 (LineageOS, `sweet`).

> **Status: static analysis.** Everything below is traced to a decompiled
> (jadx) or disassembled (baksmali) file. Nothing had been sent on the wire at
> the time of writing. Points marked ⚠ are inferences, not certainties.
> Section 7 records what was later confirmed against hardware.

Local artefacts: `peats-base.apk`, `peats-arm64.apk`, `jadx-out/`, `smali-out/`.
None of them are, or ever will be, in this repository — see the licensing
section of the README.

---

## 1. Architecture

The app embeds **4 chip SDKs** (Qualcomm GAIA, BES, JieLi, Bluetrum) and routes
by product model. Three `IDeviceInfo` implementations coexist:

| Implementation | Transport | Role |
|---|---|---|
| `DefaultBleDeviceInfoModel` | BLE GATT | generic path |
| `DefaultSppDeviceModel` | RFCOMM / SPP | generic path |
| `QcyBleDeviceModel` | BLE | QCY products |

**Central finding of the analysis: BLE and SPP produce strictly identical
frames.** Both call `BtDataUtilKt.generatePeatsPacket$default` with the same
`0x34` default mask. Only the transport differs.

Verified in smali:
- `smali-out/…/ble/DefaultBleDeviceInfoModel$sendDataEvent$2.smali`
- `smali-out/…/spp/DefaultSppDeviceModel$sendDataEvent$2.smali`

→ **On Linux, target RFCOMM/SPP.** It is the same protocol, without the LE
pairing constraints, and reachable with a plain
`AF_BLUETOOTH`/`BTPROTO_RFCOMM` socket.

### Transports

**SPP** — `SPP_DATA_UUID = 00001101-0000-1000-8000-00805F9B34FB` (standard
RFCOMM). The H3 does advertise that service, plus vendor UUIDs `eb04`→`eb07`
on BR/EDR.

**BLE GATT** — vendor base `D102-11E1-9B23-00025B00A5A5`:

| Role | UUID |
|---|---|
| Service | `00001100-D102-11E1-9B23-00025B00A5A5` |
| Write | `00001101-D102-11E1-9B23-00025B00A5A5` |
| Notify | `00001102-D102-11E1-9B23-00025B00A5A5` |

BLE address == A2DP address: **verified on the H3**. A `scan le` picks up
advertisements on `98:80:BB:40:D4:ED` (RSSI ~-53 dBm, ManufacturerData key
`0x000E`) while A2DP is active on the same address.

⚠ `getLdac`/`setLdac`/`getLeAudio`/`setLeAudio` are **empty stubs** in the BLE
model (they return `Unit`). Only the **SPP** model really implements them —
one more argument for targeting SPP.

---

## 2. Frame format

`BtDataUtilKt.generatePeatsPacket()` — `jadx-out/sources/…/util/BtDataUtilKt.java:62`

```
┌──────┬─────────┬───────┬────────┬──────────┬─────────┬─────────┐
│ SOF  │ VERSION │ FLAGS │ LENGTH │ VENDORID │ COMMAND │ PAYLOAD │
│ 0xFF │  0x04   │ 0x00  │  len   │ 2 bytes  │ 2 bytes │ N bytes │
└──────┴─────────┴───────┴────────┴──────────┴─────────┴─────────┘
   1        1        1       1         2          2         N
```

- `LENGTH` = length of the **payload alone**.
- **No checksum, no CRC.**
- `VENDORID` = `00 0A` by default — **except for the EQ, which uses `00 1D`**
  (see section 5).

Reply parsing (`buildPacket()`, line 94): identical split, offsets
`0 / 1 / 2 / 3 / 4-6 / 6-8 / 8+`.

### Conventions

- reply = `command[0] | 0x80` → a `get` of `03 XX` answers `83 XX`
- `set` = `get + 1` on the second byte (⚠ exceptions: `LE_AUDIO`,
  `WATER_DRAIN_FUNCTION`, `FIND_HEADPHONES_*`,
  `LEFT/RIGHT_EAR_MEDIA_VOLUME_CHANGE`, `PROMPT_VOLUME_LEVEL`)

### Examples

```
Read ANC mode        : FF 04 00 00 00 0A 03 10
Reply                : FF 04 00 01 00 0A 83 10 <mode>
Write ANC mode=ANC   : FF 04 00 01 00 0A 03 11 01
Left battery         : FF 04 00 00 00 0A 03 06
Reply                : FF 04 00 01 00 0A 83 06 <0-100>
Enable LDAC          : FF 04 00 01 00 0A 03 39 01
```

---

## 3. Command table

Source: `jadx-out/sources/…/data/enums/FunctionEnum.java`

Resolved constants (jadx had substituted symbols of the same numeric value):
`UPGRADE_PROCEED_TO_COMMIT`=14, `UPGRADE_COMMIT_REQ`=15,
`UPGRADE_SILENT_COMMIT_SUPPORTED_CFM`=33, `UPGRADE_SILENT_COMMIT_CFM`=34,
`CMDID_TESTMODE`=13, `CMDID_RESTART`=37, `Base64.padSymbol`=61.

### Toggles and modes

| Function | GET | REPLY | SET |
|---|---|---|---|
| `ANC_MODEL_DEFAULT` | `03 10` | `83 10` | `03 11` |
| `ANC_LEVEL` | `03 24` | `83 24` | `03 25` |
| `TRANSPARENT_LEVEL` | `03 4C` | `83 4C` | `03 4D` |
| `IN_EAR_DETECTION` | `03 0C` | `83 0C` | `03 0D` |
| `GAME_MODE` | `03 0E` | `83 0E` | `03 0F` |
| `MOVIE_MODEL` | `03 27` | `83 27` | `03 28` |
| `DOUBLE_DEVICE_CONNECT` (multipoint) | `03 14` | `83 14` | `03 15` |
| `DISABLE_ALL_TOUCHES` | `03 12` | `83 12` | `03 13` |
| `LIGHT_EFFECT_SWITCH` | `03 0A` | `83 0A` | `03 0B` |
| `PANORAMIC_SOUND_EFFECT_MODEL` | `03 29` | `83 29` | `03 30` |
| `SPACE_SOUND_EFFECT` | `03 57` | `83 57` | `03 58` |
| `DOLBY_SOUND_EFFECT` | `03 48` | `83 48` | `03 49` |
| `DYNAMIC_EQ_MODE` | `03 31` | `83 31` | `03 32` |
| `DYNAMIC_BASS` | `03 3C` | `83 3C` | `03 3D` |
| `ADAPTIVE_VOLUME` | `03 46` | `83 46` | `03 47` |
| `PERSONALIZED_VOLUME_SWITCH` | `03 40` | `83 40` | `03 41` |
| `PRIVACY_MODE` | `03 6B` | `83 6B` | `03 6C` |
| `FALL_DETECTION` | `03 55` | `83 55` | `03 56` |
| `WATER_DRAIN_FUNCTION` | `03 50` | `83 50` | `03 3E` ⚠ |
| `LDAC` | `03 38` | `83 38` | `03 39` |
| `LE_AUDIO` | `03 35` | `83 35` | `03 34` ⚠ |

### Volumes and levels

| Function | GET | REPLY | SET |
|---|---|---|---|
| `PROMPT_VOLUME_LEVEL` | `03 19` | `83 19` | `03 20` |
| `LEFT_EAR_MEDIA_VOLUME_CHANGE` | `03 42` | `83 42` | `03 44` |
| `RIGHT_EAR_MEDIA_VOLUME_CHANGE` | `03 43` | `83 43` | `03 45` |

### Read only

| Function | GET | REPLY |
|---|---|---|
| `BATTERY_LEFT_GET` | `03 06` | `83 06` |
| `BATTERY_RIGHT_GET` | `03 07` | `83 07` |
| `BATTERY_BOX_GET` | `03 23` | `83 23` |
| `VERSION_GET` | `03 09` | `83 09` |
| `CONN_CLASSIC_BT` | `03 17` | `83 17` |

### Actions (single command)

| Function | COMMAND |
|---|---|
| `RESET` | `03 05` |
| `REBOOT` | `03 16` |
| `RESET_CUSTOMIZE_TOUCH` | `03 AC` |
| `APPLY_EQUALIZER_V1` | `0E 01` |
| `APPLY_EQUALIZER_V2` | `0E 02` |

### Miscellaneous

| Function | GET | REPLY | SET |
|---|---|---|---|
| `CUSTOM_KEY` / `CUSTOMIZE_TOUCH_DEBUG` | `03 AA` | `83 AA` | `03 AB` |
| `SWITCH_VOICE_PACKAGE` | `03 21` | `83 21` | `03 22` |
| `FIND_HEADPHONES_LEFT` | `03 4A` | `83 4A` | `03 3A` |
| `FIND_HEADPHONES_RIGHT` | `03 4B` | `83 4B` | `03 3B` |

The ANC variants (`_WEAK_NOISE_REDUCTION`, `_NO_TRANSPARENCY_MODE`,
`_ADAPTIVE`, `FOCUS_MODEL`) share `03 10`/`03 11`; they only differ by the list
of modes the UI exposes per product.

---

## 4. Payload values

Source: `jadx-out/sources/…/data/enums/FunctionStatusEnum.java`

**Toggles** — `CLOSE` = `00`, `OPEN` = `01`

**ANC modes** (payload of `03 11`)

| Mode | Value |
|---|---|
| `ANC_NORMAL` (off) | `00` |
| `ANC_ANC` | `01` |
| `ANC_TRANSPARENT` | `02` |
| `ANC_ADAPTIVE` | `03` |
| `ANC_WEAK` | `04` |
| `ANC_CHASERS` | `01` |
| `NONE` | `FF` |

**ANC levels** (payload of `03 25`)

| Level | Value |
|---|---|
| `ANC_LEVEL_1` … `ANC_LEVEL_5` | `01` … `05` |
| `ANC_LEVEL_NORMAL_MODE` | `11` |
| `ANC_LEVEL_INDOOR_NOISE_REDUCTION` | `12` |
| `ANC_LEVEL_OUTDOOR_NOISE_REDUCTION` | `13` |
| `ANC_LEVEL_OUTDOOR_TRAFFIC` | `14` |
| `ANC_LEVEL_ADAPTIVE_NOISE_REDUCTION` | `15` |
| `ANC_LEVEL_ANTI_WIND_NOISE` | `16` |

⚠ The enum names `11` `NORMAL_MODE` and keeps a separate `15` for adaptive
cancellation, yet on the H3 the official UI labels the `11` entry *adaptive
cancellation* (section 7, list replies). Both implementations follow the
label: `11` is called adaptive. `15` never appeared in an H3 reply.

**Transparency levels** (payload of `03 4D`) — `VOICE_ENHANCE` = `01`,
`STANDARD_TRANSPARENT` = `02`

**Panoramic sound** (payload of `03 30`) — normal = `00`, music = `01`,
movie = `02`

**Voice packs** (payload of `03 22`) — EN=`00`, ZH=`01`, DE=`02`, JA=`03`,
FR=`04`, ES=`05`, IT=`06`

---

## 5. Equaliser

The EQ is **parametric**, not graphic. Each band is an
`EqPoint { freq: Int, gain: Double, q: Double }`
(`jadx-out/sources/…/data/model/EqPoint.java`).

⚠ **EQ frames use `vendorId = 00 1D`**, not `00 0A`. Seen in
`DefaultBleDeviceInfoModel$applyEqV3$2$1.java`: `sendDataEvent(setEqCommand,
eqPointToByteArray(...), new byte[]{0, 29}, ...)`.

**One band = one frame.** The app iterates over the points and sends one frame
per band.

### Encoding one band

`EqUtilKt.eqPointToByteArray(total, masterGain, index, point, isV1)` — line 87.

```
v1  : [ (total & 0x0F) << 4 | (index & 0x0F) ]        1 byte   → 9-byte frame
v2/3: [ total, index ]                                 2 bytes  → 10-byte frame

then, all big-endian int16:
  masterGain × 60     (2 b.)
  freq       × 3      (2 b.)
  gain       × 60     (2 b.)
  q          × 4096   (2 b.)
```

`index` is **1-based** (the caller passes `index + 1`).

**Value ranges.** Both clients clamp before encoding: freq 20–20000 Hz, gain
and masterGain −12…+12 dB, Q 0.3–6. ⚠ These are the clients' own limits, the
ranges their UIs offer, not limits read from the firmware. What they do
guarantee is that no value wraps around its field: `q × 4096` overflows an
int16 from Q = 8. Above 10922 Hz `freq × 3` no longer fits an int16 either;
it goes out as the same two bytes read unsigned (16 kHz → `BB 80`). ⚠ Whether
the firmware reads it unsigned is unverified — only 100 Hz was tested on the
wire.

### Version selection

`EqUtilKt.needInParallel(firmwareCode)` — line 140. Returns `true` (→ v1,
stacked on the classic EQ) if the chip is Qualcomm, or if the firmware is one
of: `CAPSULE3_PROV2`, `CAPSULE3_PRO`, `WINGS2`, `SPACE_PRO`, `AIR5_LLITE`,
`PearClipPro_Ldac`, `"S15"`, `"S22"`. Otherwise v2/v3, where the custom curve
is concatenated to the classic one.

⚠ The H3 firmware is not in that list → **v2/v3 expected**, to be confirmed by
reading `VERSION_GET` (`03 09`).

---

## 6. Touch controls

Payload of `03 AB`. Two enumerations, both 1-based.

**Gesture** (`CustomKeyOptionEnum`)

| Gesture | Code |
|---|---|
| Tap left / right | `01` / `02` |
| Double tap left / right | `03` / `04` |
| Triple tap left / right | `05` / `06` |
| Long press 1.5 s left / right | `07` / `08` |

**Action** (`CustomKeyFunctionEnum`)

| Action | Code |
|---|---|
| Volume + / − | `01` / `02` |
| Play / pause | `03` |
| Game mode | `04` |
| Previous / next track | `05` / `06` |
| ANC mode | `07` |
| Voice assistant | `08` |

⚠ `setCustomKey(byte[])` receives an array already built by the caller.
`SET_CUSTOMIZE_TOUCH_DEMO` is `{0x00, 0x00}`, which strongly suggests a 2-byte
`[gesture, action]` payload — **inferred, not confirmed**.

---

## 7. Confirmed on the wire ✅

Validated on 2026-07-29 with `probe/` against the H3 (firmware
`S5_20250605_V0.4.9`), over **RFCOMM/SPP**. SPP is therefore the right target:
it answers.

### Transport

`sdptool browse` returns nothing usable; discovery by channel sweep works. On
the H3: **channels 11 and 12 open**, channel 10 busy (`EBUSY`). Channel **11**
answers the protocol.

⚠ **Replies are intermittent.** The same command can return nothing and then
answer on the next attempt. Every client must implement a **retry** — do not
conclude "feature unsupported" from a single failure.

### Corrections to the static analysis

| Point | Static analysis | Measured reality |
|---|---|---|
| `version` in replies | assumed `0x04` | **`0x03`** (the request stays `0x04`) |
| Scalar payloads | assumed 1 byte | **2 bytes, big-endian** |

A battery reply is `00 64`, not `64`. An ANC mode is `00 02`, not `02`. The
useful value sits in the **last** byte.

### Readings

| Command | Raw reply | Decoding |
|---|---|---|
| `03 06` left battery | `FF 03 00 02 00 0A 83 06 00 64` | 100% |
| `03 07` right battery | `… 83 07 00 64` | 100% |
| `03 23` case battery | `… 83 23 00 5F` | 95% |
| `03 09` firmware | `… 83 09 00` + ASCII | `S5_20250605_V0.4.9` (¹) |
| `03 10` ANC mode | `… 83 10 00 02` | `02` = transparency |
| `03 38` LDAC | `… 83 38 00 01` | on |
| `03 35` LE Audio | `… 83 35 00 00` | off |
| `03 0E` game mode | `… 83 0E 00 00` | off |
| `03 14` multipoint | `… 83 14 00 00` | off |

(¹) The leading `00` is a status byte, not part of the version. Both clients
also drop trailing NUL and space padding, so a fixed-width field cannot leak
into the firmware-prefix match of section 5.

**Cross-validation**: `03 38` reports "LDAC on", which matches the LDAC codec
actually negotiated over A2DP (`api.bluez5.codec = "ldac"`, BlueZ transport
`Configuration` = Sony `0x012D` / codec `0x00AA`). Two independent paths, same
conclusion → the decoding is sound.

`03 0C` (in-ear detection) never answered across 3 attempts — the only feature
that genuinely looks absent from the H3.

### Composite replies

**`83 AA` touch controls** — `00 | 0102 0201 0303 0403 0504 0608 0707 0806`

8 `[gesture, action]` pairs after one header byte. **This confirms the
`[gesture, action]` format that was only an inference in section 6.**

| Gesture | Code | Action | Code |
|---|---|---|---|
| Tap left | `01` | Volume − | `02` |
| Tap right | `02` | Volume + | `01` |
| Double tap left | `03` | Play/pause | `03` |
| Double tap right | `04` | Play/pause | `03` |
| Triple tap left | `05` | Game mode | `04` |
| Triple tap right | `06` | Voice assistant | `08` |
| Long press left | `07` | ANC mode | `07` |
| Long press right | `08` | Next track | `06` |

**`83 24` ANC level** — `0011 0012 0113 0014 00`

First read as `[active, code]` pairs, which made `01 13` the active mode
(outdoor). **That reading was wrong**: writing showed the layout is one header
byte then `(code, active)` pairs — see *List replies* below — so this reply
says `12` (indoor) is active. The codes `11`, `12`, `13`, `14` do match the
scene modes of section 4.

**`83 4C` transparency level** — `00 01 00 02 01` (structure not yet worked
out at that point; resolved below).

### Writing ✅

First `set` validated on the wire (ANC mode, `02` → `01`):

```
→ FF 04 00 01 00 0A 03 11 01      (set, 1-byte payload)
← FF 03 00 02 00 0A 83 10 00 01   (ack, 2-byte payload)
```

Two asymmetries to carry into any implementation:

1. **The write payload is 1 byte**, while replies return 2. The app does send
   `FunctionStatusEnum.getStatus()` = `new byte[]{1}`.
2. **The acknowledgement uses the reply opcode of the `get`** (`83 10`), not
   `83 11`. A `set` is therefore confirmed by a state notification, not by an
   echo of the write command. A client must wait for `83 <get>`, not
   `83 <set>`.

Read back after the write: `83 10 00 01` — consistent. Human validation: the
"ANC MODE ON" voice prompt, and audibly effective noise cancelling.

### ⚠ NEVER retry a write

Several `set` commands **take effect without acknowledging**: ANC mode, ANC
sub-mode, multipoint. A client that reads the absence of a reply as a failure
and resends the command **replays the side effect**.

Observed in the field: a `03 11 02` write resent 3 times by retry logic
triggered **three "passthrough" voice prompts** for a single intended change.

**Rule**: retry `get` freely, never `set`. To check a write landed, **read the
state back** — do not resend the command.

### List replies: `header | (code, active) pairs`

Structure shared by `83 24`, `83 4C` and `83 AA`: one header byte, then pairs.

Confirmed by writing: `03 25 = 0x14` moves the `01` flag onto the pair of code
`14`, and `03 25 = 0x12` brings it back to `12`.

```
before: 00 | 11 00 | 12 01 | 13 00 | 14 00   → active = 12 (indoor)
after : 00 | 11 00 | 12 00 | 13 00 | 14 01   → active = 14 (traffic)
```

The 4 ANC sub-modes `11`/`12`/`13`/`14` match exactly the 4 entries of the
Android app: *adaptive cancellation*, *indoor*, *outdoor noise*, *traffic mode*
(confirmed by the user against the official UI).

`83 4C` decodes the same way: `00 | 01 00 | 02 01` → 2 transparency levels,
active = `02` (standard transparency).

### Equaliser ✅

**Validated on the wire, audible.** One +6 dB band at 100 Hz produces a clearly
perceptible bass lift; returning to 0 dB removes it.

```
→ FF 04 00 0A 00 1D 0E 02 01 01 0000 012C 0168 1000
                    └EQ vendorId  └cmd  └total,index, mg×60, f×3, g×60, q×4096
← FF 03 00 01 00 1D 0F 82 01
```

⚠ **The EQ family does not follow the `reply = request | 0x80` convention.**
Request `0E 02` → reply **`0F 82`** (first byte +1, second byte with bit 7
set). Payload `01` = success. A client waiting for `8E 02` would hang.

The encoding of section 5 is therefore correct as written, including the scale
factors `freq × 3`, `gain × 60`, `q × 4096`.

### Multipoint: side effect on the control channel

Enabling multipoint (`03 15 = 01`) works, but triggers a link reconfiguration:

1. the write does not acknowledge;
2. the RFCOMM channel briefly becomes `EBUSY`;
3. if a second device connects, **it monopolises the SPP control channel** and
   every channel disappears from the scan.

Observed: once multipoint was on, the phone (with the PeatsAudio app open) took
the link over and channels 11/12 vanished; they came back after turning the
phone's Bluetooth off. A desktop client must anticipate this and tell the user,
rather than reporting a failure.

### ⚠ LDAC and multipoint are mutually exclusive

Confirmed on the wire. Initial state: `03 38 = 01` (LDAC), `03 14 = 00`
(multipoint off), negotiated A2DP codec = **LDAC**.

After `03 15 = 01` (enabling multipoint), **with no LDAC command whatsoever**:

```
03 38  →  00 00   LDAC turned off automatically by the firmware
03 14  →  00 01   multipoint enabled
```

And the A2DP codec renegotiated by PipeWire falls back to **aptX**
(`api.bluez5.codec = "aptx"`).

The firmware arbitrates on its own: the bandwidth does not allow LDAC at
990 kbps over two simultaneous links. An honest UI must present these two
settings as an exclusive choice and explain the audio consequence, rather than
as two independent toggles.

### RFCOMM channel 12

Second SPP entry point, **identical protocol** — `03 06`, `03 10`, `03 09`
answer there exactly as on channel 11. Usable as a fallback.

### H3 support matrix

16 commands out of 31 answer reliably. The non-answers are reproducible over 3
attempts with the settle delay applied: they are genuinely absent features.

**Supported** — L/R/case batteries, firmware, ANC mode, ANC sub-mode,
transparency level, game mode, multipoint, LDAC, LE Audio, touch controls,
touch lockout, prompt volume (`00 32` = 50), voice pack (`00 00` = English),
classic BT connection, EQ.

**Absent from the H3** — in-ear detection, light effect, movie mode, panoramic
sound, dynamic EQ, dynamic bass, Dolby, spatial effect, per-ear volume,
personalised volume, adaptive volume, water drain, fall detection, privacy
mode.

### Intermittency: resolved

Cause identified: commands sent too soon after the RFCOMM link comes up get no
reply. **A ~600 ms delay after connecting** before the first send, plus reusing
the connection for subsequent commands, makes reads reliable (16/16 of the
supported features on a full sweep).

### Consequence for the EQ

Firmware `S5`: absent from the `needInParallel()` list (which holds `S15`,
`S22`…) → **EQ v2/v3**, as inferred in section 5. Later validated on writes.

⚠ The clients apply **half of that rule**. Nothing on the SPP link tells the
chip vendor apart, so the Qualcomm branch cannot be evaluated; only the
firmware list is, matched as a prefix of the version string (`S15_…`). A
Qualcomm model outside the list would get v2/v3 — untested, since the H3 is
the only model checked.

### Test vectors

`testvectors/protocol.json` turns this section into byte-level cases: frame
encoding and decoding, list replies, the firmware string, EQ bands and the
v1 selection. The Go and Kotlin test suites both read that file, so a case
added there checks the two implementations at once. Cases marked *wire* come
from the captures above; a vector that disagrees with this document is a bug
in the vector.

---

## 8. What remains uncertain

All the major points are closed. Remaining:

1. ~~Frames validated on the wire.~~ ✅ reads **and** writes.
2. `setCustomKey`: the `[gesture, action]` format is confirmed **on reads**;
   **writing was never tested** — the only functional point still open.
3. ~~Does the H3 answer on SPP?~~ ✅ channels 11 and 12.
4. ~~Accepted EQ version.~~ ✅ v2/v3, validated and audible.
5. ~~RFCOMM channel 12.~~ ✅ identical to 11.
6. ~~Structure of `83 4C`.~~ ✅ (code, active) pairs.
7. ~~Cause of the intermittency.~~ ✅ settle delay after connecting.
8. Semantics of `flags` (always `00` across every observed call).
9. Multi-band EQ: only one band was tested. The app iterates over N bands; the
   behaviour with `total > 1` and the ordering of `index` are unverified.
10. `masterGain`: always sent as 0 in the tests; its effect was not observed.
11. `handleData()` routes to `sceneLevelHandle()` for some payloads — not
    analysed.
12. No test of `RESET` (`03 05`) or `REBOOT` (`03 16`) — deliberately avoided.
