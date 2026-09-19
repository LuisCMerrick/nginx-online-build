# Nginx Online Web Builder

[![Go Report Card](https://goreportcard.com/badge/github.com/LuisCMerrick/nginx-online-build)](https://github.com/LuisCMerrick/nginx-online-build)
[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Docker](https://img.shields.io/badge/Docker-Ready-blue.svg)](Dockerfile)
[![Languages](https://img.shields.io/badge/i18n-English%20%7C%20%E7%AE%80%E4%BD%93%E4%B8%AD%E6%96%87-brightgreen.svg)](#internationalization-i18n)

[English](README.md) | [简体中文](README_zh.md)

A high-performance, secure, and cloud-native **Nginx Online Custom Compilation Platform** built with **Go**. It allows users to visually configure official Nginx modules and parameters via an intuitive Web UI, preview generated `./configure` scripts in real time, and compile in physically isolated sandbox workspaces with full verification (`nginx -V`) and distributable archive generation (`.tar.gz`).

---

## 🌟 Key Features & Architecture

1. **Version Management & Integrity Verification**
   - Automatically probes and provides official Nginx releases (Stable, Mainline, and Legacy branches). Defaults to official latest **Stable** (e.g. `1.30.5`).
   - Downloads official source tarballs directly from `nginx.org` with local caching and mandatory **SHA-256 cryptographic checksum verification** before extraction.

2. **Official Module Whitelist & Command Injection Defense**
   - Strictly curates and whitelists official parameters across categories: **SSL/TLS, HTTP, Stream (Layer 4), Mail, Performance, Debugging, Paths, and Event Models**.
   - **Zero arbitrary shell string execution**: Frontend submits structured option IDs; backend reconstructs sanitized, deterministic `./configure` argument slices.
   - Built-in bidirectional mutual exclusion detection and dependency auto-resolution (e.g. enabling `stream_ssl` automatically activates core `--with-stream`).

3. **Compile-Time Conflict Prompt & Auto-Optimization**
   - No rigid or breaking HTTP 400 rejection during option selection or preview.
   - Real-time soft alerts in the preview pane when incompatible options are detected (e.g. `--without-http_gzip_module` vs `--with-http_gzip_static_module`).
   - When the user clicks **Build**, an interactive confirmation dialog displays detected conflicts and offers one-click auto-optimization (retaining functional modules, eliminating contradictory exclusions, and auto-upgrading dependencies).

4. **Third-Party Static Source Embedding**
   - Supports static linking of third-party source packages directly into the Nginx binary:
     - **OpenSSL** (e.g. `3.4.1`, `3.0.16` LTS, `1.1.1w` Legacy, or custom tarball URLs via `--with-openssl`).
     - **PCRE / PCRE2** (e.g. `10.45`, `10.42`, or custom tarball URLs via `--with-pcre`).
     - **zlib** (e.g. `1.3.1`, or custom tarball URLs via `--with-zlib`).
   - Generates fully portable, standalone binaries free from host shared library dependencies (`libssl.so`, `libpcre2.so`, etc.).

5. **Physical Workspace Isolation**
   - Each build task is assigned a globally unique, lexicographically sortable **ULID** (e.g. `01M2Q0R3FGPPVJZHQQK5PPVJZH`):
     ```text
     data/builds/<build-id>/
     ├── source/     # Upstream source tarball & extraction cache
     ├── work/       # Isolated compilation worktree
     ├── logs/       # build.log, configure.log, error.log
     └── artifacts/  # Verified package nginx-<ver>-<os>-<arch>.tar.gz and metadata
     ```

6. **Real-time SSE Terminal Log Streaming**
   - Uses **Server-Sent Events (SSE)** to stream stdout and stderr from `./configure` and `make` live to the browser console.
   - Includes real-time progress bar, build duration counters, and automatic scrolling.

7. **Automated Compliance Verification (`nginx -V`)**
   - After compilation, the daemon automatically invokes `objs/nginx -V` inside the workspace.
   - Audits version string equality, executable permissions, and configure argument compliance before packaging.

8. **Internationalization (i18n)**
   - Built with a clean language pack architecture (`web/i18n.js`).
   - **English is the default repository and interface language**.
   - Auto-detects browser locale (`navigator.language`) and supports instant manual switching between English and Simplified Chinese (`zh-CN`), persisted in `localStorage`.

---

## 📡 RESTful API Specification

| Endpoint | Method | Description |
| :--- | :---: | :--- |
| `/api/nginx/versions` | `GET` | List available official Nginx versions (Stable, Mainline, Legacy) |
| `/api/nginx/options` | `GET` | Query official parameter whitelist, categorized taxonomy, and path defaults |
| `/api/nginx/preview` | `POST` | Generate formatted `./configure` command preview and mutual exclusion notices |
| `/api/builds` | `POST` | Create and enqueue a compilation job (supports `auto_resolve_conflicts`) |
| `/api/builds` | `GET` | Query recent build history and status records |
| `/api/builds/{id}` | `GET` | Query detailed job status, metrics, and `nginx -V` verification results |
| `/api/builds/{id}/logs` | `GET` | Stream live terminal output via SSE (`?stream=true`) or fetch raw logs |
| `/api/builds/{id}/artifact` | `GET` | Download compiled and verified distribution package (`.tar.gz`) |
| `/api/system/status` | `GET` | Check host Linux distro, package manager, and Nginx dependency status |
| `/api/system/deps/install` | `POST` | Trigger automatic dependency package installation in background |
| `/api/system/deps/logs` | `GET` | Stream live dependency installation output via SSE (`?stream=true`) |

---

## 🐳 Quick Start with Docker

### 1. Standalone Container

```bash
# Build Docker image
docker build -t nginx-builder:latest .

# Run container
docker run -d \
  --name nginx-builder \
  -p 8090:8090 \
  -v nginx-data:/app/data \
  nginx-builder:latest
```

Open your browser and navigate to: `http://localhost:8090`

### 2. Docker Compose

```bash
docker compose up -d
```

---

## 💻 CLI Usage & Configuration

`nginx-builder` provides a fully English CLI interface supporting custom host binding, port selection, workspace data directory, concurrency limits, and job timeouts:

```bash
Usage:
  nginx-builder [options]

Options:
  -h, --host <ip>        Server listen host/address (default: "0.0.0.0", env: HOST)
  -p, --port <port>      Server listen port (default: "8090", env: PORT)
  -b, --base-path <path> URL base path prefix for reverse proxy (default: "", env: BASE_PATH)
  -d, --data-dir <path>  Working data directory for builds and cache (default: "./data", env: DATA_DIR)
  -j, --jobs <n>         Max concurrent compilation jobs (default: 2, env: MAX_CONCURRENT_JOBS)
  -t, --timeout <min>    Job execution timeout in minutes (default: 20, env: JOB_TIMEOUT_MINUTES)
  -v, --version          Display version information and exit
      --help             Display this help message and exit
```

### Examples

```bash
# Listen on localhost port 9000 with custom data path
./bin/nginx-builder -host 127.0.0.1 -port 9000 -data-dir /var/lib/nginx-builder

# Configure 4 concurrent workers and 30-minute job timeout
./bin/nginx-builder -jobs 4 -timeout 30

# Mount on a non-root subpath prefix for reverse proxy (e.g. http://www.test.com/nginx)
./bin/nginx-builder -base-path /nginx -port 8090
```

### 🔀 Nginx Reverse Proxy Subpath Configuration Example

```nginx
location /nginx/ {
    proxy_pass http://127.0.0.1:8090/nginx/;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;

    # Support SSE real-time build & install streaming logs
    proxy_set_header Connection '';
    proxy_http_version 1.1;
    chunked_transfer_encoding off;
    proxy_buffering off;
    proxy_cache off;
}
```

---

## 💻 Local Development & Build

Requires **Go 1.22+** and standard compilation toolchain (`gcc`, `make`, `tar`, `curl`).

```bash
# Clone the repository
git clone https://github.com/LuisCMerrick/nginx-online-build.git
cd nginx-online-build

# Build server executable
go build -o bin/nginx-builder ./cmd/server

# Start server on port 8090
./bin/nginx-builder
```

---

## 🌍 Internationalization (i18n) & Pluggable Locales

The platform features a modular, pluggable language pack architecture located in the `web/locales/` directory:

```text
web/locales/
├── languages.json   # Manifest of registered languages for the UI switcher
├── en.json          # English language pack (Default)
└── zh.json          # Simplified Chinese language pack
```

- **Browser Locale Auto-Detection**: Reads client language (`navigator.language`) and automatically activates matching packs.
- **Manual Switcher**: Users can select their language from the header dropdown, persisted in `localStorage`.
- **Zero-Code Language Expansion**: To add a new language (e.g. Japanese `ja` or French `fr`):
  1. Create `web/locales/<lang>.json` copied from `en.json` and translate the values.
  2. Register the language entry in `web/locales/languages.json`:
     ```json
     { "code": "ja", "name": "日本語" }
     ```
  3. Rebuild binary or docker image. The language dropdown and loader will automatically recognize and load it on the fly!

---

## 📜 Verification & Audit Report Example

```text
nginx version: nginx/1.30.5
built by gcc 12.2.0 (Debian 12.2.0-14+deb12u1)
built with OpenSSL 3.4.1 11 Feb 2025
TLS SNI support enabled
configure arguments: --prefix=/usr/local/nginx --with-http_ssl_module --with-http_v2_module --with-http_v3_module --with-stream --with-threads --with-pcre-jit
```

---

## 📄 License

Distributed under the [MIT License](LICENSE).
