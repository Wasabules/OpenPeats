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

function placeholders(message) {
  return [...String(message).matchAll(PLACEHOLDER)].map((m) => m[1]).sort();
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

  if (locale === SOURCE) continue;

  for (const key of sourceKeys) {
    if (!(key in catalogue)) {
      fail(`${locale}.json: missing key "${key}"`);
      continue;
    }
    const want = placeholders(source[key]);
    const got = placeholders(catalogue[key]);
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

// Unused keys. Go emits some of them (log.*, error.*, battery.*, notify.*)
// and those never appear in a .svelte file, so they are exempt.
const GO_EMITTED = /^(log\.|error\.|battery\.|notify\.)/;
const haystack = sourceFiles(srcDir)
  .map((f) => readFileSync(f, "utf8"))
  .join("\n");

for (const key of sourceKeys) {
  if (GO_EMITTED.test(key)) continue;
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
