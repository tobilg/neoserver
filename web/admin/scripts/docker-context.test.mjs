import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";
import test from "node:test";

const root = new URL("../../../", import.meta.url);

test("UI image copies only declared inputs after its isolated dependency install", async () => {
  const dockerfile = await readFile(new URL("Dockerfile", root), "utf8");
  const uiStage = dockerfile.split("FROM golang:")[0];
  const inputs = [...uiStage.matchAll(/^COPY (.+)\s+\S+\s*$/gm)].flatMap(
    ([, sources]) => sources.split(/\s+/),
  );
  const included = (file) =>
    inputs.some(
      (source) =>
        path.posix.matchesGlob(file, source) ||
        file.startsWith(`${source.replace(/\/$/, "")}/`),
    );
  for (const file of [
    "web/admin/node_modules/esbuild/bin/esbuild",
    "web/admin/node_modules/.bin/vite",
    "web/admin/.env.production",
    "web/admin/dist/index.html",
  ]) {
    assert.equal(included(file), false, `dirty host input admitted: ${file}`);
  }
  for (const file of [
    "web/admin/src/main.tsx",
    "web/admin/public/favicon.svg",
    "web/admin/scripts/generate-form-schemas.mjs",
    "web/admin/.npmrc",
    "web/admin/openapi.json",
    "web/admin/package-lock.json",
    "web/admin/vite.config.ts",
  ]) {
    assert.equal(included(file), true, `build input missing: ${file}`);
  }
  assert.match(uiStage, /RUN npm ci --ignore-scripts/);
  assert.match(uiStage, /RUN npm run codegen && npm run build/);
});

test("Docker context excludes dependencies, local credentials, catalogs, and generated output", async () => {
  const patterns = (await readFile(new URL(".dockerignore", root), "utf8"))
    .split(/\r?\n/)
    .filter(Boolean);
  for (const pattern of [
    "**/node_modules",
    "**/.env",
    "**/.env.*",
    "data",
    "**/*.db",
    "**/*.duckdb",
    "internal/admin/dist",
    "neoserver",
  ]) {
    assert.ok(patterns.includes(pattern), `missing exclusion: ${pattern}`);
  }
});
