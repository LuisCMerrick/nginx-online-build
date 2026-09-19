# Nginx 在线编译构建平台

[English](README.md) | [简体中文](README_zh.md)

基于 Go 的 Nginx Web 编译服务。在页面选择 Nginx 版本、官方模块和编译参数，预览 configure 命令、查看构建日志，并下载 `.tar.gz` 产物。

适用于由可信用户操作的编译环境。每个任务使用独立目录，编译进程以服务账户身份运行；程序不会为每个任务创建容器或虚拟机。

## 功能

- 从 `nginx.org` 获取版本列表，缓存 30 分钟；获取失败时回退到内置列表，默认选择稳定分支。
- 提供官方模块参数目录、安装路径配置和场景预设。参数目录由本仓库维护，具体能否使用仍取决于 Nginx 版本及编译环境。
- 预览依赖补充和选项冲突。创建任务时若存在未解决冲突，需调整选项或启用 `auto_resolve_conflicts`。
- 支持从预设或自定义源码压缩包 URL 构建 OpenSSL、PCRE/PCRE2、zlib。这些是依赖库源码，不是任意第三方 Nginx 模块上传功能。
- 支持任务排队、并发限制、SSE 实时日志，以及通过 API 请求取消任务。
- 记录源码和产物 SHA-256，检查 `nginx -V`，在可用时记录 `ldd` 输出，并对打包后的二进制执行配置冒烟检查。
- 默认启用共享访问密钥，支持 URL 子路径部署和中英文界面。部分后端消息及鉴权页面仍为中文。

本次检查的提交、验证证据及已知限制见[审计记录](docs/AUDIT_2026-09-19.md)。该记录针对注明的版本，不代表后续修订的审计结论。

## 环境要求

| 项目 | 要求 |
| --- | --- |
| 编译服务端 | Go **1.24 或更高版本**，以 `go.mod` 为准；前端嵌入二进制，无需 Node.js 构建 |
| 执行 Nginx 编译 | Linux、C 编译工具链、`make`、所选模块的开发头文件，以及可写的数据目录和临时目录 |
| 源码依赖 | OpenSSL 源码编译需要 Perl；其他要求取决于具体源码版本 |
| 网络 | 可访问 `nginx.org` 及选定的源码站点，包括下载重定向目标 |
| 产物运行平台 | 受编译环境的操作系统、CPU 架构、C 运行库和链接库约束 |

当前 Dockerfile 的服务端构建阶段使用 `golang:1.25-bookworm`，Nginx 编译环境使用 `debian:bookworm-slim`，预装 C 工具链及核心、可选模块开发库。Go 服务端采用静态链接，**不代表生成的 Nginx 也是完全静态二进制**。

## 快速启动

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

在 Docker 宿主机打开 `http://127.0.0.1:8090/`，输入启动日志中的密钥；也可使用日志打印的 `?key=...` 链接，并按实际环境替换主机地址。`0.0.0.0` 是监听地址，不是客户端访问地址。远程访问可通过 HTTPS 反向代理或 SSH 隧道。

也可以使用仓库提供的 Compose 配置：

```bash
docker compose up -d --build
docker compose logs nginx-builder
```

现有 Compose 文件将 8090 端口发布到宿主机所有接口，并使用命名卷 `builder-data`；可按部署需要调整端口绑定和目录映射。以上方式均默认开启鉴权。若希望重启后密钥保持不变，请显式设置 `AUTH_KEY`；自动生成的密钥会随服务重启变化。

### Linux 直接运行

Debian/Ubuntu 上可先以管理员身份安装核心依赖：

```bash
apt-get update
apt-get install -y --no-install-recommends \
  build-essential ca-certificates perl libpcre2-dev libssl-dev zlib1g-dev
```

选用 XSLT、图片处理、GeoIP 等可选模块时，需另外安装对应开发包。随后以具有数据目录写权限的账户编译并启动服务：

```bash
go build -o bin/nginx-builder ./cmd/server
mkdir -p data
./bin/nginx-builder --host 127.0.0.1 --data-dir "$(pwd)/data"
```

建议使用**绝对数据目录**，尤其是启用依赖库源码编译时。目前程序切换到 Nginx 源码目录后，仍将依赖目录路径直接传给 configure；默认相对路径 `./data` 在此场景下可能导致构建失败。

网页依赖安装功能使用预定义包名映射，通过参数数组调用包管理器。安装目标是服务所在环境：Docker 部署时为容器，直接部署时为宿主机。自动安装需要 root 或程序检测到的免密 sudo 权限；提前安装依赖后，普通编译任务无需这些权限。

## 配置

优先级：命令行参数 > 环境变量 > 内置默认值。`--no-auth` 即使与 `--auth` 同时指定，也会关闭鉴权。长参数支持单横线和双横线形式。

