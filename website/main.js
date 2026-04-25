import "./style.css";

const defaultSiteURL = new URL("./", window.location.href).href;

const defaultMetadata = {
  site_url: defaultSiteURL,
  github_repository: "soakes/s3ctl",
  github_url: "https://github.com/soakes/s3ctl",
  release_url: "https://github.com/soakes/s3ctl/releases",
  container_url: "https://ghcr.io/soakes/s3ctl",
  install_script_url: `${defaultSiteURL}install.sh`,
  release_commit: "",
  latest_release: null,
  apt_repository: {
    available: false,
    url: `${defaultSiteURL}apt/`,
    suite: "stable",
    component: "main",
    key_url: `${defaultSiteURL}apt/s3ctl-archive-keyring.gpg`,
    fingerprint: "",
  },
};

function formatDate(value) {
  if (!value) {
    return "Not published yet";
  }

  const parsed = new Date(value);
  if (Number.isNaN(parsed.valueOf())) {
    return value;
  }

  return `${new Intl.DateTimeFormat("en-GB", {
    dateStyle: "long",
    timeStyle: "short",
    timeZone: "UTC",
  }).format(parsed)} UTC`;
}

function humanSize(bytes) {
  if (!Number.isFinite(bytes) || bytes <= 0) {
    return "Unknown size";
  }

  const units = ["B", "KB", "MB", "GB"];
  let value = bytes;
  let unitIndex = 0;

  while (value >= 1024 && unitIndex < units.length - 1) {
    value /= 1024;
    unitIndex += 1;
  }

  return `${value.toFixed(value >= 10 || unitIndex === 0 ? 0 : 1)} ${units[unitIndex]}`;
}

function classifyAsset(name) {
  if (name.endsWith(".deb")) {
    return "Debian package";
  }
  if (name.endsWith(".tar.gz")) {
    return "Release archive";
  }
  if (name.includes("SHA256SUMS")) {
    return "Checksums";
  }
  return "Asset";
}

function normalizeMetadata(metadata = {}) {
  const apt = metadata.apt_repository || {};

  return {
    ...defaultMetadata,
    ...metadata,
    site_url: `${metadata.site_url || defaultMetadata.site_url}`.replace(/\/?$/, "/"),
    install_script_url: metadata.install_script_url || defaultMetadata.install_script_url,
    release_commit: metadata.release_commit || "",
    latest_release: metadata.latest_release || null,
    apt_repository: {
      ...defaultMetadata.apt_repository,
      ...apt,
      url: `${apt.url || defaultMetadata.apt_repository.url}`.replace(/\/?$/, "/"),
      key_url: apt.key_url || defaultMetadata.apt_repository.key_url,
      fingerprint: apt.fingerprint || "",
      available: Boolean(apt.available),
    },
  };
}

function setText(id, value) {
  const element = document.getElementById(id);
  if (element) {
    element.textContent = value;
  }
}

function setHref(id, value) {
  const element = document.getElementById(id);
  if (element && value) {
    element.href = value;
  }
}

function normalizeWhitespace(value) {
  return value.replace(/\s+/g, " ").trim();
}

