package builder

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"nginx-builder/internal/config"
	"nginx-builder/internal/model"
	"nginx-builder/internal/nginx"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ExecuteBuild runs the entire build lifecycle for a single BuildJob:
// 1. Downloading & verifying Nginx source
// 2. Unpacking tar.gz into isolated work dir
// 3. Generating configure arguments from whitelist
// 4. Executing ./configure
// 5. Executing make -j$(nproc)
// 6. Verifying compiled binary with `objs/nginx -V`
// 7. Staging and packaging output tar.gz
// 8. Calculating SHA256 and saving metadata
func ExecuteBuild(ctx context.Context, job *model.BuildJob, ws *Workspace, cfg *config.Config, broadcaster *LogBroadcaster) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	cacheDir := cfg.CacheDir
	limits := nginx.DownloadLimits{MaxBytes: cfg.MaxDownloadBytes, CacheBytes: cfg.MaxCacheBytes}

	logWriter := broadcaster
	errLogFile, err := os.OpenFile(ws.ErrorLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	defer errLogFile.Close()
	errLog := &limitedWriter{writer: errLogFile, remaining: config.MaxLogBytes}

	writeLog := func(format string, a ...any) {
		line := fmt.Sprintf("[%s] %s\n", time.Now().Format("15:04:05"), fmt.Sprintf(format, a...))
		_, _ = logWriter.Write([]byte(line))
	}

	writeErr := func(format string, a ...any) {
		line := fmt.Sprintf("[%s] [ERROR] %s\n", time.Now().Format("15:04:05"), fmt.Sprintf(format, a...))
		_, _ = logWriter.Write([]byte(line))
		_, _ = io.WriteString(errLog, line)
	}

	writeLog("==================================================================")
	writeLog("🚀 开始执行 Nginx 构建任务: %s", job.BuildID)
	writeLog("目标版本: %s | 操作系统: %s | 架构: %s", job.NginxVersion, job.TargetOS, job.TargetArch)
	writeLog("工作根目录: %s", ws.BaseDir)
	writeLog("==================================================================")

	// Step 1: Downloading source
	if err := setBuildPhase(ctx, job, model.StatusDownloading, "下载官方源码包并进行完整性校验", 10); err != nil {
		return err
	}
	saveMetadata(job, ws.MetadataPath)

	verInfo, err := nginx.GetVersionContext(ctx, job.NginxVersion)
	if err != nil {
		writeErr("获取版本信息失败: %v", err)
		return err
	}
	job.Update(func(j *model.BuildJob) {
		j.SourceURL = verInfo.SourceURL
	})

	tarballPath, sha256Hex, err := nginx.DownloadAndVerifySourceContext(ctx, ws.SourceDir, verInfo, cacheDir, limits, logWriter)
	if err != nil {
		writeErr("下载或校验源码失败: %v", err)
		return err
	}
	job.Update(func(j *model.BuildJob) {
		j.SourceSHA256 = sha256Hex
	})
	writeLog("✔ 源码下载与完整性校验通过 (SHA256: %s)", sha256Hex)

	// Step 2: Extracting source
	writeLog("[Extract] 解压源码包到独立工作空间: %s", ws.WorkDir)
	extractDirName, err := extractTarGzContext(ctx, tarballPath, ws.WorkDir)
	if err != nil {
		writeErr("解压源码失败: %v", err)
		return err
	}
	srcRoot := filepath.Join(ws.WorkDir, extractDirName)
	writeLog("✔ 源码解压完成，根目录: %s", srcRoot)

	// Step 2.5: Third-party source dependencies (OpenSSL, PCRE, zlib)
	resolvedDepDirs := make(map[string]string)
	if job.ThirdPartySources != nil {
		writeLog("------------------------------------------------------------------")
		writeLog("📦 正在准备第三方依赖库源码 (OpenSSL / PCRE / zlib) ...")

		// 1. OpenSSL Source
		if job.ThirdPartySources.UseOpenSSLSource {
			writeLog("[OpenSSL] 解析并准备 OpenSSL 源码包 (版本: %s)...", job.ThirdPartySources.OpenSSLVersion)
			sslInfo, err := nginx.ResolveDepSource("openssl", job.ThirdPartySources.OpenSSLVersion, job.ThirdPartySources.OpenSSLSourceURL)
			if err != nil {
				writeErr("解析 OpenSSL 源码信息失败: %v", err)
				return err
			}
			tarPath, shaHex, err := nginx.DownloadAndVerifyDepContext(ctx, ws.SourceDir, sslInfo, cacheDir, limits, logWriter)
			if err != nil {
				writeErr("下载或校验 OpenSSL 源码失败: %v", err)
				return err
			}
			writeLog("✔ OpenSSL 源码包就绪 (SHA256: %s)，正在解压至: %s", shaHex, ws.DepsDir)
			dirName, err := extractTarGzContext(ctx, tarPath, ws.DepsDir)
			if err != nil {
				writeErr("解压 OpenSSL 源码包失败: %v", err)
				return err
			}
			resolvedDepDirs["openssl"] = filepath.Join(ws.DepsDir, dirName)
			writeLog("✔ OpenSSL 源码目录已就绪: %s", resolvedDepDirs["openssl"])
		}

		// 2. PCRE Source
		if job.ThirdPartySources.UsePCRESource {
			writeLog("[PCRE] 解析并准备 PCRE/PCRE2 源码包 (版本: %s)...", job.ThirdPartySources.PCREVersion)
			pcreInfo, err := nginx.ResolveDepSource("pcre", job.ThirdPartySources.PCREVersion, job.ThirdPartySources.PCRESourceURL)
			if err != nil {
				writeErr("解析 PCRE 源码信息失败: %v", err)
				return err
			}
			tarPath, shaHex, err := nginx.DownloadAndVerifyDepContext(ctx, ws.SourceDir, pcreInfo, cacheDir, limits, logWriter)
			if err != nil {
				writeErr("下载或校验 PCRE 源码失败: %v", err)
				return err
			}
			writeLog("✔ PCRE 源码包就绪 (SHA256: %s)，正在解压至: %s", shaHex, ws.DepsDir)
			dirName, err := extractTarGzContext(ctx, tarPath, ws.DepsDir)
			if err != nil {
				writeErr("解压 PCRE 源码包失败: %v", err)
				return err
			}
			resolvedDepDirs["pcre"] = filepath.Join(ws.DepsDir, dirName)
			writeLog("✔ PCRE 源码目录已就绪: %s", resolvedDepDirs["pcre"])
		}

		// 3. zlib Source
		if job.ThirdPartySources.UseZlibSource {
			writeLog("[zlib] 解析并准备 zlib 源码包 (版本: %s)...", job.ThirdPartySources.ZlibVersion)
			zlibInfo, err := nginx.ResolveDepSource("zlib", job.ThirdPartySources.ZlibVersion, job.ThirdPartySources.ZlibSourceURL)
			if err != nil {
				writeErr("解析 zlib 源码信息失败: %v", err)
				return err
			}
			tarPath, shaHex, err := nginx.DownloadAndVerifyDepContext(ctx, ws.SourceDir, zlibInfo, cacheDir, limits, logWriter)
			if err != nil {
				writeErr("下载或校验 zlib 源码失败: %v", err)
				return err
			}
			writeLog("✔ zlib 源码包就绪 (SHA256: %s)，正在解压至: %s", shaHex, ws.DepsDir)
			dirName, err := extractTarGzContext(ctx, tarPath, ws.DepsDir)
			if err != nil {
				writeErr("解压 zlib 源码包失败: %v", err)
				return err
			}
			resolvedDepDirs["zlib"] = filepath.Join(ws.DepsDir, dirName)
			writeLog("✔ zlib 源码目录已就绪: %s", resolvedDepDirs["zlib"])
		}
	}

	// Step 3: Configuring
	if err := setBuildPhase(ctx, job, model.StatusConfiguring, "生成安全编译配置参数并执行 ./configure", 30); err != nil {
		return err
	}
	saveMetadata(job, ws.MetadataPath)

	configureArgs, warns, conflicts, err := nginx.ValidateAndBuildArgs(job.Options, job.PathOverrides, job.ThirdPartySources, resolvedDepDirs, true)
	if err != nil {
		writeErr("编译参数校验失败: %v", err)
		return err
	}
	_ = conflicts
	for _, w := range warns {
		writeLog("⚠️ %s", w)
	}
	cmdPreview := fmt.Sprintf("./configure \\\n  %s", strings.Join(configureArgs, " \\\n  "))
	job.Update(func(j *model.BuildJob) {
		j.ConfigureArguments = configureArgs
		j.FullConfigureCmd = cmdPreview
	})

	writeLog("------------------------------------------------------------------")
	writeLog("🔧 生成的正式 ./configure 命令:")
	writeLog("%s", cmdPreview)
	writeLog("------------------------------------------------------------------")

	confLogFile, err := os.OpenFile(ws.ConfigureLogPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		writeErr("无法创建 configure.log: %v", err)
		return err
	} else {
		defer confLogFile.Close()
	}

	confCmd := newBuildCommand(ctx, "./configure", configureArgs...)
	confCmd.Dir = srcRoot
	confCmd.Env = append(buildEnvironment(), "LANG=C", "LC_ALL=C")
	confLog := &limitedWriter{writer: confLogFile, remaining: config.MaxLogBytes}
	confCmd.Stdout = io.MultiWriter(logWriter, confLog)
	confCmd.Stderr = io.MultiWriter(logWriter, confLog, errLog)

	writeLog("正在运行 ./configure ...")
	if err := confCmd.Run(); err != nil {
		writeErr("./configure 执行失败: %v", err)
		return fmt.Errorf("configure 失败: %w", err)
	}
	writeLog("✔ ./configure 配置成功完成！")

	// Step 4: Building (make)
	if err := setBuildPhase(ctx, job, model.StatusBuilding, "执行 make 编译二进制产物", 50); err != nil {
		return err
	}
	saveMetadata(job, ws.MetadataPath)

	numCPU := runtime.NumCPU()
	if numCPU > 8 {
		numCPU = 8 // cap parallel make to prevent excessive CPU thrashing
	}
	if numCPU < 1 {
		numCPU = 1
	}

	writeLog("------------------------------------------------------------------")
	writeLog("🔨 开始编译 make (并行度: -j%d) ...", numCPU)
	writeLog("------------------------------------------------------------------")

	makeCmd := newBuildCommand(ctx, "make", fmt.Sprintf("-j%d", numCPU))
	makeCmd.Dir = srcRoot
	makeCmd.Env = append(buildEnvironment(), "LANG=C", "LC_ALL=C")
	makeCmd.Stdout = logWriter
	makeCmd.Stderr = io.MultiWriter(logWriter, errLog)

	if err := makeCmd.Run(); err != nil {
		writeErr("make 编译失败: %v", err)
		return fmt.Errorf("make 编译失败: %w", err)
	}
	writeLog("✔ make 编译成功！")

	// Step 5: Verification (nginx -V)
	job.Update(func(j *model.BuildJob) {
		j.Progress = 75
	})
	nginxBinPath := filepath.Join(srcRoot, "objs", "nginx")
	writeLog("------------------------------------------------------------------")
	writeLog("🔍 验证编译产物: %s -V", nginxBinPath)
	writeLog("------------------------------------------------------------------")

	verifyRes := verifyCompiledNginx(ctx, nginxBinPath, job.NginxVersion, job.Clone().ConfigureArguments)
	job.Update(func(j *model.BuildJob) {
		j.VerifyResult = verifyRes
	})

	if !verifyRes.Passed {
		writeErr("❌ 编译结果校验未通过: %s", verifyRes.CheckError)
		return fmt.Errorf("nginx -V 校验未通过: %s", verifyRes.CheckError)
	}
	writeLog("✔ nginx -V 验证通过！")
	writeLog("%s", verifyRes.RawOutput)

	// Step 6: Packaging artifacts
	if err := setBuildPhase(ctx, job, model.StatusPackaging, "整理构建产物并打包为 tar.gz", 85); err != nil {
		return err
	}
	saveMetadata(job, ws.MetadataPath)

	artifactName := fmt.Sprintf("nginx-%s-%s-%s.tar.gz", job.NginxVersion, job.TargetOS, job.TargetArch)
	artifactPath, err := safeArtifactPath(ws.ArtifactsDir, artifactName)
	if err != nil {
		return err
	}

	writeLog("正在将构建产物打包至: %s ...", artifactPath)
	artifactInfo, err := packageArtifactsContext(ctx, srcRoot, artifactPath, artifactName, job)
	if err != nil {
		writeErr("打包构建产物失败: %v", err)
		return fmt.Errorf("打包失败: %w", err)
	}
	job.Update(func(j *model.BuildJob) {
		j.Artifact = artifactInfo
	})

	writeLog("✔ 构建产物打包成功: %s", artifactInfo.Name)
	writeLog("  大小: %.2f MB (%d bytes)", float64(artifactInfo.Size)/(1024*1024), artifactInfo.Size)
	writeLog("  SHA256: %s", artifactInfo.SHA256)
	writeLog("  下载接口: %s", artifactInfo.DownloadURL)

	// Step 7: Packaging Smoke Test
	writeLog("------------------------------------------------------------------")
	writeLog("🧪 正在对打包产物执行独立沙盒冒烟自检...")
	smokeSummary, err := smokeTestArtifact(ctx, artifactPath, job.NginxVersion, job.Clone().ConfigureArguments)
	if err != nil {
		writeErr("❌ 产物冒烟测试未通过: %v", err)
		return fmt.Errorf("产物冒烟测试未通过: %w", err)
	}
	writeLog("✔ 产物冒烟测试通过！配置语法检验正常: %s", smokeSummary)
	job.Update(func(j *model.BuildJob) {
		if j.VerifyResult != nil {
			j.VerifyResult.SmokeTest = smokeSummary
		}
	})

	if err := ctx.Err(); err != nil {
		return err
	}
	writeLog("✔ 编译、打包和产物检查完成")

	return nil
}

