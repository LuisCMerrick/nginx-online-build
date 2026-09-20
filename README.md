# Nginx Online Web Builder

[English](README.md) | [简体中文](README_zh.md)

A Go service for configuring and building Nginx through a web interface. Select a Nginx version, choose supported official modules, preview the configure arguments, follow build logs, and download a `.tar.gz` artifact.

The intended deployment is a build environment operated by trusted users. Builds run as child processes of the service account, with a separate directory for each job. The service does not create a container or virtual machine for each build.

## Features

- Retrieves release versions from `nginx.org`, caches the list for 30 minutes, and falls back to a bundled version list if retrieval fails. The default selection is the stable channel.
- Provides a catalog of official module options, installation paths, and scenario presets. The catalog is maintained in this repository; availability still depends on the chosen Nginx version and build environment.
- Previews dependency adjustments and option conflicts. Build requests with unresolved conflicts are rejected unless `auto_resolve_conflicts` is enabled.
- Supports building OpenSSL, PCRE/PCRE2, and zlib from preset or custom source archive URLs. These are dependency libraries, not arbitrary third-party Nginx module uploads.
- Bounds the build queue, limits concurrent workers, streams logs over SSE, and supports cancellation through the web UI or API.
- Records source and artifact SHA-256 values, checks `nginx -V`, records `ldd` output when available, and runs a configuration smoke check on the packaged binary.
- Includes a shared access key enabled by default, deployment under a URL prefix, and English/Simplified Chinese UI packs. Some backend messages and the authentication screen remain in Chinese.

See the [audit record, including known limitations](docs/AUDIT_2026-09-19.md) for the reviewed commit and validation evidence. It is a dated review, not a guarantee about later revisions.

## Requirements

| Component | Requirement |
| --- | --- |
| Build the service | Go **1.24 or later**, as declared in `go.mod`; embedded frontend assets require no Node.js build |
| Run Nginx builds | Linux, a C toolchain, `make`, development headers for selected modules, and writable data/temporary directories |
| Source dependencies | Perl for OpenSSL source builds; additional requirements depend on the chosen source release |
| Network | Access to `nginx.org` and selected source hosts, including release-download redirects |
| Artifact platform | The build environment's OS, CPU architecture, C runtime, and linked libraries |

The current Dockerfile uses `golang:1.25-bookworm` for the service build and `debian:bookworm-slim` for the Nginx build environment. It installs the C toolchain and core/optional development libraries. The Go service being statically linked does **not** make generated Nginx binaries fully static.

## Quick start

```bash
git clone https://github.com/LuisCMerrick/nginx-online-build.git
cd nginx-online-build
```

### Docker

```bash
mkdir -p data
docker build -t nginx-builder:latest .
docker run -d \
  --name nginx-builder \
  --restart unless-stopped \
  -p 127.0.0.1:8090:8090 \
  --mount type=bind,src="$(pwd)/data",dst=/app/data \
  nginx-builder:latest

docker logs nginx-builder
```

Open `http://127.0.0.1:8090/` on the Docker host and enter your configured `AUTH_KEY`, or the generated key in this process's startup log when no fixed key was configured. Fixed keys are not logged. `0.0.0.0` is a bind address, not a client address. Use an HTTPS reverse proxy or SSH tunnel for remote access.

The supplied Compose configuration can also be used:

```bash
docker compose up -d --build
docker compose logs nginx-builder
```

That file publishes port 8090 on all host interfaces and uses the named volume `builder-data`. Adjust its port binding and volume mapping for your deployment. Authentication is enabled by default in both examples. Set `AUTH_KEY` explicitly if it must survive restarts; an automatically generated key changes when the service restarts.

### Run directly on Linux

For a typical Debian/Ubuntu build host, install core dependencies as an administrator:

```bash
apt-get update
apt-get install -y --no-install-recommends \
  build-essential ca-certificates perl libpcre2-dev libssl-dev zlib1g-dev
```

Install additional development packages for optional modules, for example XSLT, image filtering, or GeoIP. Then build and run the service under an account that can write its data directory:

```bash
go build -o bin/nginx-builder ./cmd/server
mkdir -p data
./bin/nginx-builder --host 127.0.0.1 --data-dir "$(pwd)/data"
```

