// Builds the web UI and the lucidd daemon, and places the daemon where Tauri
// expects an externalBin sidecar: binaries/lucidd-<target-triple>.exe.
import { execSync } from "node:child_process";
import { mkdirSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
const run = (cmd, cwd) => {
  console.log(`> ${cmd}  (${cwd})`);
  execSync(cmd, { cwd, stdio: "inherit" });
};

const host = execSync("rustc -vV", { encoding: "utf8" }).match(/^host: (.+)$/m);
if (!host) throw new Error("could not read the target triple from `rustc -vV`");
const triple = host[1].trim();

run("npm ci && npm run build", join(root, "web"));

const outDir = join(root, "desktop", "src-tauri", "binaries");
mkdirSync(outDir, { recursive: true });
const out = join(outDir, `lucidd-${triple}.exe`);
run(
  `go build -ldflags "-X github.com/ChinmayGit8765/lucidbench/internal/version.Version=0.1.0" -o "${out}" ./cmd/lucidd`,
  root,
);
console.log(`sidecar ready: ${out}`);