function stripMarkdown(value) {
  return normalizeWhitespace(
    value
      .replace(/\[([^\]]+)\]\([^)]+\)/g, "$1")
      .replace(/`([^`]+)`/g, "$1"),
  );
}

function shortCommit(value) {
  if (!value) {
    return "---";
  }

  return value.length > 10 ? value.slice(0, 7) : value;
}

function releaseCommit(metadata) {
  if (metadata.release_commit) {
    return metadata.release_commit;
  }

  const body = metadata.latest_release?.body || "";
  const match = body.match(/\/commit\/([0-9a-f]{7,40})/i);
  return match ? match[1] : "";
}

function setLinkState(id, { href, text, enabled }) {
  const element = document.getElementById(id);
  if (!element) {
    return;
  }

  if (text) {
    element.textContent = text;
  }

  element.href = href || "#";
  element.classList.toggle("is-disabled", !enabled);
  element.setAttribute("aria-disabled", enabled ? "false" : "true");
}

function chooseDirectDebAsset(assets = []) {
  return assets.find((asset) => asset.name.endsWith("_amd64.deb")) || assets.find((asset) => asset.name.endsWith(".deb")) || null;
}

function chooseChecksumAsset(assets = []) {
  return assets.find((asset) => asset.name.includes("SHA256SUMS")) || null;
}

function assetSummary(assets = []) {
  const archives = assets.filter((asset) => asset.name.endsWith(".tar.gz"));
  const debs = assets.filter((asset) => asset.name.endsWith(".deb"));
  const checksumAsset = chooseChecksumAsset(assets);

  return {
    archiveCount: archives.length,
    debCount: debs.length,
    checksumAsset,
  };
}

function changeHighlights(body) {
  if (!body) {
    return [];
  }

  const lines = body.split("\n");
  const highlights = [];
  let inIncludedChanges = false;

  for (const rawLine of lines) {
    const line = rawLine.trim();

    if (line === "## Included Changes") {
      inIncludedChanges = true;
      continue;
    }

    if (inIncludedChanges && line.startsWith("## ")) {
      break;
    }

    if (!inIncludedChanges || !line.startsWith("- ")) {
      continue;
    }

    const cleaned = stripMarkdown(line.slice(2)).replace(/\s+\([^)]*\)\s*$/, "");
    if (!cleaned || cleaned.startsWith("Automatically merged")) {
      continue;
    }

    highlights.push(cleaned);
    if (highlights.length === 3) {
      break;
    }
  }

  return highlights;
}

function releaseHighlights(metadata) {
  const release = metadata.latest_release;
  const assets = release?.assets || [];
  const summary = assetSummary(assets);
  const checksumAsset = chooseChecksumAsset(assets);
  const noteHighlights = changeHighlights(release?.body || "");
  const highlights = [];

  if (release?.tag_name) {
    highlights.push(
      `Release ${release.tag_name} ships ${summary.archiveCount} archives, ${summary.debCount} Debian packages, and ${checksumAsset ? "attached checksums" : "pending checksums"}.`,
    );
  }

  if (metadata.apt_repository.available && metadata.apt_repository.fingerprint) {
    highlights.push(`Signed APT metadata is live with archive fingerprint ${metadata.apt_repository.fingerprint}.`);
  } else if (metadata.apt_repository.available) {
    highlights.push("Signed APT metadata is published for stable Debian installs.");
  }

  if (noteHighlights.length > 0) {
    highlights.push(...noteHighlights);
  } else if (release?.tag_name) {
    highlights.push(`Latest release notes are published on the ${release.tag_name} GitHub release page.`);
  }

  return highlights.slice(0, 4);
}

function sortedAssets(assets = []) {
  const typeRank = {
    "Release archive": 0,
    "Debian package": 1,
    Checksums: 2,
    Asset: 3,
  };

  return [...assets].sort((left, right) => {
    const leftType = classifyAsset(left.name);
    const rightType = classifyAsset(right.name);
    const rankDelta = typeRank[leftType] - typeRank[rightType];

    if (rankDelta !== 0) {
      return rankDelta;
    }

    return left.name.localeCompare(right.name);
  });
}

function aptASCIIKeyURL(metadata) {
  return metadata.apt_repository.key_url.replace(/\.gpg$/, ".asc");
}

function releaseInstallCommand(metadata) {
  const release = metadata.latest_release;
  if (release?.tag_name) {
    return `curl -fsSL ${metadata.install_script_url} | bash -s -- --version ${release.tag_name}`;
  }

  return `curl -fsSL ${metadata.install_script_url} | bash`;
}

function renderAssetList(release) {
  const container = document.getElementById("asset-list");
  const count = document.getElementById("asset-count");

  if (!container || !count) {
    return;
  }

  const assets = sortedAssets(release?.assets || []);
  count.textContent = assets.length > 0 ? `${assets.length} published assets` : "No published assets yet";

  if (assets.length === 0) {
    container.innerHTML = '<div class="asset-empty">Publish a stable release to populate download links and package metadata.</div>';
    return;
  }

  container.replaceChildren(
    ...assets.map((asset) => {
      const row = document.createElement("div");
      row.className = "asset-row";

      const link = document.createElement("a");
      link.href = asset.browser_download_url;
      link.textContent = asset.name;

      const kind = document.createElement("span");
      kind.className = "asset-kind";
      kind.textContent = classifyAsset(asset.name);

      const size = document.createElement("span");
      size.className = "asset-size";
      size.textContent = humanSize(asset.size);

      row.append(link, kind, size);
      return row;
    }),
  );
}

function renderCommands(metadata) {
  const release = metadata.latest_release;
  const assets = release?.assets || [];
  const checksumAsset = chooseChecksumAsset(assets);
  const directDebAsset = chooseDirectDebAsset(assets);

  const installScriptCommand = document.getElementById("install-script-command");
  const installScriptNote = document.getElementById("install-script-note");
  const containerCommand = document.getElementById("container-command");
  const containerNote = document.getElementById("container-note");
  const aptCommand = document.getElementById("apt-command");
  const aptCopy = document.getElementById("apt-copy");
  const aptFingerprint = document.getElementById("apt-fingerprint-row");
  const debCommand = document.getElementById("deb-command");
  const debNote = document.getElementById("deb-note");

  if (installScriptCommand) {
    installScriptCommand.textContent = releaseInstallCommand(metadata);
  }

  if (installScriptNote) {
    installScriptNote.textContent = release?.tag_name
      ? `Pinned to ${release.tag_name}. macOS installs into a user-owned home bin dir and clears the quarantine marker.`
      : "The installer supports --version, --install-dir, and --binary-name; macOS uses a user-owned home bin dir.";
  }

  if (containerCommand) {
    containerCommand.textContent = release?.tag_name
      ? `docker run --rm ghcr.io/soakes/s3ctl:${release.tag_name} --help`
      : "docker run --rm ghcr.io/soakes/s3ctl:latest --help";
  }

  if (containerNote) {
    containerNote.textContent = release?.tag_name
      ? `Use ${release.tag_name} for reproducible automation or switch to :latest for the moving stable channel.`
      : "Use the published image when you want the install path to stay container-native.";
  }

  if (aptCommand) {
    if (metadata.apt_repository.available) {
      aptCommand.textContent = `sudo install -d -m 0755 /etc/apt/keyrings