// cleanIntermediateFiles removes heavy temporary compilation directories (work, deps, source)
// while preserving final artifacts, logs, and metadata.
func cleanIntermediateFiles(ws *Workspace) {
	if ws == nil {
		return
	}
	_ = os.RemoveAll(ws.WorkDir)
	_ = os.RemoveAll(ws.DepsDir)
	_ = os.RemoveAll(ws.SourceDir)
}

// verifyCompiledNginx executes `objs/nginx -V` and verifies version, exit code, and configure args.
func verifyCompiledNginx(ctx context.Context, binPath string, expectedVersion string, expectedArgs []string) *model.VerifyResult {
	res := &model.VerifyResult{Passed: false}

	// 1. Check binary exists and is executable
	info, err := os.Stat(binPath)
	if err != nil {
		res.CheckError = fmt.Sprintf("未找到编译后的二进制文件: %v", err)
		return res
	}
	if info.Mode()&0111 == 0 {
		res.CheckError = "编译后的 nginx 文件没有可执行权限"
		return res
	}

	// 2. Run nginx -V
	cmd := newBuildCommand(ctx, binPath, "-V")
	outBytes, err := boundedCommandOutput(cmd)
	rawOutput := strings.TrimSpace(string(outBytes))
	res.RawOutput = rawOutput

	if err != nil {
		res.CheckError = fmt.Sprintf("运行 nginx -V 出错 (exit code != 0): %v (输出: %s)", err, rawOutput)
		return res
	}

	// 3. Validate version string
	versionMatch := regexp.MustCompile(`(?m)^nginx version: nginx/([^\s]+)`).FindStringSubmatch(rawOutput)
	if len(versionMatch) == 2 && versionMatch[1] == expectedVersion {
		res.VersionMatch = true
	} else {
		res.CheckError = fmt.Sprintf("版本号不匹配: 期望包含 %s, 实际输出: %s", expectedVersion, rawOutput)
		return res
	}

	// 4. Validate configure arguments strictly
	actualArgs := parseConfigureArguments(rawOutput)
	missing, unexpected, match := compareConfigureArgs(expectedArgs, actualArgs)
	res.MissingArgs = missing
	res.UnexpectedArgs = unexpected
	res.ArgsMatch = match
	if !match {
		res.CheckError = fmt.Sprintf("编译参数核验不通过，缺少期望编译参数: %s", strings.Join(missing, ", "))
		return res
	}

	// 5. Dynamic libraries audit (ldd)
	res.SharedLibs = auditDynamicLibraries(ctx, binPath)

	res.Passed = true
	lines := strings.Split(rawOutput, "\n")
	if len(lines) > 0 {
		res.VersionText = lines[0]
	}
	return res
}

