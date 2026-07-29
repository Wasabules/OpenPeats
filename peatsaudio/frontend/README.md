# PeatsAudio frontend

Svelte + Vite interface for the desktop application. Built and embedded by
Wails; there is no standalone deployment.

```bash
npm install
npm run build     # produces dist/, which main.go embeds
npm run dev       # only useful under `wails dev`
```

`npm run build` needs the Wails bindings in `wailsjs/` (git-ignored). Generate
them without a full build:

```bash
cd .. && wails generate module
```

## Layout

```
src/App.svelte        window shell: top bar, tabs, panels
src/lib/i18n.js       translation store (~60 lines, no dependency)
src/lib/*.svelte      Card, Toggle, Segmented, EQEditor, TouchPanel
src/locales/en.json   source language — the reference catalogue
src/locales/fr.json   translation, falls back to English key by key
src/style.css         theme variables and shared element styles
```

## Strings

No user-facing string is written inline. Everything goes through `$t`:

```svelte
<script>
  import { t } from "./lib/i18n.js";
</script>

<h2>{$t("empty.title")}</h2>
<p>{$t("status.channel", { channel: state.channel })}</p>
```

Adding a language means adding `src/locales/<code>.json` and listing it in
`LOCALES` inside `src/lib/i18n.js`. Nothing else changes. The `i18n` job in CI
checks every catalogue against `en.json`: missing keys, extra keys and
mismatched `{placeholders}` all fail the build.

Go sends `Message` objects, not text — `{ key, args }`, rendered by `$t` at
display time. That is why switching language re-renders the log retroactively.
An argument whose value starts with `@:` is itself a catalogue key, which keeps
a nested reason translated inside a sentence Go assembled.

Voice-prompt language names (`English`, `中文`, `Français`, …) stay inline on
purpose: they are endonyms and read the same whatever the interface language
is.