curl -fsSL ${metadata.apt_repository.key_url} \\
  | sudo tee /etc/apt/keyrings/s3ctl-archive-keyring.gpg >/dev/null

sudo tee /etc/apt/sources.list.d/s3ctl.sources >/dev/null <<'EOF'
Types: deb
URIs: ${metadata.apt_repository.url}
Suites: ${metadata.apt_repository.suite}
Components: ${metadata.apt_repository.component}
Signed-By: /etc/apt/keyrings/s3ctl-archive-keyring.gpg
EOF

sudo apt update && sudo apt install s3ctl`;
    } else {
      aptCommand.textContent = "APT repository metadata has not been published yet. Use the installer script or a direct .deb package in the meantime.";
    }
  }

  if (aptCopy) {
    aptCopy.textContent = metadata.apt_repository.available
      ? `This repository is published with signed metadata. Binary and ASCII export forms of the archive key are available from the release hub.`
      : "The landing page is ready for a signed APT repository; until it is published, the direct installer and .deb assets remain the cleanest path.";
  }

  if (aptFingerprint) {
    aptFingerprint.textContent = metadata.apt_repository.fingerprint
      ? `Archive fingerprint: ${metadata.apt_repository.fingerprint}`
      : "";
  }

  if (debCommand) {
    if (directDebAsset) {
      debCommand.textContent = `curl -fsSLO ${directDebAsset.browser_download_url}
