## What and why

<!-- What changes, and the reason — the commit messages can carry the detail. -->

## Tested on

<!-- Model and firmware, or "no hardware" — then say what is marked unverified. -->

## Checklist

- [ ] Nothing from the official application: no APK, decompiled code, smali, resources or trademarks ([CONTRIBUTING.md](../CONTRIBUTING.md#the-legal-rule--read-this-first)).
- [ ] Protocol changes: `PROTOCOL.md` first, a case in `testvectors/protocol.json`, then **both** the Go and Kotlin implementations.
- [ ] No write is retried; destructive commands still ask for confirmation.
- [ ] New user-facing strings are in the catalogues, English and French.
- [ ] What could not be checked on hardware is marked as unverified.