| 参数 | 简写 | 环境变量 | 默认值 |
| --- | --- | --- | --- |
| `--host` | `-h` | `HOST` | `0.0.0.0` |
| `--port` | `-p` | `PORT` | `8090` |
| `--base-path` | `-b` | `BASE_PATH` | 空，即根路径 |
| `--auth` | `-a` | `AUTH_ENABLED` | `true` |
| `--no-auth` | — | — | `false` |
| `--auth-key` | `-k` | `AUTH_KEY` | 启动时自动生成 |
| `--data-dir` | `-d` | `DATA_DIR` | `./data` |
| `--jobs` | `-j` | `MAX_CONCURRENT_JOBS` | `2` |
| `--timeout` | `-t` | `JOB_TIMEOUT_MINUTES` | `20` 分钟 |
| `--version` | `-v` | — | 输出服务端版本后退出 |
| `--help` | — | — | 输出帮助后退出 |

未设置 `AUTH_ENABLED` 时兼容旧环境变量 `AUTH`。`AUTH_ENABLED` 为 `false`、`0`、`no`、`off` 时关闭鉴权。并发数、超时时间为非正数时回退到内置默认值。

`--jobs` 控制同时执行的任务数，每个任务的 make 并行度最多为 `min(runtime.NumCPU(), 8)`。任务超时从等待 Worker 时开始计时，排队时间包含在内；下载和解压尚未完整接入任务 context，因此该值不是严格的端到端截止时间。

### 鉴权

所有路由共用一个密钥。凭据按以下顺序读取：

1. URL 参数：`?key=...`、`?token=...`、`?auth=...`。
2. Cookie：`nginx_builder_key`。
3. 请求头：`X-Auth-Key: ...` 或 `Authorization: Bearer ...`。

持有密钥的用户对构建历史、产物、取消任务及依赖安装拥有相同访问能力。密钥会出现在启动日志中；前端也会将其保存在 localStorage，并附加到 API/SSE URL。当前 Cookie 未设置 `HttpOnly` 和 `Secure`。部署时使用 HTTPS，保护或脱敏包含查询参数的日志；API 客户端优先使用请求头。修改配置密钥可使旧凭据失效，更换密钥后需清理浏览器中的旧凭据。

## 子路径反向代理

后端使用绝对数据目录，并配置匹配的 URL 前缀：

```bash
./bin/nginx-builder \
  --host 127.0.0.1 --port 8090 \
  --base-path /nginx --data-dir "$(pwd)/data"
```

在提供 HTTPS 的 Nginx server 中添加：