func parseConfigureArguments(rawOutput string) []string {
	const marker = "configure arguments:"
	idx := strings.Index(rawOutput, marker)
	if idx == -1 {
		return nil
	}
	argsStr := strings.TrimSpace(rawOutput[idx+len(marker):])
	var args []string
	var cur strings.Builder
	inQuote := false
	var quoteChar rune

	for _, r := range argsStr {
		switch {
		case (r == '\'' || r == '"') && !inQuote:
			inQuote = true
			quoteChar = r
		case inQuote && r == quoteChar:
			inQuote = false
		case (r == ' ' || r == '\t' || r == '\n') && !inQuote:
			if cur.Len() > 0 {
				args = append(args, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		args = append(args, cur.String())
	}
	return args
}

func normalizeArg(arg string) string {
	a := strings.TrimSpace(arg)
	a = strings.Trim(a, "'\"")
	return a
}

func compareConfigureArgs(expectedArgs, actualArgs []string) (missing []string, unexpected []string, match bool) {
	actualMap := make(map[string]bool)
	for _, a := range actualArgs {
		actualMap[normalizeArg(a)] = true
	}

	for _, exp := range expectedArgs {
		norm := normalizeArg(exp)
		if !actualMap[norm] {
			missing = append(missing, exp)
		}
	}

	expectedMap := make(map[string]bool)
	for _, exp := range expectedArgs {
		expectedMap[normalizeArg(exp)] = true
	}

	for _, act := range actualArgs {
		norm := normalizeArg(act)
		if !expectedMap[norm] {
			unexpected = append(unexpected, act)
		}
	}

	return missing, unexpected, len(missing) == 0
}

func auditDynamicLibraries(ctx context.Context, binPath string) []string {
	cmd := newBuildCommand(ctx, "ldd", binPath)
	out, err := boundedCommandOutput(cmd)
	if err != nil {
		return nil
	}
	var libs []string
	for _, line := range strings.Split(string(out), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			libs = append(libs, trimmed)
		}
	}
	return libs
}

func smokeTestArtifact(ctx context.Context, artifactPath, expectedVersion string, options []string) (string, error) {
	tmpDir, err := os.MkdirTemp("", "nginx-smoke-*")
	if err != nil {
		return "", fmt.Errorf("创建冒烟测试沙盒目录失败: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	_, err = extractTarGzContext(ctx, artifactPath, tmpDir)
	if err != nil {
		return "", fmt.Errorf("冒烟测试产物解包失败: %w", err)
	}

	// Ensure runtime logs directory exists in test sandbox
	_ = os.MkdirAll(filepath.Join(tmpDir, "logs"), 0755)

	binPath := filepath.Join(tmpDir, "sbin", "nginx")

	// Test 1: Binary version execution (-v)
	cmdV := newBuildCommand(ctx, binPath, "-v")
	outV, err := boundedCommandOutput(cmdV)
	if err != nil {
		return "", fmt.Errorf("冒烟测试执行 sbin/nginx -v 失败: %w (输出: %s)", err, strings.TrimSpace(string(outV)))
	}
	versionMatch := regexp.MustCompile(`(?m)^nginx version: nginx/([^\s]+)`).FindStringSubmatch(string(outV))
	if len(versionMatch) != 2 || versionMatch[1] != expectedVersion {
		return "", fmt.Errorf("冒烟测试版本不匹配: 输出 %s 未包含期望版本 %s", string(outV), expectedVersion)
	}

	// Test 2: Generate dedicated minimal configuration matching compiled modules
	testConfPath := filepath.Join(tmpDir, "smoke-test.conf")
	testConfContent := generateSmokeTestConfig(options)
	if err := os.WriteFile(testConfPath, []byte(testConfContent), 0644); err != nil {
		return "", fmt.Errorf("生成冒烟测试配置失败: %w", err)
	}

	// Test 3: Configuration syntax inspection (-t)
	cmdT := newBuildCommand(ctx, binPath, "-t", "-c", testConfPath, "-p", tmpDir, "-e", "logs/error.log")
	outT, err := boundedCommandOutput(cmdT)
	smokeSummary := strings.TrimSpace(string(outT))
	if err != nil {
		return smokeSummary, fmt.Errorf("冒烟测试配置文件语法检验 (-t) 失败: %w (输出: %s)", err, smokeSummary)
	}

	return smokeSummary, nil
}

func generateSmokeTestConfig(options []string) string {
	selected := make(map[string]bool)
	for _, o := range options {
		selected[o] = true
	}
	disabled := func(id, flag string) bool { return selected[id] || selected[flag] }
	var sb strings.Builder
	sb.WriteString("pid logs/nginx.pid;\nerror_log logs/error.log;\n")
	if os.Geteuid() == 0 {
		if account, err := user.Current(); err == nil {
			fmt.Fprintf(&sb, "user %s;\n", strconv.Quote(account.Username))
		}
	}
	sb.WriteString("events { worker_connections 64; }\n")
	if !disabled("without_http", "--without-http") {
		sb.WriteString("http {\n access_log off;\n client_body_temp_path client_body_temp;\n")
		for _, module := range []string{"proxy", "fastcgi", "uwsgi", "scgi"} {
			if !disabled("without_http_"+module, "--without-http_"+module+"_module") {
				fmt.Fprintf(&sb, " %s_temp_path %s_temp;\n", module, module)
			}
		}
		sb.WriteString(" server { listen 127.0.0.1:18080; }\n}\n")
	}
	if (selected["stream"] || selected["--with-stream"]) && disabled("without_http", "--without-http") {
		sb.WriteString("stream { server { listen 127.0.0.1:18081; return \"ok\"; } }\n")
	}
	return sb.String()
}

// packageArtifacts packages the compiled nginx binary, default conf, and docs into a tar.gz.
func packageArtifacts(srcRoot, destTarGz, artifactName string, job *model.BuildJob) (*model.ArtifactInfo, error) {
	return packageArtifactsContext(context.Background(), srcRoot, destTarGz, artifactName, job)
}
func packageArtifactsContext(ctx context.Context, srcRoot, destTarGz, artifactName string, job *model.BuildJob) (*model.ArtifactInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := safeArtifactPath(filepath.Dir(destTarGz), artifactName); err != nil {
		return nil, err
	}
	outFile, err := os.CreateTemp(filepath.Dir(destTarGz), ".artifact-*")
	if err != nil {
		return nil, err
	}
	defer outFile.Close()
	defer os.Remove(outFile.Name())

	hasher := sha256.New()
	multiWriter := contextWriter{ctx: ctx, writer: io.MultiWriter(outFile, hasher)}
	gw := gzip.NewWriter(multiWriter)
	tw := tar.NewWriter(contextWriter{ctx: ctx, writer: gw})

	// Items to package:
	// 1. sbin/nginx (from objs/nginx)
	nginxBin := filepath.Join(srcRoot, "objs", "nginx")
	if err := addFileToTar(tw, nginxBin, "sbin/nginx", 0755); err != nil {
		return nil, fmt.Errorf("打包 sbin/nginx 失败: %w", err)
	}

	// 2. conf/ directory (from srcRoot/conf)
	confDir := filepath.Join(srcRoot, "conf")
	if err := addDirToTar(tw, confDir, "conf"); err != nil {
		return nil, fmt.Errorf("打包 conf 目录失败: %w", err)
	}

	// 2.5 logs/ directory structure (essential runtime directory)
	logsHeader := &tar.Header{
		Name:     "logs/",
		Mode:     0755,
		Typeflag: tar.TypeDir,
		ModTime:  time.Now(),
	}
	if err := tw.WriteHeader(logsHeader); err != nil {
		return nil, fmt.Errorf("打包 logs 目录失败: %w", err)
	}

	// 3. html/ directory (from srcRoot/html)
	htmlDir := filepath.Join(srcRoot, "html")
	if err := addDirToTar(tw, htmlDir, "html"); err != nil {
		// non-fatal if html dir doesn't exist
	}

	// 4. Any dynamic modules (.so) under objs/*.so
	objsDir := filepath.Join(srcRoot, "objs")
	entries, _ := os.ReadDir(objsDir)
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".so") {
			soPath := filepath.Join(objsDir, entry.Name())
			if err := addFileToTar(tw, soPath, filepath.Join("modules", entry.Name()), 0755); err != nil {
				return nil, fmt.Errorf("打包动态模块失败: %w", err)
			}
		}
	}

	// 5. Snapshot before final smoke testing; API/meta.json hold the final result.
	jobSnapshot := job.Clone()
	jobSnapshot.Status = model.StatusPackaging
	jobSnapshot.CurrentStep = "产物已打包，等待最终自检"
	jobSnapshot.Progress = 85
	jobSnapshot.EndTime = nil
	jobSnapshot.DurationSeconds = time.Since(jobSnapshot.StartTime).Seconds()
	infoBytes, _ := json.MarshalIndent(jobSnapshot, "", "  ")
	if err := addBytesToTar(tw, infoBytes, "BUILD_INFO.json", 0644); err != nil {
		return nil, fmt.Errorf("打包 BUILD_INFO.json 失败: %w", err)
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gw.Close(); err != nil {
		return nil, err
	}

	stat, err := outFile.Stat()
	if err != nil {
		return nil, err
	}

	shaHex := hex.EncodeToString(hasher.Sum(nil))
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := outFile.Close(); err != nil {
		return nil, err
	}
	if err := os.Rename(outFile.Name(), destTarGz); err != nil {
		return nil, err
	}
	return &model.ArtifactInfo{
		Name:        artifactName,
		Size:        stat.Size(),
		SHA256:      shaHex,
		DownloadURL: artifactDownloadPath(job.BuildID),
		Path:        destTarGz,
	}, nil
}

func artifactDownloadPath(buildID string) string {
	return fmt.Sprintf("/api/builds/%s/artifact", buildID)
}

func addFileToTar(tw *tar.Writer, srcPath, tarPath string, mode int64) error {
	f, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return err
	}

	header := &tar.Header{
		Name:    tarPath,
		Size:    stat.Size(),
		Mode:    mode,
		ModTime: stat.ModTime(),
	}

	if err := tw.WriteHeader(header); err != nil {
		return err
	}

	_, err = io.Copy(tw, f)
	return err
}

func addDirToTar(tw *tar.Writer, dirPath, prefix string) error {
	return filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dirPath, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		tarPath := filepath.Join(prefix, rel)

		if info.IsDir() {
			header := &tar.Header{
				Name:     tarPath + "/",
				Mode:     0755,
				Typeflag: tar.TypeDir,
				ModTime:  info.ModTime(),
			}
			return tw.WriteHeader(header)
		}

		return addFileToTar(tw, path, tarPath, 0644)
	})
}

