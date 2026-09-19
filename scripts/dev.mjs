#!/usr/bin/env node
// Local-only contributor runner. No Docker volumes or existing data/ catalog
// are modified. This intentionally uses the documented local key abc123.
import { spawn, spawnSync } from "node:child_process";
import { existsSync, mkdirSync, rmdirSync } from "node:fs";
import { createServer } from "node:net";
import { fileURLToPath, pathToFileURL } from "node:url";
import path from "node:path";

export function supportedNode(version) {
  const [major, minor] = version.split(".").map(Number);
  return major === 22 ? minor >= 22 : major === 24;
}
export function supportedGo(version) {
  const match = /go(\d+)\.(\d+)\.(\d+)/.exec(version);
  return (
    !!match &&
    (+match[1] > 1 ||
      (+match[1] === 1 &&
        (+match[2] > 26 || (+match[2] === 26 && +match[3] >= 8))))
  );
}
const root = fileURLToPath(new URL("..", import.meta.url));
const ui = path.join(root, "web/admin");
const state = path.join(root, ".cache/dev");
const executable = path.join(state, "neoserver");
const store = path.join(state, "neoserver.db");
const linker = path.join(root, "scripts/native-linker.sh");
const readCommand = (command, args) =>
  spawnSync(command, args, { cwd: root, encoding: "utf8", timeout: 15000 });

export function doctor({
  log = console.log,
  run = readCommand,
  nodeVersion = process.versions.node,
} = {}) {
  const checks = [
    [
      "Node 24 or 22.22+",
      supportedNode(nodeVersion),
      "Run nvm use in web/admin (or install Node 24).",
    ],
    ["npm", run("npm", ["--version"]).status === 0, "Install npm with Node."],
    [
      "Go 1.26.8+",
      supportedGo(run("go", ["version"]).stdout ?? ""),
      "Install the Go version in go.mod.",
    ],
    [
      "C/C++ toolchain",
      run("cc", ["--version"]).status === 0 &&
        run("c++", ["--version"]).status === 0,
      "macOS: xcode-select --install; Linux: install build-essential.",
    ],
    [
      "GDAL development files",
      run("gdal-config", ["--cflags"]).status === 0,
      "macOS: brew install gdal; Debian/Ubuntu: install libgdal-dev and gdal-bin.",
    ],
    [
      "GDAL pkg-config metadata",
      run("pkg-config", [
        "--cflags",
        path.join(root, "internal/gdalmd/gdal-headers.pc"),
      ]).status === 0,
      "Install pkg-config/pkgconf and GDAL development files; make gdal.pc available via PKG_CONFIG_PATH.",
    ],
    [
      "CGO enabled",
      run("go", ["env", "CGO_ENABLED"]).stdout?.trim() === "1",
      "Set CGO_ENABLED=1; GDAL/DuckDB require CGO.",
    ],
  ];
  for (const [label, ok, help] of checks)
    log(`${ok ? "OK" : "MISSING"} ${label}${ok ? "" : ` — ${help}`}`);
  log(
    "Docker is optional for native development; required for live integration suites. See docs/development.md.",
  );
  return checks.every(([, ok]) => ok);
}

export function checkPort(port) {
  return new Promise((resolve, reject) => {
    const server = createServer();
    server.once("error", (error) =>
      reject(
        new Error(
          error.code === "EADDRINUSE"
            ? `Port ${port} is already in use. Stop its owner before make dev; no process was killed.`
            : `Cannot bind local port ${port}: ${error.message}`,
        ),
      ),
    );
    server.listen(port, "127.0.0.1", () => server.close(resolve));
  });
}

