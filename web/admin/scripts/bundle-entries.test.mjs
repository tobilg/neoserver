import { test } from "node:test";
import assert from "node:assert/strict";
import { initialJavaScript } from "./bundle-entries.mjs";

test("counts eager shared dependencies once and excludes lazy routes", () => {
  assert.deepEqual(
    initialJavaScript({
      main: {
        isEntry: true,
        file: "assets/main.js",
        imports: ["shared", "other"],
        dynamicImports: ["lazy"],
      },
      shared: { file: "assets/shared.js" },
      other: { file: "assets/other.js", imports: ["shared"] },
      lazy: { file: "assets/lazy.js" },
    }),
    ["assets/main.js", "assets/shared.js", "assets/other.js"],
  );
});
test("does not exempt an eagerly imported map or style by filename", () => {
  assert.deepEqual(
    initialJavaScript({ main: { isEntry: true, file: "style-preview.js" } }),
    ["style-preview.js"],
  );
});
test("fails closed for a missing entry or dependency", () => {
  assert.throws(() => initialJavaScript({}), /no entry/);
  assert.throws(
    () =>
      initialJavaScript({
        main: { isEntry: true, file: "main.js", imports: ["missing"] },
      }),
    /Missing/,
  );
});