sudo apt install ./${directDebAsset.name}`;
    } else {
      debCommand.textContent = "Linux release packages will appear here after the next published stable release.";
    }
  }

  if (debNote) {
    debNote.textContent = checksumAsset
      ? `Checksums are published as ${checksumAsset.name} so direct package installs can be verified before execution.`
      : "The release hub will prefer the amd64 package when one is published.";
  }
}

function renderLinks(metadata) {
  const release = metadata.latest_release;
  const assets = release?.assets || [];
  const checksumAsset = chooseChecksumAsset(assets);
  const releasePageURL = release?.html_url || metadata.release_url;

  setHref("site-home-link", metadata.site_url);
  setHref("nav-github-link", metadata.github_url);
  setHref("nav-releases-link", metadata.release_url);
  setHref("nav-apt-link", metadata.apt_repository.url);
  setHref("hero-release-link", releasePageURL);
  setHref("nav-container-link", metadata.container_url);
  setLinkState("hero-checksum-link", {
    href: checksumAsset?.browser_download_url || releasePageURL,
    text: checksumAsset ? "Checksums" : "Release Page",
    enabled: true,
  });
  setLinkState("asset-release-link", {
    href: releasePageURL,
    text: "Release page",
    enabled: true,
  });
  setLinkState("asset-checksum-link", {
    href: checksumAsset?.browser_download_url || releasePageURL,
    text: checksumAsset ? "Checksums" : "Checksums pending",
    enabled: Boolean(checksumAsset),
  });
  setLinkState("asset-key-link", {
    href: metadata.apt_repository.key_url,
    text: metadata.apt_repository.available ? "APT keyring" : "APT keyring pending",
    enabled: metadata.apt_repository.available,
  });
  setLinkState("asset-key-asc-link", {
    href: aptASCIIKeyURL(metadata),
    text: metadata.apt_repository.available ? "ASCII key" : "ASCII key pending",
    enabled: metadata.apt_repository.available,
  });
}

function renderMetadata(rawMetadata) {
  const metadata = normalizeMetadata(rawMetadata);
  const release = metadata.latest_release;
  const commit = shortCommit(releaseCommit(metadata));
  const highlights = releaseHighlights(metadata);

  renderLinks(metadata);

  setText("release-version", release?.tag_name || "Awaiting release metadata");
  setText("release-commit", commit);
  setText("release-date", formatDate(release?.published_at));
  setText("release-fingerprint", metadata.apt_repository.fingerprint || "Awaiting signed APT metadata");
  setText("footer-version", release?.tag_name || "---");
  setText("footer-commit", commit);

  const highlightsList = document.getElementById("release-highlights");
  if (highlightsList) {
    highlightsList.replaceChildren(
      ...(highlights.length > 0 ? highlights : ["Release metadata has not been published yet."]).map((highlight) => {
        const item = document.createElement("li");
        item.textContent = highlight;
        return item;
      }),
    );
  }

  renderCommands(metadata);
  renderAssetList(release);
}

async function copyText(value) {
  if (navigator.clipboard?.writeText) {
    await navigator.clipboard.writeText(value);
    return;
  }

  const textarea = document.createElement("textarea");
  textarea.value = value;
  textarea.setAttribute("readonly", "");
  textarea.style.position = "absolute";
  textarea.style.left = "-9999px";
  document.body.append(textarea);
  textarea.select();
  document.execCommand("copy");
  textarea.remove();
}

function wireCopyButtons() {
  document.querySelectorAll("[data-copy-target]").forEach((button) => {
    if (button.dataset.bound === "true") {
      return;
    }

    button.dataset.bound = "true";
    button.addEventListener("click", async () => {
      const targetID = button.dataset.copyTarget;
      const target = document.getElementById(targetID);
      if (!target) {
        return;
      }

      const originalLabel = button.textContent;

      try {
        await copyText(target.textContent || "");
        button.textContent = "Copied";
      } catch (error) {
        button.textContent = "Failed";
      }

      window.setTimeout(() => {
        button.textContent = originalLabel;
      }, 1400);
    });
  });
}

async function loadMetadata() {
  renderMetadata(defaultMetadata);

  for (const path of ["./website-metadata.json", "./preview-metadata.json"]) {
    try {
      const response = await fetch(path, { cache: "no-store" });
      if (!response.ok) {
        continue;
      }

      const metadata = await response.json();
      renderMetadata(metadata);
      return;
    } catch (error) {
      // Try the next metadata source.
    }
  }
}

document.addEventListener("DOMContentLoaded", () => {
  wireCopyButtons();
  void loadMetadata();

  const observer = new IntersectionObserver(
    (entries, instance) => {
      for (const entry of entries) {
        if (!entry.isIntersecting) {
          continue;
        }

        entry.target.classList.add("fade-in");
        instance.unobserve(entry.target);
      }
    },
    {
      root: null,
      rootMargin: "0px",
      threshold: 0.1,
    },
  );

  document.querySelectorAll(".glass-card").forEach((card) => {
    if (card.closest(".stagger-in") || card.closest(".fade-in")) {
      return;
    }

    card.style.opacity = "0";
    observer.observe(card);
  });
});
