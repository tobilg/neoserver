import { test } from "node:test";
import assert from "node:assert/strict";
import { createServer } from "node:net";
import { checkPort, doctor, supportedGo, supportedNode } from "./dev.mjs";

test("only supported Node LTS lines are accepted", () => {
  for (const version of ["22.22.0", "22.23.1", "24.0.0"])
    assert.equal(supportedNode(version), true);
  for (const version of ["20.20.0", "22.21.0", "23.1.0", "25.1.0"])
    assert.equal(supportedNode(version), false);
});
test("Go patch-level minimum is enforced", () => {
  for (const version of ["go version go1.26.8 darwin/arm64", "go1.27.0"])
    assert.equal(supportedGo(version), true);
  for (const version of ["go1.26.7", "go1.25.9", "", "devel"])
    assert.equal(supportedGo(version), false);
});
test("occupied ports fail without killing their owner", async () => {
  const server = createServer();
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  try {
    await assert.rejects(checkPort(server.address().port), /already in use/);
    assert.equal(server.listening, true);
  } finally {
    await new Promise((resolve) => server.close(resolve));
  }
});
test("available port is released after checking", async () => {
  await checkPort(0);
});

test("doctor explains all missing dependencies without stopping at the first", () => {
  const lines = [];
  const ok = doctor({
    log: (line) => lines.push(line),
    nodeVersion: "18.0.0",
    run: () => ({ status: null, stdout: undefined }),
  });
  assert.equal(ok, false);
  assert.equal(lines.filter((line) => line.startsWith("MISSING")).length, 7);
  assert.ok(lines.some((line) => line.includes("PKG_CONFIG_PATH")));
  assert.ok(lines.some((line) => line.includes("CGO_ENABLED=1")));
});

test("doctor accepts a complete supported toolchain", () => {
  assert.equal(
    doctor({
      log: () => {},
      nodeVersion: "24.0.0",
      run: (command, args) => ({
        status: 0,
        stdout:
          command === "go"
            ? args[0] === "version"
              ? "go version go1.26.8 linux/amd64"
              : "1\n"
            : "ok",
      }),
    }),
    true,
  );
});
