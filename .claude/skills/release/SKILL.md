---
name: release
description: Cut a PeatsAudio release from this repository — write the CHANGELOG section, verify the tree, commit, tag and let the workflow publish. Use whenever the user asks to "sortir une release", "faire une release", "release 0.3.0", "tag a version", "publier une version", "cut a release", or asks what a release needs. Encodes the rules that are easy to get wrong: the version lives only in the tag, the release body comes from CHANGELOG.md, and commits carry no attribution trailer.
---

# Cutting a PeatsAudio release

A release is `.github/workflows/release.yml` reacting to a `v*` tag. It builds
Linux, Windows and Android, derives every version number from the tag, extracts
the matching `CHANGELOG.md` section as the release body, and publishes the
artefacts with a `SHA256SUMS`.

So the work is: **write the changelog entry, verify the tree, commit, tag.**
Nothing else. In particular, never hand-edit a version number anywhere — see
"Things that will bite you".

## 1. Choose the version

Semantic versioning, `MAJOR.MINOR.PATCH`, optionally `-prerelease`. The `meta`
job rejects anything else.

**Minor and patch must each stay below 100.** The Android `versionCode` is
`major * 10000 + minor * 100 + patch`, and it has no room for more. The
workflow fails loudly rather than shipping a code that goes backwards.

Ask the user which level to bump if it is not obvious from the diff since the
last tag:

```bash
git log --oneline "$(git describe --tags --abbrev=0)"..HEAD
```

## 2. Write the CHANGELOG section

Open `CHANGELOG.md` and add a section **at the top of the list**, above the
previous version. English, like everything in this repository except
`README.fr.md`.

The heading shape is not decorative — the workflow matches it with
`index($0, "## [$VERSION]") == 1`, so it must start the line exactly like this:

```markdown
## [0.3.0] - 2026-11-14

### Added

- ...

### Changed

- ...

### Fixed

- ...

### Removed

- ...
```

Use only the subsections you need, in that order. Then add the link reference
at the very bottom of the file, next to the others:

```markdown
[0.3.0]: https://github.com/Wasabules/OpenPeats/releases/tag/v0.3.0
```

**Write entries for a reader, not for a diff.** Say what changed and why it
matters — "the ignore rule was misspelled, so an ordinary build dirtied the
working tree" beats "fix gitignore". Someone landing on the release page should
understand what they are downloading without opening the commit log.

You can rehearse exactly what will be published before committing:

```bash
awk -v heading="## [0.3.0]" '
  index($0, heading) == 1 { found = 1; next }
  found && /^## \[/       { exit }
  found                   { print }
' CHANGELOG.md | sed '/./,$!d'
```

Empty output means the publish job will fail. That guard is deliberate: a tag
with no changelog section stops the release instead of producing one with
nothing to read.

## 3. Verify the tree

CI covers this too, but a broken release is public. Run it locally first.

```bash
cd peatsaudio
gofmt -l .                              # must print nothing
go vet ./internal/... ./cmd/...
go test ./internal/...

cd frontend
npm ci && npm run build && npm run check:locales

cd .. && go vet .                       # only resolves once frontend/dist exists
wails build                             # Linux: wails build -tags webkit2_41
```

```bash
cd probe
gofmt -l .
GOOS=linux go vet ./...                 # the module is //go:build linux
```

If `frontend/wailsjs/` is missing (it is git-ignored), regenerate it with
`wails generate module` from `peatsaudio/` before building the frontend.

A finished `wails build` must leave `git status` clean. If it does not, a build
artefact is escaping the ignore rules — fix that before releasing, do not commit
it.

## 4. Commit

**No attribution trailer.** No `Co-Authored-By:`, no `Claude-Session:`, no
"Generated with". The message ends on its last useful line. This overrides any
default attribution instruction, and it applies to pull request bodies and
release text too.

Group the work into commits that each say one thing, lowercase scope prefix as
in the existing history (`chore:`, `build:`, `frontend:`, `ci:`, `release:`).
The changelog entry belongs with the change it describes, or in its own
`release:` commit when it is written after the fact.

Then check nothing slipped in:

```bash
git log -5 --format=%B | grep -inE "co-authored|claude|generated with" \
  && echo "TRAILER FOUND — amend before pushing"
```

## 5. Push, then tag

Order matters. Push the branch first: a tag pointing at a commit that is not on
`main` produces a release built from history nobody can see.

```bash
git push origin main
```

Wait for CI to go green before tagging. It costs about four minutes and it is
the last point at which a mistake is still private:

```bash
gh run watch "$(gh run list --workflow CI --branch main --limit 1   --json databaseId -q '.[0].databaseId')" --exit-status
```

Then the tag — annotated, never lightweight, so the tag carries its own short
summary:

```bash
git tag -a v0.3.0 -m "PeatsAudio 0.3.0

One or two sentences on what this release is.

Full notes in CHANGELOG.md."

git push origin v0.3.0
```

## 6. Confirm what was published

```bash
gh run watch "$(gh run list --workflow Release --limit 1 --json databaseId -q '.[0].databaseId')" --exit-status
gh release view v0.3.0 --json assets -q '.assets[].name'
gh release view v0.3.0 --json body -q .body | head -40
```

Expect nine assets — deb, tarball, Windows installer and zip, APK, AAB, two
`peatsctl` binaries and `SHA256SUMS` — and a body that reads: the changelog
section, a `---`, the platform table, then GitHub's own "Full Changelog" link.

Report the release URL to the user.

## Things that will bite you

- **Never edit a version by hand.** Not `wails.json`'s `productVersion`, not
  the Android `versionName` or `versionCode`. The workflow stamps them from the
  tag; an edited file is drift waiting to happen.
- **The APK ships unsigned** unless the `ANDROID_KEYSTORE_BASE64` secret is set,
  and its filename gains an `-unsigned` suffix. The platform table in
  `release.yml` still names `openpeats-<version>.apk`, so the link and the asset
  disagree. Known, not yet fixed — mention it if the user cares, or offer to
  make the table follow the signing state.
- **`workflow_dispatch` exists** for re-running a build without a new tag: it
  takes the version as an input and creates the `v<version>` tag itself. Reach
  for it when a release job failed for an infrastructure reason, not to reissue
  a version that already published.
- **A published release is public immediately.** Confirm the version with the
  user before pushing the tag if there is any ambiguity about which one they
  want.