async function dev() {
  if (!doctor())
    throw new Error(
      "Resolve the missing dependencies above, then retry make dev.",
    );
  await Promise.all([checkPort(9000), checkPort(5173)]);
  mkdirSync(state, { recursive: true, mode: 0o700 });
  const lock = path.join(state, "running");
  try {
    mkdirSync(lock);
  } catch {
    throw new Error(
      `Another dev runner may own ${lock}. If none is running, remove this empty lock directory with rmdir and retry. The catalog is untouched.`,
    );
  }
  const children = new Set();
  let stopping = false;
  const env = {
    ...Object.fromEntries(
      Object.entries(process.env).filter(
        ([key]) => !key.startsWith("NEOSRV_") && key !== "DATABASE_URL",
      ),
    ),
    NEOSRV_STORE_KEY: "abc123",
    NEOSRV_STORE_PATH: store,
    NEOSRV_SERVER_HTTPHOST: "127.0.0.1",
    NEOSRV_SERVER_HTTPPORT: "9000",
    NEOSRV_SERVER_URLBASE: "http://localhost:9000",
    NEOSRV_SERVER_BASEPATH: "",
    NEOSRV_SERVER_ADMINUI: "true",
    NEOSRV_AUTH_REQUIREHTTPS: "false",
    NEOSRV_IMPORTER_ENABLED: "true",
    NEOSRV_IMPORTER_ROOT: path.join(state, "imports"),
    NEOSRV_IMPORTER_TEMPORARYDIRECTORY: path.join(state, "imports/tmp"),
    NEOSRV_WMS_ENABLED: "true",
    NEOSRV_WFS_ENABLED: "true",
    NEOSRV_WCS_ENABLED: "true",
  };
  function run(command, args, cwd = root, extra = {}) {
    if (stopping)
      return Promise.reject(new Error("Development session stopped."));
    const child = spawn(command, args, {
      cwd,
      stdio: "inherit",
      env: { ...env, ...extra },
      detached: process.platform !== "win32",
    });
    children.add(child);
    return new Promise((resolve, reject) => {
      child.once("error", (error) => {
        children.delete(child);
        reject(error);
      });
      child.once("exit", (code, signal) => {
        children.delete(child);
        if (code === 0 || stopping) resolve();
        else reject(new Error(`${command} exited (${code ?? signal}).`));
      });
    });
  }
  function stop() {
    stopping = true;
    for (const child of children) {
      try {
        if (process.platform === "win32") child.kill("SIGTERM");
        else process.kill(-child.pid, "SIGTERM");
      } catch {
        /* Already exited. */
      }
    }
  }
  process.once("SIGINT", stop);
  process.once("SIGTERM", stop);
  try {
    if (!existsSync(path.join(ui, "node_modules/.package-lock.json")))
      await run("npm", ["ci"], ui);
    console.log(
      "Building the backend. First build downloads Go modules and can take several minutes.",
    );
    const flags =
      process.platform === "darwin"
        ? ["-ldflags", `-extld=${JSON.stringify(linker)}`]
        : [];
    await run("go", ["build", ...flags, "-o", executable, "./cmd/neoserver"]);
    console.log(
      "Local-only catalog: .cache/dev/neoserver.db; encryption key: abc123. Never deploy this configuration.",
    );
    const configFlags = [
      "--config",
      path.join(root, "testing/tutorial/development.toml"),
    ];
    await run(executable, [
      ...(existsSync(store)
        ? ["create-token", "--role", "super_admin", "--expires", "24h"]
        : ["init"]),
      ...configFlags,
    ]);
    if (stopping) return;
    console.log(
      "Use the JWT printed above to sign in. It is not the encryption key. Ctrl-C stops both processes; data is preserved.",
    );
    const backend = run(executable, ["serve", ...configFlags]);
    // Observe startup failures immediately, even while readiness is being polled.
    let backendError;
    void backend.catch((error) => {
      backendError = error;
    });
    for (let attempt = 0; ; attempt++) {
      if (backendError) throw backendError;
      if (stopping) {
        await backend;
        return;
      }
      try {
        if (
          (
            await fetch("http://127.0.0.1:9000/ready", {
              signal: AbortSignal.timeout(1000),
            })
          ).ok
        )
          break;
      } catch {
        /* Still starting. */
      }
      if (attempt >= 120)
        throw new Error(
          "Backend did not become ready in two minutes. Inspect its logs above.",
        );
      await new Promise((resolve) => setTimeout(resolve, 1000));
    }
    console.log(
      "Open http://localhost:5173/admin/ — Vite hot reload; backend http://localhost:9000.",
    );
    const frontend = run(
      "npm",
      [
        "run",
        "dev",
        "--",
        "--host",
        "127.0.0.1",
        "--port",
        "5173",
        "--strictPort",
      ],
      ui,
      {
        NEOSRV_DEV_API_TARGET: "http://127.0.0.1:9000",
        NEOSRV_DEV_BASE_PATH: "",
      },
    );
    try {
      await Promise.race([backend, frontend]);
    } finally {
      stop();
      await Promise.allSettled([backend, frontend]);
    }
  } finally {
    stop();
    // Only the empty directory created by this invocation is removed.
    // Keep the lock if any child has not exited yet; never expose an open store.
    if (!children.size) rmdirSync(lock);
    process.removeListener("SIGINT", stop);
    process.removeListener("SIGTERM", stop);
  }
}

if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href
) {
  const command = process.argv[2];
  if (command === "doctor") process.exitCode = doctor() ? 0 : 1;
  else if (command === "dev")
    dev().catch((error) => {
      console.error(error.message);
      process.exitCode = 1;
    });
  else {
    console.error("Usage: node scripts/dev.mjs doctor|dev");
    process.exitCode = 2;
  }
}
