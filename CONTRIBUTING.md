# Contributing

Thanks for helping. Two kinds of contribution are worth the most here:
**captures from other models** — what `peatsctl` reads on earbuds other than
the H3 — and **transports** for platforms that have none yet (macOS).

## The legal rule — read this first

This project exists under the interoperability exceptions (EU directive
2009/24/EC art. 6; 17 U.S.C. §1201(f)). The repository must **never** contain:

- the official application's APK, or any part of it;
- decompiled code, smali, or anything pasted from them;
- the manufacturer's resources (images, strings, sounds) or trademarks.

Describe what the earbuds *do* — frames on the wire, observed behaviour — and
write the code from that description. A pull request carrying any of the above
is closed, however good the rest of it is. `.gitignore` already keeps the
usual suspects (`*.apk`, `jadx-out/`, `smali-out/`, `*.dex`, `*.btsnoop`) out;
keep them local.

## Changing the protocol

The protocol is implemented twice on purpose — Go in
`desktop/internal/peats/`, Kotlin in `android/.../protocol/` — and
[PROTOCOL.md](PROTOCOL.md) is the single reference. A protocol change goes, in
this order:

1. **PROTOCOL.md**, keeping its distinction between what is *confirmed on the
   wire* and what is *inferred* (⚠).
2. **`testvectors/protocol.json`**: a byte-level case for the new behaviour.
   Both test suites read this file, so it is what proves the two
   implementations agree.
3. **Both implementations**, in the same pull request.

Three rules break real earbuds when ignored — see section 7 of PROTOCOL.md:

- **Never retry a write.** Several `set` commands take effect without
  acknowledging; a resend replays the side effect. Read back instead.
- **Destructive commands** (reset, reboot) are never sent without an explicit
  confirmation from the user.
- **Mark what is not verified** on hardware, in the code and in the UI, until
  someone checks it. Say in the pull request which model and firmware you
  tested on.

## Strings

No user-facing text is written inline. It lives in a catalogue, English first,
French alongside:

| Surface | Catalogue |
|---|---|
| Desktop UI | `desktop/frontend/src/locales/*.json` |
| Desktop notifications, `peatsctl` | `desktop/internal/i18n/locales/*.json` |
| Android | `android/app/src/main/res/values*/strings.xml` |

Add every new key to both languages. If you do not speak French, add the
English text to the French file too and say so in the pull request: a
translator will pick it up, and CI fails on a missing key rather than on an
imperfect one. Adding a language never requires touching application code.

## Checks

CI runs all of this; running it first saves a round trip.

```bash
cd desktop
gofmt -l .                       # must print nothing
go vet ./internal/... ./cmd/...
go test -race ./internal/...     # -race needs a C toolchain; drop it otherwise

cd frontend
wails generate module            # once: the bindings are git-ignored (run from desktop/)
npm ci
npm run check                    # svelte-check
npm run check:locales            # catalogue parity, and the keys Go emits
npm run build

cd ../../android
./gradlew :app:testDebugUnitTest :app:assembleDebug :app:lintDebug
python3 ../.github/scripts/check-android-strings.py
```

Building and running the apps is described in
[desktop/README.md](desktop/README.md) and [CLAUDE.md](CLAUDE.md).

## Commits and pull requests

- English, everywhere: code, comments, commit messages, issues.
- One commit says one thing, with a lowercase scope prefix as in the history
  (`peats:`, `desktop:`, `frontend:`, `android:`, `protocol:`, `ci:`,
  `docs:`). The body explains *why*, for someone reading it in a year.
- Bug fixes come with the test that would have caught them where one can be
  written without hardware — the fake transport in
  `desktop/internal/peats/client_test.go` covers most client behaviour.

## Reporting a security issue

Not in a public issue — see [SECURITY.md](SECURITY.md).
