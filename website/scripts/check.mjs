import { readFileSync, existsSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const websiteRoot = path.resolve(__dirname, "..");

const requiredFiles = [
  "favicon.svg",
  "index.html",
  "main.js",
  "style.css",
  "vite.config.js",
  "preview-metadata.json",
];

const requiredDOMIDs = [
  "site-home-link",
  "nav-github-link",
  "nav-releases-link",
  "nav-container-link",
  "nav-apt-link",
  "hero-release-link",
  "hero-checksum-link",
  "release-version",
  "release-date",
  "release-channels",
  "release-summary",
  "signal-archives",
  "signal-debs",
  "signal-checksums",
  "signal-apt",
  "install-script-command",
  "install-script-note",
  "apt-command",
  "apt-copy",
  "apt-fingerprint-row",
  "deb-command",
  "deb-note",
  "container-command",
  "container-note",
  "asset-count",
  "asset-release-link",
  "asset-checksum-link",
  "asset-key-link",
  "asset-key-asc-link",
  "asset-list",
  "footer-note",
];

const errors = [];

function assert(condition, message) {
  if (!condition) {
    errors.push(message);
  }
}

function load(relativePath) {
  const fullPath = path.join(websiteRoot, relativePath);
  assert(existsSync(fullPath), `Missing required file: ${relativePath}`);
  return existsSync(fullPath) ? readFileSync(fullPath, "utf8") : "";
}

function checkNodeSyntax(relativePath) {
  const result = spawnSync(process.execPath, ["--check", relativePath], {
    cwd: websiteRoot,
    encoding: "utf8",
  });

  if (result.status !== 0) {
    const output = [result.stdout, result.stderr].filter(Boolean).join("\n").trim();
    errors.push(`Syntax check failed for ${relativePath}${output ? `\n${output}` : ""}`);
  }
}

function findRootRelativeLocalAssets(html) {
  const assetRefs = [...html.matchAll(/\b(?:src|href)=["']([^"']+)["']/g)].map((match) => match[1]);
  return assetRefs.filter((ref) => ref.startsWith("/") && !ref.startsWith("//"));
}

const indexHTML = load("index.html");
const mainJS = load("main.js");
const viteConfig = load("vite.config.js");

for (const relativePath of requiredFiles) {
  assert(existsSync(path.join(websiteRoot, relativePath)), `Missing required file: ${relativePath}`);
}

checkNodeSyntax("main.js");
checkNodeSyntax("vite.config.js");

assert(
  indexHTML.includes('<script type="module" src="./main.js"></script>'),
  "index.html must load the Vite entrypoint via ./main.js",
);

assert(
  indexHTML.includes('<link rel="icon" href="./favicon.svg" type="image/svg+xml" />'),
  "index.html must declare the SVG favicon via ./favicon.svg",
);

assert(
  mainJS.includes('import "./style.css";') || mainJS.includes("import './style.css';"),
  "main.js must import ./style.css so Vite owns stylesheet output",
);

assert(
  viteConfig.includes("base: \"./\"") || viteConfig.includes("base: './'"),
  "vite.config.js must keep base set to ./ for GitHub Pages compatibility",
);

for (const domID of requiredDOMIDs) {
  assert(indexHTML.includes(`id="${domID}"`), `index.html is missing required id="${domID}"`);
}

const rootRelativeAssets = findRootRelativeLocalAssets(indexHTML);
assert(
  rootRelativeAssets.length === 0,
  `index.html contains root-relative local asset paths that break GitHub Pages subpath deploys: ${rootRelativeAssets.join(", ")}`,
);

assert(indexHTML.includes('href="#install"'), "index.html must retain the install anchor link");
assert(indexHTML.includes('id="install"'), "index.html must retain the install section target");

if (errors.length > 0) {
  console.error("Website validation failed:\n");
  for (const error of errors) {
    console.error(`- ${error}`);
  }
  process.exit(1);
}

console.log("Website validation passed.");