func addBytesToTar(tw *tar.Writer, content []byte, tarPath string, mode int64) error {
	header := &tar.Header{
		Name:    tarPath,
		Size:    int64(len(content)),
		Mode:    mode,
		ModTime: time.Now(),
	}
	if err := tw.WriteHeader(header); err != nil {
		return err
	}
	_, err := tw.Write(content)
	return err
}

// extractTarGz extracts a .tar.gz archive and returns the top-level directory name.
// It enforces strict ZipSlip / path traversal checks and decompression bomb limits.
func extractTarGz(tarGzPath, destDir string) (string, error) {
	return extractTarGzContext(context.Background(), tarGzPath, destDir)
}
func extractTarGzContext(ctx context.Context, tarGzPath, destDir string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	const (
		maxTotalBytes = 1024 * 1024 * 1024 // 1GB maximum decompressed size
		maxFileBytes  = 512 * 1024 * 1024  // 512MB maximum single file size
		maxFiles      = 20000              // Maximum number of entries
	)

	f, err := os.Open(tarGzPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	gr, err := gzip.NewReader(contextReader{ctx: ctx, reader: f})
	if err != nil {
		return "", err
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	var candidateTopDir string
	var totalBytes int64
	var fileCount int
	cleanDest := filepath.Clean(destDir)

	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}

		fileCount++
		if fileCount > maxFiles {
			return "", fmt.Errorf("解压文件数量超限 (超过 %d 个条目)，疑似压缩炸弹", maxFiles)
		}

		// Skip PAX header metadata
		if header.Typeflag == tar.TypeXHeader || header.Typeflag == tar.TypeXGlobalHeader || header.Name == "pax_global_header" {
			continue
		}

		target := filepath.Join(cleanDest, header.Name)
		cleanTarget := filepath.Clean(target)

		// Strict ZipSlip check using filepath.Rel
		rel, err := filepath.Rel(cleanDest, cleanTarget)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("非法压缩包路径逃逸: %s", header.Name)
		}

		parts := strings.Split(filepath.ToSlash(header.Name), "/")
		if len(parts) > 0 && candidateTopDir == "" && parts[0] != "" && parts[0] != "pax_global_header" {
			candidateTopDir = parts[0]
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(cleanTarget, 0755); err != nil {
				return "", err
			}
		case tar.TypeReg:
			_ = os.MkdirAll(filepath.Dir(cleanTarget), 0755)
			outFile, err := os.OpenFile(cleanTarget, os.O_CREATE|os.O_RDWR|os.O_TRUNC, header.FileInfo().Mode())
			if err != nil {
				return "", err
			}

			// Guard against single file bomb
			lr := io.LimitReader(contextReader{ctx: ctx, reader: tr}, maxFileBytes+1)
			written, err := io.Copy(outFile, lr)
			outFile.Close()
			if err != nil {
				return "", err
			}
			if written > maxFileBytes {
				return "", fmt.Errorf("单文件解压体积超限 (超过 512MB): %s", header.Name)
			}
			totalBytes += written
			if totalBytes > maxTotalBytes {
				return "", fmt.Errorf("总解压体积超限 (超过 1GB)，已终止解压")
			}
		}
	}

	// Verify top directory exists on disk or scan destDir
	if candidateTopDir != "" {
		if fi, err := os.Stat(filepath.Join(destDir, candidateTopDir)); err == nil && fi.IsDir() {
			return candidateTopDir, nil
		}
	}

	// Fallback scan: find first actual directory in destDir
	entries, err := os.ReadDir(destDir)
	if err == nil {
		for _, e := range entries {
			if e.IsDir() && e.Name() != "pax_global_header" {
				return e.Name(), nil
			}
		}
	}

	return candidateTopDir, nil
}

