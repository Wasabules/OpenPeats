#!/usr/bin/env python3
"""Check the Android string resources against the English source.

values/strings.xml is the source language. Every values-<lang>/strings.xml must
translate exactly the same names, with exactly the same format specifiers.

Android silently falls back to the default resource for a missing name, which
is the right runtime behaviour but a terrible review signal: a translation can
rot for releases without anyone noticing. A mismatched specifier is worse — it
throws IllegalFormatException at runtime, in the translated build only.

Unlike the JSON catalogues, key order is not enforced here: Android resources
are conventionally grouped by section with comments, and that grouping is
worth more than alphabetical order.
"""

import re
import sys
import xml.etree.ElementTree as ET
from pathlib import Path

RES = Path(__file__).resolve().parents[2] / "android/app/src/main/res"
SOURCE = RES / "values/strings.xml"

# Names that legitimately have no translation. Keep this list short and
# justified — it is the escape hatch that quietly empties the check.
UNTRANSLATED = {
    "app_name",  # proper noun, identical in every language
}

# %1$s, %1$d, %.1f, %%, ... — Android format specifiers.
SPECIFIER = re.compile(r"%(?:\d+\$)?[-#+ 0,(]*\d*(?:\.\d+)?[a-zA-Z%]")


def load(path):
    """Map every string name to its text, for one resource file."""
    root = ET.parse(path).getroot()
    return {
        el.get("name"): "".join(el.itertext())
        for el in root.findall("string")
        if el.get("name")
    }


def specifiers(text):
    return sorted(SPECIFIER.findall(text))


def main():
    if not SOURCE.exists():
        print(f"missing source resources: {SOURCE}", file=sys.stderr)
        return 1

    source = load(SOURCE)
    if not source:
        print(f"{SOURCE} defines no strings", file=sys.stderr)
        return 1

    problems = []
    translations = sorted(RES.glob("values-*/strings.xml"))

    if not translations:
        print("no translated resources found — nothing to compare")
        return 0

    for path in translations:
        lang = path.parent.name
        translated = load(path)

        for name in sorted(source):
            if name in UNTRANSLATED:
                continue
            if name not in translated:
                problems.append(f"{lang}: missing string {name!r}")
                continue
            want = specifiers(source[name])
            got = specifiers(translated[name])
            if want != got:
                problems.append(
                    f"{lang}: {name!r} has specifiers {got}, English has {want}"
                )

        for name in sorted(translated):
            if name not in source:
                problems.append(
                    f"{lang}: string {name!r} is absent from the English source"
                )

    if problems:
        print(f"Android resource check failed ({len(problems)} problem(s)):\n",
              file=sys.stderr)
        for p in problems:
            print(f"  {p}", file=sys.stderr)
        return 1

    print(
        f"Android resources consistent: {len(translations)} translation(s), "
        f"{len(source)} strings."
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