Relative and absolute data directories are supported. Startup resolves the root to an absolute path and exits if the directories cannot be created or written. Dependency source paths passed to configure are absolute as well.

The web dependency installer uses predefined package mappings and argument-based process execution. It installs packages into the environment running the service: the container when containerized, or the host for a direct deployment. It requires root or the passwordless sudo access detected by the application. Preinstalling dependencies allows ordinary builds to run without these privileges.

## Configuration

Precedence: command-line flags > environment variables > built-in defaults. `--no-auth` explicitly disables authentication even if `--auth` is also provided. Both single-dash and double-dash long flags are accepted.

| Flag | Short | Environment | Default |
| --- | --- | --- | --- |
| `--host` | `-h` | `HOST` | `0.0.0.0` |
| `--port` | `-p` | `PORT` | `8090` |
| `--base-path` | `-b` | `BASE_PATH` | Empty, root deployment |
| `--auth` | `-a` | `AUTH_ENABLED` | `true` |
| `--no-auth` | — | — | `false` |
| `--auth-key` | `-k` | `AUTH_KEY` | Generated at startup |
| `--data-dir` | `-d` | `DATA_DIR` | `./data` |
| `--jobs` | `-j` | `MAX_CONCURRENT_JOBS` | `2` |
| `--timeout` | `-t` | `JOB_TIMEOUT_MINUTES` | `20` minutes |
| `--queue-limit` | — | `MAX_QUEUED_JOBS` | `16` |
| `--retain-builds` | — | `MAX_RETAINED_BUILDS` | `20` terminal jobs |
| `--max-download-mb` | — | `MAX_DOWNLOAD_MB` | `256` MiB |
| `--max-cache-mb` | — | `MAX_CACHE_MB` | `1024` MiB |
| `--cookie-secure` | — | `COOKIE_SECURE` | `false`; enable behind HTTPS proxies |
| `--version` | `-v` | — | Print service version and exit |
| `--help` | — | — | Print help and exit |

`AUTH` is a legacy fallback when `AUTH_ENABLED` is unset. `AUTH_ENABLED=false`, `0`, `no`, or `off` disables authentication. Nonpositive job counts/timeouts fall back to the built-in values.

`--jobs` limits simultaneous build jobs. Each job runs `make` with up to `min(runtime.NumCPU(), 8)` parallel processes. The timeout starts at admission, including queue time, and propagates through downloads, extraction, configure, make, packaging, and verification. Cancellation terminates the build process group; all worker exits record a terminal status/end time and close their log stream.

### Authentication

All routes share one administrative key. Key holders have the same access to build history, artifacts, cancellation, and dependency installation.

- Browser login: `POST /api/auth/login` with `{"key":"…"}` exchanges the key for a random `nginx_builder_session` cookie with `HttpOnly`, `SameSite=Lax`, and a 12-hour lifetime. Sessions live in server memory; restarting requires a new login.
- HTTPS reverse proxies: set `COOKIE_SECURE=true` or `--cookie-secure` to add `Secure`. The backend does not trust arbitrary client-supplied `X-Forwarded-Proto` headers to choose cookie security.
- API clients: send `X-Auth-Key: ...` or `Authorization: Bearer ...`. Browser APIs, locales, downloads, and SSE use the same-origin session. The frontend no longer stores the key in localStorage or appends it to URLs.
- Logout: `POST /api/auth/logout` revokes the current session and clears its cookie. Session-authenticated mutations reject mismatched browser Origins.

**Upgrade:** the old `nginx_builder_key` cookie is no longer accepted; legacy localStorage credentials are removed. Sign in again. Entry-page `?key=...`, `?token=...`, and `?auth=...` logins remain supported and immediately redirect to a credential-free URL. API query-key authentication has been removed; migrate to headers. Entry URLs may still enter browser/proxy history, so prefer the login form and protect access logs. Fixed `AUTH_KEY` values are omitted from startup logs and build subprocess environments. Only a newly generated key is printed once at startup when no key is configured.

## Reverse proxy under a subpath

Start the backend with an absolute data path and matching URL prefix:

```bash
./bin/nginx-builder \
  --host 127.0.0.1 --port 8090 \
  --base-path /nginx --data-dir "$(pwd)/data" --cookie-secure
```

Add these locations to your HTTPS Nginx server block:

