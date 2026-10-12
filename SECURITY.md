# Security

## Reporting

Please report vulnerabilities **privately**, through GitHub's
[private vulnerability reporting](../../security/advisories/new) on this
repository, not in a public issue. You should get an answer within a week.

Include what you found, how to reproduce it, and the version and platform it
affects.

## Scope

Worth reporting:

- anything that lets a nearby Bluetooth device, or anything else outside the
  user's control, crash the desktop or Android app, make it send commands the
  user did not ask for, or reach the host beyond the RFCOMM link;
- a way to tamper with release artefacts or the release workflow — the
  published files carry a `SHA256SUMS`, and the workflow holds write access
  to the releases;
- a destructive command (reset, reboot) reachable without the confirmation
  the interface is supposed to ask for.

Out of scope: the earbuds' own firmware and the official application. This
project only talks to the earbuds; it cannot fix what runs on them.

## Supported versions

Only the latest release gets fixes.
