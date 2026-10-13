/**
 * Minimal i18n for the desktop UI.
 *
 * Hand-rolled rather than pulled from npm: the whole need is a reactive
 * catalogue lookup with named interpolation, which is the ~60 lines below.
 * A dependency would cost more in supply chain and lockfile churn than it
 * saves, and this app has to stay auditable — it talks to hardware over a
 * reverse-engineered protocol.
 *
 * English is the source language. Every other catalogue is a translation of
 * en.json and falls back to it key by key, so a partial translation degrades
 * to English instead of showing raw keys.
 *
 * The Go side never sends text: it sends {key, args} Message values (see
 * ../../message.go) that `t` renders here. An argument whose value starts with
 * "@:" is itself a catalogue key — the linked-message convention — which is
 * how a nested reason stays translated inside a sentence Go assembled.
 *
 * A message that depends on a number has one form per plural category, picked
 * by its `count` argument (see pluralForm). "(s)" is not a plural.
 */

import { derived, writable } from "svelte/store";

import en from "../locales/en.json";
import fr from "../locales/fr.json";

const CATALOGUES = { en, fr };

/** Source language, and the fallback for every key a translation misses. */
export const FALLBACK_LOCALE = "en";

/**
 * Locales offered in the picker, labelled in their own language: someone
 * looking for their language does not read the one currently displayed.
 */
export const LOCALES = [
  { code: "en", label: "English" },
  { code: "fr", label: "Français" },
];

const STORAGE_KEY = "openpeats.locale";

/** Maps a BCP 47 tag onto a catalogue we actually ship: "fr-FR" -> "fr". */
export function normalize(tag) {
  if (!tag) return FALLBACK_LOCALE;
  const lower = String(tag).toLowerCase();
  if (CATALOGUES[lower]) return lower;
  const primary = lower.split("-")[0];
  return CATALOGUES[primary] ? primary : FALLBACK_LOCALE;
}

/**
 * Picks the starting locale: an explicit past choice wins over the browser
 * languages, which win over English.
 */
function detect() {
  try {
    const saved = localStorage.getItem(STORAGE_KEY);
    if (saved && CATALOGUES[saved]) return saved;
  } catch {
    // Private mode or a sandboxed webview can throw on localStorage access.
    // A missing preference is not worth failing a startup over.
  }
  for (const tag of navigator.languages || [navigator.language]) {
    const code = normalize(tag);
    if (code !== FALLBACK_LOCALE) return code;
    if (String(tag).toLowerCase().startsWith(FALLBACK_LOCALE)) return FALLBACK_LOCALE;
  }
  return FALLBACK_LOCALE;
}

export const locale = writable(detect());

// Keep <html lang> in sync, starting from the detected value rather than only
// from a later user choice. Assistive technology and the browser's own text
// handling both read it.
locale.subscribe((code) => {
  if (typeof document !== "undefined") {
    document.documentElement.lang = code;
  }
});

/** Switches language and remembers the choice for the next launch. */
export function setLocale(tag) {
  const code = normalize(tag);
  locale.set(code);
  try {
    localStorage.setItem(STORAGE_KEY, code);
  } catch {
    // Same as above: not remembering the choice is a minor degradation.
  }
  return code;
}

function lookup(catalogue, key) {
  const hit = catalogue[key];
  return hit !== undefined ? hit : CATALOGUES[FALLBACK_LOCALE][key];
}

/**
 * Plural forms. A catalogue value may be an object keyed by CLDR plural
 * category — "one", "few", "many", "other", as Intl.PluralRules names them —
 * instead of a string. The `count` argument picks the form, with the rules of
 * the language being displayed: French puts 0 and 1 in "one", English only 1.
 * "other" is mandatory, and stands in for any category a translation leaves
 * out.
 *
 *   "log.eqApplied": { "one": "{count} band", "other": "{count} bands" }
 */
function pluralForm(template, args, code) {
  if (template === null || typeof template !== "object") return template;
  const n = Number(args?.count);
  const category = Number.isFinite(n) ? new Intl.PluralRules(code).select(n) : "other";
  return template[category] ?? template.other;
}

function interpolate(template, args, catalogue, code) {
  if (!args) return template;
  return template.replace(/\{(\w+)\}/g, (whole, name) => {
    const value = args[name];
    if (value === undefined || value === null) return whole;
    // Linked message: Go marks a nested catalogue key with "@:". Resolved
    // without arguments, so this cannot recurse indefinitely.
    if (typeof value === "string" && value.startsWith("@:")) {
      const nested = lookup(catalogue, value.slice(2));
      return nested !== undefined ? pluralForm(nested, undefined, code) : value.slice(2);
    }
    return String(value);
  });
}

/**
 * Reactive translator.
 *
 *   $t("card.anc.title")
 *   $t("status.channel", { channel: 11 })
 *   $t(messageFromGo)            // { key, args }
 *
 * A missing key renders as the key itself: that is a bug worth spotting, and
 * far better than a blank label.
 */
export const t = derived(locale, ($locale) => {
  const catalogue = CATALOGUES[$locale] || CATALOGUES[FALLBACK_LOCALE];

  const render = (template, args) =>
    interpolate(pluralForm(template, args, $locale), args, catalogue, $locale);

  return (key, args) => {
    // Message values coming from Go carry their own arguments.
    if (key && typeof key === "object") {
      if (!key.key) return ""; // zero Message: success, nothing to show
      return render(lookup(catalogue, key.key) ?? key.key, key.args);
    }
    if (!key) return "";
    const template = lookup(catalogue, key);
    return template === undefined ? key : render(template, args);
  };
});