```nginx
location = /nginx {
    return 308 /nginx/$is_args$args;
}

location /nginx/ {
    # Keep the /nginx prefix when forwarding.
    proxy_pass http://127.0.0.1:8090;
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header Connection "";
    proxy_buffering off;
    proxy_cache off;
    proxy_read_timeout 300s;
}
```

Use `/nginx/` as the browser entry point. SSE streams send periodic heartbeats; proxy timeout values measure inactivity between upstream reads, not the total build duration. Configure access logging so credentials in query strings are not retained.

When `BASE_PATH` is set, the backend registers both root and prefixed API routes. The prefix is a routing convenience, not an access-control boundary. Artifact metadata currently returns a root-relative `download_url`; external clients using a subpath must prepend their public prefix. The web UI does this automatically.

## API

Add the configured public prefix to the paths below when needed. JSON endpoints generally return a `success` field; errors also include `error`. Build creation returns HTTP 202.

| Method | Path | Purpose |
| --- | --- | --- |
| POST | `/api/auth/login` | Exchange JSON `key` for a browser session |
| POST | `/api/auth/logout` | Revoke the current session |
| GET | `/api/nginx/versions` | Version list |
| GET | `/api/nginx/options` | Option catalog, categories, path defaults, presets, and dependency libraries |
| GET | `/api/nginx/presets` | Scenario presets |
| GET | `/api/nginx/deps` | Preset dependency library sources |
| POST | `/api/nginx/preview` | Configure argument preview, warnings, and conflicts |
| POST | `/api/builds` | Create a build; accepts `auto_resolve_conflicts` |
| GET | `/api/builds` | Most recent 50 jobs |
| GET | `/api/builds/{id}` | Status and metadata |
| POST | `/api/builds/{id}/cancel` | Request cancellation |
| GET | `/api/builds/{id}/logs` | Plain-text log; `?stream=true` or `Accept: text/event-stream` enables SSE |
| GET | `/api/builds/{id}/artifact` | Download a completed build's archive |
| GET | `/api/system/status` | Build-environment and dependency status |
| POST | `/api/system/deps/install` | Install missing predefined packages in the service environment |
| GET | `/api/system/deps/logs` | JSON log history; `?stream=true` enables SSE |

Example using the key from your running instance:

```bash
export NGINX_BUILDER_KEY='replace-with-your-instance-key'

curl --fail-with-body \
  -H "X-Auth-Key: $NGINX_BUILDER_KEY" \
  -H 'Content-Type: application/json' \
  --data '{"version":"stable","options":["http_ssl","http_v2","http_stub_status"]}' \
  http://127.0.0.1:8090/api/nginx/preview

curl --fail-with-body \
  -H "X-Auth-Key: $NGINX_BUILDER_KEY" \
  -H 'Content-Type: application/json' \
  --data '{"version":"stable","options":["http_ssl","http_v2","http_stub_status"],"auto_resolve_conflicts":false}' \
  http://127.0.0.1:8090/api/builds
```

`options` contains catalog **IDs**, not raw configure flags. Supported request fields also include `path_overrides` and `third_party_sources`; their definitions are in [model.go](internal/model/model.go).

A preview only produces arguments; it does not guarantee a source URL, dependency combination, or older Nginx version will compile. Unknown option IDs, JSON fields, path keys, and invalid values return 400. Path arguments have a stable order, and automatic resolution rechecks dependency closure and conflicts. Omit `target_os`/`target_arch` or match the build host exactly; cross-compilation is not supported.

Request bodies are limited to 64 KiB (413 on overflow). At most `--jobs + --queue-limit` unfinished jobs are admitted; overflow returns 429 with `Retry-After: 5`. Login JSON has a separate 4 KiB limit. The web UI includes a cancel button and treats `cancelled` as terminal, stopping polling/SSE and restoring controls.

## Source trust and build boundaries

