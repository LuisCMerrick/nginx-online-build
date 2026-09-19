# Nginx Online Web Builder

[English](README.md) | [简体中文](README_zh.md)

A Go service for configuring and building Nginx through a web interface. Select a Nginx version, choose supported official modules, preview the configure arguments, follow build logs, and download a `.tar.gz` artifact.

The intended deployment is a build environment operated by trusted users. Builds run as child processes of the service account, with a separate directory for each job. The service does not create a container or virtual machine for each build.

## Features

- Retrieves release versions from `nginx.org`, caches the list for 30 minutes, and falls back to a bundled version list if retrieval fails. The default selection is the stable channel.
- Provides a catalog of official module options, installation paths, and scenario presets. The catalog is maintained in this repository; availability still depends on the chosen Nginx version and build environment.
- Previews dependency adjustments and option conflicts. Build requests with unresolved conflicts are rejected unless `auto_resolve_conflicts` is enabled.
- Supports building OpenSSL, PCRE/PCRE2, and zlib from preset or custom source archive URLs. These are dependency libraries, not arbitrary third-party Nginx module uploads.
- Queues builds, limits concurrent workers, streams logs over SSE, and exposes cancellation through the API.
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

Open `http://127.0.0.1:8090/` on the Docker host and enter the key printed at startup, or use the printed `?key=...` URL with the hostname corrected for your environment. `0.0.0.0` is a listen address, not a client destination. For remote use, put the service behind an HTTPS reverse proxy or use an SSH tunnel.

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

Use an **absolute data directory**, especially for source dependency builds. The current implementation passes dependency directory paths to Nginx while running `configure` from a different working directory; the default relative `./data` can therefore fail with source dependencies.

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
| `--version` | `-v` | — | Print service version and exit |
| `--help` | — | — | Print help and exit |

`AUTH` is a legacy fallback when `AUTH_ENABLED` is unset. `AUTH_ENABLED=false`, `0`, `no`, or `off` disables authentication. Nonpositive job counts/timeouts fall back to the built-in values.

`--jobs` limits simultaneous build jobs. Each job runs `make` with up to `min(runtime.NumCPU(), 8)` parallel processes. The timeout starts while the job is waiting for a worker, so queue time counts toward it. Download and extraction stages do not fully follow the job context; the configured timeout is not a strict end-to-end deadline.

### Authentication

All routes use the same shared key. The accepted credentials, in precedence order, are:

1. URL query: `?key=...`, `?token=...`, or `?auth=...`.
2. Cookie: `nginx_builder_key`.
3. Header: `X-Auth-Key: ...`, or `Authorization: Bearer ...`.

All key holders have the same access to build history, artifacts, cancellation, and dependency installation. The key is printed in startup logs; the browser also stores it in localStorage and sends it in API/SSE URLs. The current cookie lacks `HttpOnly` and `Secure`. Use HTTPS and protect or redact logs containing query strings. An API client should prefer a header. Rotating the configured key invalidates previous credentials; clear stale browser credentials when changing keys.

## Reverse proxy under a subpath

Start the backend with an absolute data path and matching URL prefix:

```bash
./bin/nginx-builder \
  --host 127.0.0.1 --port 8090 \
  --base-path /nginx --data-dir "$(pwd)/data"
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

`options` contains catalog **IDs**, not raw configure flags. Supported request fields also include `path_overrides` and `third_party_sources`; their definitions are in [model.go](internal/model/model.go). Omit `target_os` and `target_arch`: these fields label the artifact but do not configure cross-compilation and are currently insufficiently validated.

Preview success means arguments were generated; it does not prove that a source URL, dependency combination, or older Nginx release will build. Unknown option IDs and some invalid overrides are currently silently ignored. Inspect the generated arguments before building. Cancellation is currently an API feature; the web UI does not provide a build-cancel control or fully handle the `cancelled` terminal state.

## Source trust and build boundaries

- The downloader records SHA-256 for every source. It compares against an expected hash only when the selected entry has `ExpectedSHA`. Dynamically discovered Nginx releases and custom source URLs usually lack that value; calculating a hash alone does not authenticate the source. Signature verification is not implemented.
- Preset dependency versions and hashes are maintained in source code. A preset named “latest” or “recommended” is not automatically updated from upstream.
- Downloads validate destination addresses at DNS resolution, connection, and redirect stages to reject private/local targets. This does not restrict what a downloaded build script can do after execution.
- The extractor checks paths and limits each archive to 20,000 entries, 512 MiB per regular file, and 1 GiB of regular-file contents. Links and special files are skipped. These checks are not a per-job disk quota, and compressed downloads have no explicit size cap.
- `configure`, dependency build scripts, `make`, and binary checks run with the service account's permissions and inherited environment. A custom source archive must be trusted as executable code, even though the backend downloads it.
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

Successful builds remove `source/`, `deps/`, and `work/`. Failed or cancelled jobs can retain them. There is no scheduled retention job or cleanup API; the internal `PruneOldBuilds` helper is not wired into the running service. Plan disk monitoring and offline cleanup of old jobs/cache.

The archive contains `sbin/nginx`, `conf/`, `logs/`, available `html/` files, any packaged `modules/*.so`, and `BUILD_INFO.json`. It does not run `make install` or install a system service. Custom configure paths affect the binary's defaults, not the archive's directory layout. The bundled configuration comes from the source tree and may need changes for the modules selected.

Source-built OpenSSL/PCRE/zlib can remove dependencies on those particular shared libraries, but libc and other module libraries may still be dynamically linked. Use a compatible target system and examine `ldd` output; the archive filename is not proof of binary compatibility.

Before deployment, extract to a new directory, compare the archive hash with API metadata, inspect `nginx -V`, and run `nginx -t` against the actual target configuration. From the extracted directory, a basic check is:

```bash
./sbin/nginx -V
ldd ./sbin/nginx
./sbin/nginx -t -p "$PWD/" -c "$PWD/conf/nginx.conf" -e logs/error.log
```

The built-in smoke check uses a generated minimal configuration, not the shipped or production configuration. It currently can accept a nonzero `nginx -t` exit when output contains `syntax is ok`; therefore a `completed` status does not replace the target-side check. `BUILD_INFO.json` is created before this smoke check; consult the job API or `meta.json` for the final record.

## Development and localization

```bash
go test ./...
go test -race ./...
go vet ./...
go build -o bin/nginx-builder ./cmd/server
```

These commands cover the existing tests and service build, not every Nginx module/source/platform combination. Real Nginx build checks require the C toolchain and selected dependencies.

UI language packs are in `web/locales/`. To add a language, copy `en.json`, translate its values, register the code/name in `languages.json`, and rebuild the binary/image because assets are embedded. New packs are available in the manual switcher; automatic browser-language matching currently recognizes Chinese and otherwise uses English. Backend messages require separate localization work.

## License file

This repository snapshot does not contain a `LICENSE` file. The previous README linked to MIT terms at that missing path; the maintainer needs to supply the intended license text.
