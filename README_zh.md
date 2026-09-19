# Nginx 在线编译构建平台 (Nginx Online Web Builder)

[English](README.md) | [简体中文](README_zh.md)

基于 **Go** 实现的高性能、安全可控、轻量云原生的 Nginx 在线自定义编译平台。用户可在 Web 页面直观选择 Nginx 官方模块与参数，实时预览生成的 `./configure` 配置命令，后端在沙箱隔离工作区内完成源码拉取、完整性校验、编译构建、二进制有效性审计 (`nginx -V`) 并生成可直接下载的部署压缩包 (`tar.gz`)。

---

## 🌟 核心特性与架构规范

1. **版本管理与完整性校验**
   - 自动探测与拉取官方发布版本（包含 Stable 稳定版、Mainline 主线版与 Legacy 经典版）。默认采用官方最新 **Stable 稳定版**（当前为 `1.30.5`）。
   - 直接从 `nginx.org` 官方源下载，支持本地源码缓存，并在解压前执行强制性 **SHA-256 完整性哈希校验**。

2. **官方参数白名单与防命令注入设计**
   - 严格收录 Nginx 官方源码支持的编译参数：**SSL/TLS、HTTP、四层 Stream、Mail、性能与核心、调试、文件路径及事件驱动模型**。
   - **绝对禁止执行用户提交的任意 Shell 字符串**：前端提交结构化选项标识，后端通过安全白名单重构规范的 `./configure` 参数切片。
   - 内置双向互斥检测与依赖自动满足引擎（例如启用 `stream_ssl` 自动激活底层的 `--with-stream`）。

3. **编译期互斥提示与智能优化**
   - 废除选参或预览阶段生硬打断的 HTTP 400 阻断。
   - 参数预览区支持柔性温和警示（如 `--without-http_gzip_module` 与 `--with-http_gzip_static_module` 互斥）。
   - 用户点击 **“立即开始编译构建”** 时，系统会弹出交互确认框，列出所有冲突项并支持一键智能调和（保留功能模块、移除冲突禁用项、自动升级依赖）。

4. **第三方源码静态嵌入编译**
   - 支持将第三方依赖源码静态嵌入编译至 Nginx 二进制中：
     - **OpenSSL**（如 `3.4.1`、`3.0.16` LTS、`1.1.1w` Legacy 或自定义 tarball URL）。
     - **PCRE / PCRE2**（如 `10.45`、`10.42` 或自定义 tarball URL）。
     - **zlib**（如 `1.3.1` 或自定义 tarball URL）。
   - 生成无需依赖宿主机 `libssl.so`、`libpcre2.so` 共享库的完全绿色独立二进制。

5. **物理工作区物理隔离**
   - 每个构建任务依据唯一有序的 **ULID** 分配独立目录：
     ```text
     data/builds/<build-id>/
     ├── source/     # 源码压缩包与解压缓存
     ├── work/       # 独立工作树与编译中间文件
     ├── logs/       # build.log, configure.log, error.log
     └── artifacts/  # 校验合格的部署压缩包与元数据
     ```

6. **SSE 实时终端日志流**
   - 基于 **Server-Sent Events (SSE)** 将 `./configure` 与 `make` 的标准输出/错误实时流式推送至浏览器终端。
   - 前端自带构建阶段指示条、执行耗时计数与自动滚屏控制。

7. **编译产物自动化验证 (`nginx -V`)**
   - 编译完成后，后台守护程序自动调用工作区内的 `objs/nginx -V`。
   - 严审版本号一致性、可执行权限与配置参数吻合度，方才打包发布。

8. **国际化架构 (i18n) 与可插拔语言包**
   - 采用完全解耦、模块化的语言包架构，全部语言包位于 `web/locales/` 独立目录。
   - **仓库与平台界面默认采用英文**。
   - 支持基于浏览器语言 (`navigator.language`) 自动匹配，并提供顶部导航栏手动切换（English / 简体中文），偏好持久化保存在 `localStorage`。
   - **零代码新增多语言**：未来若要增加更多语言（如日语 `ja`、德语 `de`、法语 `fr`），仅需两步：
     1. 在 `web/locales/` 目录下新增 `<code-name>.json`（以 `en.json` 为模板翻译）；
     2. 在 `web/locales/languages.json` 清单中添加该语言名称（如 `{"code": "ja", "name": "日本語"}`）。前端导航栏会自动识别并按需动态加载，无需修改任何 HTML 或 JS 逻辑代码！

---

## 📡 RESTful API 接口规范

| 接口 | 方法 | 说明 |
| :--- | :---: | :--- |
| `/api/nginx/versions` | `GET` | 获取官方 Nginx 可选版本列表 |
| `/api/nginx/options` | `GET` | 获取官方编译参数白名单目录与分类树 |
| `/api/nginx/preview` | `POST` | 预览 `./configure` 完整命令及互斥提示 |
| `/api/builds` | `POST` | 创建并提交编译构建任务（支持 `auto_resolve_conflicts`） |
| `/api/builds` | `GET` | 查询最近的构建历史记录 |
| `/api/builds/{id}` | `GET` | 查询任务实时状态、指标、耗时及 `nginx -V` 验证结果 |
| `/api/builds/{id}/logs` | `GET` | 实时流式获取日志 (`?stream=true`) 或拉取完整文本 |
| `/api/builds/{id}/artifact` | `GET` | 下载编译打包好的 `tar.gz` 生产部署包 |
| `/api/system/status` | `GET` | 检查宿主系统 Linux 发行版、包管理器与 Nginx 编译依赖状态 |
| `/api/system/deps/install` | `POST` | 触发后台自动安装缺失依赖包 |
| `/api/system/deps/logs` | `GET` | 通过 SSE 实时流式获取依赖安装输出 (`?stream=true`) |

---

## 🐳 Docker 快速启动

```bash
# 构建镜像
docker build -t nginx-builder:latest .

# 启动容器
docker run -d \
  --name nginx-builder \
  -p 8090:8090 \
  -v nginx-data:/app/data \
  nginx-builder:latest
```

访问地址：`http://localhost:8090`

---

## 📄 开源许可证

基于 [MIT License](LICENSE) 协议发布。