- The downloader records SHA-256 for every source. It compares against an expected hash only when the selected entry has `ExpectedSHA`. Discovered releases matching bundled entries retain their expected hash; other releases and custom source URLs may lack one; calculating a hash alone does not authenticate the source. Signature verification is not implemented.
- Preset dependency versions and hashes are maintained in source code. A preset named “latest” or “recommended” is not automatically updated from upstream.
- Downloads validate destination addresses at DNS resolution, connection, and redirect stages to reject private/local targets. This does not restrict what a downloaded build script can do after execution.
- The extractor checks paths and limits each archive to 20,000 entries, 512 MiB per regular file, and 1 GiB of regular-file contents. Links and special files are skipped. Compressed downloads default to a 256 MiB limit, including responses without Content-Length. Cache hits also enforce size and available expected hashes. Cache publication/reuse evicts least-recently-used archives to the default 1024 MiB budget. Failed or cancelled downloads remove temporary files.
- `configure`, dependency build scripts, `make`, and binary checks run with the service account's permissions and inherited environment (excluding `AUTH_KEY`). A custom source archive must be trusted as executable code, even though the backend downloads it.
- The Docker image runs the service as root inside one container shared by all jobs. It separates the service environment from the host subject to container settings and mounts; it does not isolate jobs from each other. Avoid privileged mode and unrelated host mounts.

## Data and artifacts

| Path under `DATA_DIR` | Contents |
| --- | --- |
| `cache/` | Shared source archive cache |
| `builds/<id>/meta.json` | Persisted job metadata |
| `builds/<id>/source/` | Source archives copied for the job |
| `builds/<id>/deps/` | Extracted dependency sources |
| `builds/<id>/work/` | Nginx source and compilation files |
| `builds/<id>/logs/` | `build.log`, `configure.log`, `error.log` |
| `builds/<id>/artifacts/` | Generated `.tar.gz` |

Every worker exit, including failure and cancellation, cleans `source/`, `deps/`, and `work/`, retaining logs, metadata, and any existing artifact. At startup and after each worker finishes, the newest `--retain-builds` terminal jobs by creation time are retained (default 20); older job directories are deleted. Workers still shutting down are excluded. Back up any builds you need to keep before upgrading. On restart, every interrupted nonterminal job, including packaging, is marked failed and persisted.

Each build log is capped at about 16 MiB (build.log may include a short truncation notice); configure/error logs are also capped at 16 MiB. Binary-check output is capped at 1 MiB, and the UI terminal keeps its latest 1,048,576 characters. HTTP header/read/idle timeouts are 10/30/60 seconds, with no global write timeout so SSE remains available. These bounds are not hard CPU, memory, or disk quotas for compiler processes; apply deployment-level resource limits. Use one service process per DATA_DIR.

The archive contains `sbin/nginx`, `conf/`, `logs/`, available `html/` files, any packaged `modules/*.so`, and `BUILD_INFO.json`. It does not run `make install` or install a system service. Custom configure paths affect the binary's defaults, not the archive's directory layout. The bundled configuration comes from the source tree and may need changes for the modules selected.

Source-built OpenSSL/PCRE/zlib can remove dependencies on those particular shared libraries, but libc and other module libraries may still be dynamically linked. Use a compatible target system and examine `ldd` output; the archive filename is not proof of binary compatibility.

Before deployment, extract to a new directory, compare the archive hash with API metadata, inspect `nginx -V`, and run `nginx -t` against the actual target configuration. From the extracted directory, a basic check is:

```bash
./sbin/nginx -V
ldd ./sbin/nginx
./sbin/nginx -t -p "$PWD/" -c "$PWD/conf/nginx.conf" -e logs/error.log
```

The built-in smoke check generates a minimal configuration with PID, logs, and temporary paths in its test directory, using the current account and high ports. The version must match exactly and every nonzero `nginx -t` exit fails verification. It does not validate the shipped or production configuration, so `completed` still does not replace target-side checks. `BUILD_INFO.json` is created before the final smoke check and explicitly records packaging status; consult the job API or `meta.json` for the final result.

## Development and localization

```bash
go test ./...
go test -race ./...
go vet ./...
node --test web/app_test.js
go build -o bin/nginx-builder ./cmd/server
```

These commands cover the existing tests and service build, not every Nginx module/source/platform combination. Real Nginx build checks require the C toolchain and selected dependencies.

UI language packs are in `web/locales/`. To add a language, copy `en.json`, translate its values, register the code/name in `languages.json`, and rebuild the binary/image because assets are embedded. New packs are available in the manual switcher; automatic browser-language matching currently recognizes Chinese and otherwise uses English. Backend messages require separate localization work.

## License file

This repository snapshot does not contain a `LICENSE` file. The previous README linked to MIT terms at that missing path; the maintainer needs to supply the intended license text.