```nginx
location = /nginx {
    return 308 /nginx/$is_args$args;
}

location /nginx/ {
    # 转发时保留 /nginx 前缀。
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

浏览器入口使用 `/nginx/`。SSE 定期发送心跳；代理读取超时针对相邻两次上游读取的间隔，不是整个编译任务的总时长。访问日志需避免记录查询参数中的访问密钥。

配置 `BASE_PATH` 后，后端仍同时注册根路径和带前缀的 API，因此子路径不是权限边界。产物元数据中的 `download_url` 当前为根相对路径；通过子路径调用的外部客户端需自行添加公开前缀，Web 界面已自动处理。

## API

子路径部署时按需在以下路径前添加公开前缀。JSON 接口通常返回 `success`，失败时同时返回 `error`；创建构建任务返回 HTTP 202。

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| GET | `/api/nginx/versions` | 获取版本列表 |
| GET | `/api/nginx/options` | 参数目录、分类、路径默认值、预设和依赖库列表 |
| GET | `/api/nginx/presets` | 场景预设 |
| GET | `/api/nginx/deps` | 依赖库预设源码 |
| POST | `/api/nginx/preview` | 预览 configure 参数、警告和冲突 |
| POST | `/api/builds` | 创建任务，支持 `auto_resolve_conflicts` |
| GET | `/api/builds` | 最近 50 个任务 |
| GET | `/api/builds/{id}` | 任务状态及元数据 |
| POST | `/api/builds/{id}/cancel` | 请求取消任务 |
| GET | `/api/builds/{id}/logs` | 纯文本日志；`?stream=true` 或 `Accept: text/event-stream` 启用 SSE |
| GET | `/api/builds/{id}/artifact` | 下载已完成任务的压缩包 |
| GET | `/api/system/status` | 编译环境和依赖状态 |
| POST | `/api/system/deps/install` | 在服务运行环境安装缺失的预定义依赖包 |
| GET | `/api/system/deps/logs` | JSON 日志历史；`?stream=true` 启用 SSE |

以下示例使用运行中实例的密钥：

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

`options` 传入参数目录中的 **ID**，不是原始 configure 参数。请求还支持 `path_overrides` 和 `third_party_sources`，字段定义见 [model.go](internal/model/model.go)。请省略 `target_os`、`target_arch`：这两个字段仅影响产物标签，不会配置交叉编译，当前校验也不充分。

预览成功只说明已生成参数，不代表源码地址、依赖组合或历史 Nginx 版本一定能编译。未知选项 ID 和部分无效覆盖值目前会被静默忽略，请核对最终生成的参数。取消构建目前通过 API 操作；Web 界面没有构建取消按钮，对 `cancelled` 终态的处理也不完整。

## 源码信任与执行边界

- 下载器会记录每份源码的 SHA-256；只有选定条目包含 `ExpectedSHA` 时才比较预期哈希。动态获取的 Nginx 版本和自定义源码 URL 通常没有该值；仅计算哈希不能证明源码来源，当前也未实现签名验证。
- 预设依赖版本和哈希维护在代码中。标注“最新”“推荐”的预设不会自动跟随上游更新。
- 下载过程在域名解析、实际连接及重定向阶段检查目标地址，拒绝私有或本地目标。这不能限制下载完成后构建脚本的行为。
- 解压器检查目录越界；每个压缩包限制 20,000 个条目、单个普通文件 512 MiB、普通文件总内容 1 GiB，并跳过链接及特殊文件。这不等于任务磁盘配额，压缩包下载本身也没有明确体积上限。
- configure、依赖构建脚本、make 和二进制验证都继承服务账户权限及环境变量。即使源码由后端下载，自定义源码包仍需作为可执行代码信任。
- 当前镜像内的服务以 root 运行，所有任务共用一个容器。容器与宿主机之间的边界取决于运行参数和挂载，它不提供任务间隔离；避免特权模式及无关宿主目录挂载。

## 数据与产物

| `DATA_DIR` 下的路径 | 内容 |
| --- | --- |
| `cache/` | 共享源码压缩包缓存 |
| `builds/<id>/meta.json` | 持久化任务元数据 |
| `builds/<id>/source/` | 当前任务使用的源码压缩包 |
| `builds/<id>/deps/` | 解压后的依赖库源码 |
| `builds/<id>/work/` | Nginx 源码及编译中间文件 |
| `builds/<id>/logs/` | `build.log`、`configure.log`、`error.log` |
| `builds/<id>/artifacts/` | 生成的 `.tar.gz` |

构建成功后删除 `source/`、`deps/`、`work/`；失败或取消的任务可能保留这些目录。目前没有定时保留策略或清理 API，内部 `PruneOldBuilds` 尚未接入运行流程，需安排磁盘监控及停机清理历史任务、缓存。

压缩包包含 `sbin/nginx`、`conf/`、`logs/`、存在时的 `html/`、打包的 `modules/*.so` 和 `BUILD_INFO.json`。程序不会执行 `make install`，也不会安装系统服务。自定义 configure 路径改变二进制默认值，不改变压缩包目录布局；随包配置来自源码树，可能需要按所选模块调整。

从源码构建 OpenSSL、PCRE、zlib 可以减少对这些特定共享库的依赖，但 libc 及其他模块库仍可能动态链接。目标环境需与构建环境兼容，并检查 `ldd` 输出；产物文件名不能证明兼容性。

部署前解压到新目录，将压缩包哈希与 API 元数据比较，检查 `nginx -V`，并对目标配置运行 `nginx -t`。在解压目录内可先执行：

```bash
./sbin/nginx -V
ldd ./sbin/nginx
./sbin/nginx -t -p "$PWD/" -c "$PWD/conf/nginx.conf" -e logs/error.log
```

内置冒烟检查使用生成的最小配置，不是随包配置或生产配置。目前只要输出含 `syntax is ok`，即使 `nginx -t` 非零退出也可能判定通过，因此 `completed` 不能替代目标机检查。`BUILD_INFO.json` 在冒烟检查之前生成；最终结果应查看任务 API 或 `meta.json`。

## 开发与语言包

```bash
go test ./...
go test -race ./...
go vet ./...
go build -o bin/nginx-builder ./cmd/server
```

这些命令覆盖既有测试和服务端编译，不代表验证过所有 Nginx 模块、源码及平台组合；真实 Nginx 构建验证仍需要 C 工具链和所选依赖。

界面语言包位于 `web/locales/`。新增语言时复制并翻译 `en.json`，在 `languages.json` 注册语言代码和名称，再重新编译二进制或镜像。新增语言可通过下拉框手动选择；浏览器语言自动匹配目前只识别中文，其余回退英文。后端消息的翻译需要单独实现。

## 许可证文件

当前仓库没有 `LICENSE` 文件。旧 README 的 MIT 链接指向不存在的路径，需由维护者补充所采用的许可证正文。
