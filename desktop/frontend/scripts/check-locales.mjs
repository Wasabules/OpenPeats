/**
 * Checks the UI catalogues against the English source.
 *
 * English is the source language: every other catalogue must define exactly
 * the same keys with exactly the same {placeholders}. A missing key renders as
 * a raw identifier, an extra key is dead weight nobody will ever remove, and a
 * mismatched placeholder silently drops an interpolated value — none of which
 * a reviewer would catch by reading a diff of two JSON files.
 *
 * Also flags catalogue keys no component references, which is how a string
 * survives long after the code that used it is gone.
 *
 *   npm run check:locales
 */

import { readdirSync, readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const localesDir = resolve(here, "..", "src", "locales");
const srcDir = resolve(here, "..", "src");

const SOURCE = "en";
const PLACEHOLDER = /\{(\w+)\}/g;

const problems = [];

function fail(message) {
  problems.push(message);
}

function readCatalogue(locale) {
  const path = join(localesDir, `${locale}.json`);
  try {
    return JSON.parse(readFileSync(path, "utf8"));
  } catch (err) {
    fail(`${locale}.json: cannot be parsed — ${err.message}`);
    return null;
  }
}

// A message is a string, or plural forms keyed by CLDR category (see
// pluralForm in src/lib/i18n.js). "other" is the form every language has and
// the one a missing category falls back to, so it is mandatory.
const PLURAL_CATEGORIES = new Set(["zero", "one", "two", "few", "many", "other"]);

function forms(locale, key, message) {
  if (typeof message === "string") return [message];
  if (message && typeof message === "object" && !Array.isArray(message)) {
    for (const category of Object.keys(message)) {
      if (!PLURAL_CATEGORIES.has(category)) {
        fail(`${locale}.json: "${key}" has an unknown plural category "${category}"`);
      }
    }
    if (typeof message.other !== "string") {
      fail(`${locale}.json: "${key}" has plural forms but no "other"`);
    }
    return Object.values(message).map(String);
  }
  fail(`${locale}.json: "${key}" is neither a string nor plural forms`);
  return [];
}

/** Placeholders a message uses, across all its plural forms. */
function placeholders(locale, key, message) {
  const names = forms(locale, key, message).flatMap((form) =>
    [...form.matchAll(PLACEHOLDER)].map((m) => m[1])
  );
  return [...new Set(names)].sort();
}

/** Every .svelte and .js file under src/, so keys can be traced to their use. */
function sourceFiles(dir) {
  const out = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) {
      if (entry.name !== "locales") out.push(...sourceFiles(path));
    } else if (/\.(svelte|js)$/.test(entry.name)) {
      out.push(path);
    }
  }
  return out;
}

const locales = readdirSync(localesDir)
  .filter((f) => f.endsWith(".json"))
  .map((f) => f.slice(0, -".json".length))
  .sort();

if (!locales.includes(SOURCE)) {
  console.error(`No ${SOURCE}.json: the source catalogue is missing.`);
  process.exit(1);
}

const source = readCatalogue(SOURCE);
if (!source) process.exit(1);

const sourceKeys = Object.keys(source).sort();
if (sourceKeys.length === 0) fail(`${SOURCE}.json is empty`);

// Keys must stay sorted: it is the only thing keeping a catalogue reviewable
// once it passes a hundred entries.
for (const locale of locales) {
  const catalogue = readCatalogue(locale);
  if (!catalogue) continue;

  const keys = Object.keys(catalogue);
  const sorted = [...keys].sort();
  for (let i = 0; i < keys.length; i++) {
    if (keys[i] !== sorted[i]) {
      fail(`${locale}.json: keys are not sorted (first at "${keys[i]}")`);
      break;
    }
  }

  if (locale === SOURCE) {
    // The source is checked for shape too: a malformed plural in English
    // would otherwise pass, being the reference everything is compared to.
    for (const key of sourceKeys) forms(locale, key, catalogue[key]);
    continue;
  }

  for (const key of sourceKeys) {
    if (!(key in catalogue)) {
      fail(`${locale}.json: missing key "${key}"`);
      continue;
    }
    const want = placeholders(SOURCE, key, source[key]);
    const got = placeholders(locale, key, catalogue[key]);
    if (want.join(",") !== got.join(",")) {
      fail(
        `${locale}.json: "${key}" has placeholders [${got}], English has [${want}]`
      );
    }
  }
  for (const key of sorted) {
    if (!(key in source)) {
      fail(`${locale}.json: key "${key}" is absent from the English source`);
    }
  }
}

// Keys Go emits. Nothing under src/ names them, so without this a typo in a
// msg("…") call would ship as a raw identifier on screen. Go builds Messages
// with msg(), logs with a.log() and nests keys with ref(): those three call
// sites are the whole contract (see message.go).
const goDir = resolve(here, "..", "..");
const goSources = Object.fromEntries(
  readdirSync(goDir)
    .filter((f) => f.endsWith(".go") && !f.endsWith("_test.go"))
    .map((f) => [f, readFileSync(join(goDir, f), "utf8")])
);
const GO_KEY = /\b(?:msg|ref|a\.log)\("([\w.]+)"/g;
const goKeys = new Set();
for (const [file, text] of Object.entries(goSources)) {
  for (const [, key] of text.matchAll(GO_KEY)) {
    goKeys.add(key);
    if (!(key in source)) fail(`${file}: emits "${key}", absent from ${SOURCE}.json`);
  }
}

// Feature keys: Go reports support per entry of the table in features.go, and
// the frontend renders each as feature.<key>.
const FEATURE_KEY = /\bkey:\s*"(\w+)"/g;
const featureKeys = [...(goSources["features.go"] ?? "").matchAll(FEATURE_KEY)].map((m) => m[1]);
if (featureKeys.length === 0) fail("features.go: no feature table found — has it moved?");
for (const key of featureKeys) {
  if (!(`feature.${key}` in source)) {
    fail(`features.go: feature "${key}" has no "feature.${key}" entry in ${SOURCE}.json`);
  }
}

// Unused keys. Go-emitted ones count as used; so do battery.* and notify.*,
// which Go passes around as variables rather than literals.
const GO_EMITTED = /^(log\.|error\.|battery\.|notify\.)/;
const haystack = sourceFiles(srcDir)
  .map((f) => readFileSync(f, "utf8"))
  .join("\n");

for (const key of sourceKeys) {
  if (goKeys.has(key) || GO_EMITTED.test(key)) continue;
  if (key.startsWith("feature.")) {
    if (!featureKeys.includes(key.slice("feature.".length))) {
      fail(`${SOURCE}.json: "${key}" matches no entry of the feature table in features.go`);
    }
    continue;
  }
  if (!haystack.includes(`"${key}"`)) {
    fail(`${SOURCE}.json: key "${key}" is not referenced anywhere in src/`);
  }
}

if (problems.length > 0) {
  console.error(`Catalogue check failed (${problems.length} problem(s)):\n`);
  for (const p of problems) console.error(`  ${p}`);
  process.exit(1);
}

console.log(
  `Catalogues consistent: ${locales.length} locale(s), ${sourceKeys.length} keys.`
);
