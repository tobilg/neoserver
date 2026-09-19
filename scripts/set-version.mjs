#!/usr/bin/env node
// Keeps the version literals in code and configuration in step.
//
//   node scripts/set-version.mjs v0.1.0   # rewrite them
//   node scripts/set-version.mjs --check  # fail if they disagree
//
// A release build does not read these: `make release-build VERSION=...` and the
// container build inject the version through ldflags and build args. These are
// the fallback an unstamped development build reports, and they should still
// agree with each other.
//
// Documentation is deliberately not rewritten. Some of it quotes the version of
// a build that was actually tested, and substituting a newer string there would
// turn a record of evidence into a false claim. Prose mentions are reported
// instead, for a human to update where it makes sense.

import { readFileSync, writeFileSync } from "node:fs";

const VERSION_PATTERN = /^v?\d+\.\d+\.\d+(-[A-Za-z0-9.-]+)?$/;

// Each target names one literal, located by a regex whose first group is the
// version itself, so rewriting never has to reformat the surrounding file.
const targets = [
  {
    file: "internal/conf/appconfig.go",
    what: "unstamped build version",
    pattern: /(?<=var setVersion = ")[^"]+(?=")/g,
  },
  {
    file: "Dockerfile",
    what: "image build argument",
    pattern: /(?<=^ARG VERSION=).+$/gm,
  },
  {
    file: "web/admin/package.json",
    what: "console package version",
    pattern: /(?<="version": ")[^"]+(?=")/,
  },
  {
    file: "web/admin/package-lock.json",
    what: "console lockfile package version",
    pattern: /(?<="name": "neoserver-admin",\n\s+"version": ")[^"]+(?=")/g,
  },
  {
    file: ".github/workflows/ci.yml",
    what: "release-container smoke test",
    pattern: /(?<=test-release-container\.sh neoserver:candidate )\S+/,
  },
  {
    file: "scripts/test-release-container.sh",
    what: "smoke-test default version",
    pattern: /(?<=^version=\$\{2:-)[^}]+(?=\})/m,
  },
];

// Generated from the Go server, so it is verified rather than written here.
const generated = {
  file: "web/admin/openapi.json",
  read: (text) => JSON.parse(text).info?.version,
  regenerate: "make ui-openapi",
};

const read = (file) => readFileSync(file, "utf8");

function currentValues() {
  const found = new Map();
  for (const target of targets) {
    const matches = read(target.file).match(target.pattern) ?? [];
    found.set(target.file, [...new Set(matches)]);
  }
  return found;
}

function reportDocs(version) {
  const docs = [
    "docs/release-notes.md",
    "docs/deployment.md",
    "README.md",
    "CLAUDE.md",
  ];
  const mentions = [];
  for (const file of docs) {
    let text;
    try {
      text = read(file);
    } catch {
      continue;
    }
    text.split("\n").forEach((line, index) => {
      if (line.includes(version)) mentions.push(`${file}:${index + 1}  ${line.trim().slice(0, 96)}`);
    });
  }
  return mentions;
}

const argument = process.argv[2];

if (!argument || argument === "--help") {
  console.error("usage: set-version.mjs <version>|--check");
  process.exit(2);
}

if (argument === "--check") {
  const values = currentValues();
  const all = [...values.values()].flat();
  const distinct = [...new Set(all)];
  let failed = false;
  if (distinct.length !== 1) {
    failed = true;
    console.error("Version literals disagree:");
    for (const [file, found] of values) console.error(`  ${file}: ${found.join(", ") || "(no match)"}`);
  }
  const expected = distinct[0];
  const generatedValue = generated.read(read(generated.file));
  if (distinct.length === 1 && generatedValue !== expected) {
    failed = true;
    console.error(
      `${generated.file} reports ${generatedValue}, expected ${expected}. Run: ${generated.regenerate}`,
    );
  }
  if (failed) {
    console.error("\nRun: node scripts/set-version.mjs <version>");
    process.exit(1);
  }
  console.log(`Version literals agree on ${expected}.`);
  process.exit(0);
}

if (!VERSION_PATTERN.test(argument)) {
  console.error(`"${argument}" is not vMAJOR.MINOR.PATCH with an optional -suffix`);
  process.exit(2);
}

// The literals carry no leading "v"; the tag does.
const version = argument.replace(/^v/, "");
const previous = [...new Set([...currentValues().values()].flat())];

for (const target of targets) {
  const text = read(target.file);
  const updated = text.replace(target.pattern, version);
  if (updated === text) {
    console.log(`  unchanged  ${target.file} (${target.what})`);
    continue;
  }
  writeFileSync(target.file, updated);
  console.log(`  updated    ${target.file} (${target.what})`);
}

console.log(`\nSet to ${version}. Next:`);
console.log(`  ${generated.regenerate}   # ${generated.file} is generated from the server`);

for (const old of previous.filter((value) => value !== version)) {
  const mentions = reportDocs(old);
  if (!mentions.length) continue;
  console.log(`\nDocumentation still mentions ${old}. Review each; some are records of what`);
  console.log("was tested and must keep the old version:");
  for (const mention of mentions) console.log(`  ${mention}`);
}
