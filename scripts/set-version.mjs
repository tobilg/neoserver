#!/usr/bin/env node
// Keeps current-release version literals in code, configuration and docs in step.
//
//   node scripts/set-version.mjs v0.1.0   # rewrite them
//   node scripts/set-version.mjs --check  # fail if they disagree
//
// A release build does not read these: `make release-build VERSION=...` and the
// container build inject the version through ldflags and build args. These are
// the fallback an unstamped development build reports, and they should still
// agree with each other.
//
// Documentation targets describe the current release. Published release notes,
// measured baselines, dependency versions and upgrade source versions retain
// their original values.

import { readFileSync, writeFileSync } from "node:fs";

const VERSION_PATTERN = /^v?\d+\.\d+\.\d+(-[A-Za-z0-9.-]+)?$/;

// Patterns match only the version, preserving surrounding formatting. Each
// target is read independently, so rerunning also repairs stale documentation.
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
  {
    file: "README.md",
    what: "current version",
    pattern: /(?<=^Current version: \*\*)[^*\s]+(?=\*\*\.)/m,
  },
  {
    file: "README.md",
    what: "container example",
    pattern: /(?<=tobilg\/neoserver:)\d+\.\d+\.\d+(?:-[A-Za-z0-9.-]+)?/g,
  },
  {
    file: "docs/deployment.md",
    what: "container examples",
    pattern: /(?<=tobilg\/neoserver:)\d+\.\d+\.\d+(?:-[A-Za-z0-9.-]+)?/g,
  },
  {
    file: "docs/deployment.md",
    what: "current upgrade target",
    pattern: /(?<=^In neoserver )\d+\.\d+\.\d+(?:-[A-Za-z0-9.-]+)?/m,
  },
  {
    file: "docs/releasing.md",
    what: "release command examples",
    pattern:
      /(?<=make (?:release-build|set-version) VERSION=v)\d+\.\d+\.\d+(?:-[A-Za-z0-9.-]+)?/g,
  },
  {
    file: "docs/release-notes.md",
    what: "current release notes",
    pattern: /(?<=^## )\d+\.\d+\.\d+(?:-[A-Za-z0-9.-]+)?(?= — )/m,
    rewrite(text, version) {
      const heading = text.match(
        /^## (\d+\.\d+\.\d+(?:-[A-Za-z0-9.-]+)?) — (.+)$/m,
      );
      if (heading[1] === version) return text;
      if (heading[2] === "unreleased")
        return text.replace(this.pattern, version);
      // Begin the next release without relabelling a published release's notes.
      return (
        text.slice(0, heading.index) +
        `## ${version} — unreleased\n\n` +
        text.slice(heading.index)
      );
    },
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
    if (!matches.length) {
      console.error(
        `${target.file}: cannot find ${target.what}; update its version target pattern`,
      );
      process.exit(1);
    }
    found.set(`${target.file} (${target.what})`, [...new Set(matches)]);
  }
  return found;
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
    for (const [file, found] of values)
      console.error(`  ${file}: ${found.join(", ") || "(no match)"}`);
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
  console.error(
    `"${argument}" is not vMAJOR.MINOR.PATCH with an optional -suffix`,
  );
  process.exit(2);
}

// The literals carry no leading "v"; the tag does.
const version = argument.replace(/^v/, "");
// Validate every target before writing any file.
currentValues();

for (const target of targets) {
  const text = read(target.file);
  const updated = target.rewrite
    ? target.rewrite(text, version)
    : text.replace(target.pattern, version);
  if (updated === text) {
    console.log(`  unchanged  ${target.file} (${target.what})`);
    continue;
  }
  writeFileSync(target.file, updated);
  console.log(`  updated    ${target.file} (${target.what})`);
}

console.log(`\nSet to ${version}. Next:`);
console.log(
  `  ${generated.regenerate}   # ${generated.file} is generated from the server`,
);