var metadataMu sync.Mutex

func saveMetadata(job *model.BuildJob, metaPath string) error {
	if job == nil {
		return nil
	}
	metadataMu.Lock()
	defer metadataMu.Unlock()
	data, err := json.MarshalIndent(job.Clone(), "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(metaPath), ".meta-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(name, metaPath)
}

func setBuildPhase(ctx context.Context, job *model.BuildJob, status model.BuildStatus, step string, progress int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var err error
	job.Update(func(j *model.BuildJob) {
		if model.IsTerminal(j.Status) {
			err = context.Canceled
			return
		}
		j.Status = status
		j.CurrentStep = step
		j.Progress = progress
	})
	return err
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

type contextWriter struct {
	ctx    context.Context
	writer io.Writer
}

func (w contextWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	return w.writer.Write(p)
}

func newBuildCommand(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = buildEnvironment()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 3 * time.Second
	return cmd
}

// CollectHostInfo gathers server host environment details.
func CollectHostInfo() *model.HostInfo {
	hostname, _ := os.Hostname()
	gccVer := "unknown"
	out, err := exec.Command("gcc", "-dumpversion").Output()
	if err == nil {
		gccVer = strings.TrimSpace(string(out))
	}

	return &model.HostInfo{
		OS:         runtime.GOOS,
		Arch:       runtime.GOARCH,
		Hostname:   hostname,
		GoVersion:  runtime.Version(),
		NumCPU:     runtime.NumCPU(),
		GCCVersion: gccVer,
	}
}

// Keep service credentials out of compiler/script environments.
func buildEnvironment() []string {
	var env []string
	for _, item := range os.Environ() {
		if !strings.HasPrefix(item, "AUTH_KEY=") {
			env = append(env, item)
		}
	}
	return env
}

func boundedCommandOutput(cmd *exec.Cmd) ([]byte, error) {
	var output bytes.Buffer
	limited := &limitedWriter{writer: &output, remaining: 1 << 20}
	cmd.Stdout, cmd.Stderr = limited, limited
	err := cmd.Run()
	if limited.truncated && err == nil {
		err = fmt.Errorf("command output exceeds 1 MiB")
	}
	return output.Bytes(), err
}
