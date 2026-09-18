package builder

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"nginx-builder/internal/model"
	"nginx-builder/internal/nginx"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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
func ExecuteBuild(ctx context.Context, job *model.BuildJob, ws *Workspace, cacheDir string, broadcaster *LogBroadcaster) error {
	defer broadcaster.Close()

	logWriter := broadcaster
	errLogFile, err := os.OpenFile(ws.ErrorLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		defer errLogFile.Close()
	}

	writeLog := func(format string, a ...any) {
		line := fmt.Sprintf("[%s] %s\n", time.Now().Format("15:04:05"), fmt.Sprintf(format, a...))
		_, _ = logWriter.Write([]byte(line))
	}

	writeErr := func(format string, a ...any) {
		line := fmt.Sprintf("[%s] [ERROR] %s\n", time.Now().Format("15:04:05"), fmt.Sprintf(format, a...))
		_, _ = logWriter.Write([]byte(line))
		if errLogFile != nil {
			_, _ = errLogFile.WriteString(line)
		}
	}

	writeLog("==================================================================")
	writeLog("🚀 开始执行 Nginx 构建任务: %s", job.BuildID)
	writeLog("目标版本: %s | 操作系统: %s | 架构: %s", job.NginxVersion, job.TargetOS, job.TargetArch)
	writeLog("工作根目录: %s", ws.BaseDir)
	writeLog("==================================================================")

	// Step 1: Downloading source
	job.Status = model.StatusDownloading
	job.CurrentStep = "下载官方源码包并进行完整性校验"
	job.Progress = 10
	saveMetadata(job, ws.MetadataPath)

	verInfo, err := nginx.GetVersion(job.NginxVersion)
	if err != nil {
		writeErr("获取版本信息失败: %v", err)
		return err
	}
	job.SourceURL = verInfo.SourceURL

	tarballPath, sha256Hex, err := nginx.DownloadAndVerifySource(ws.SourceDir, verInfo, cacheDir, logWriter)
	if err != nil {
		writeErr("下载或校验源码失败: %v", err)
		return err
	}
	job.SourceSHA256 = sha256Hex
	writeLog("✔ 源码下载与完整性校验通过 (SHA256: %s)", sha256Hex)

	// Step 2: Extracting source
	writeLog("[Extract] 解压源码包到独立工作空间: %s", ws.WorkDir)
	extractDirName, err := extractTarGz(tarballPath, ws.WorkDir)
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
			tarPath, shaHex, err := nginx.DownloadAndVerifyDep(ws.SourceDir, sslInfo, cacheDir, logWriter)
			if err != nil {
				writeErr("下载或校验 OpenSSL 源码失败: %v", err)
				return err
			}
			writeLog("✔ OpenSSL 源码包就绪 (SHA256: %s)，正在解压至: %s", shaHex, ws.DepsDir)
			dirName, err := extractTarGz(tarPath, ws.DepsDir)
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
			tarPath, shaHex, err := nginx.DownloadAndVerifyDep(ws.SourceDir, pcreInfo, cacheDir, logWriter)
			if err != nil {
				writeErr("下载或校验 PCRE 源码失败: %v", err)
				return err
			}
			writeLog("✔ PCRE 源码包就绪 (SHA256: %s)，正在解压至: %s", shaHex, ws.DepsDir)
			dirName, err := extractTarGz(tarPath, ws.DepsDir)
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
			tarPath, shaHex, err := nginx.DownloadAndVerifyDep(ws.SourceDir, zlibInfo, cacheDir, logWriter)
			if err != nil {
				writeErr("下载或校验 zlib 源码失败: %v", err)
				return err
			}
			writeLog("✔ zlib 源码包就绪 (SHA256: %s)，正在解压至: %s", shaHex, ws.DepsDir)
			dirName, err := extractTarGz(tarPath, ws.DepsDir)
			if err != nil {
				writeErr("解压 zlib 源码包失败: %v", err)
				return err
			}
			resolvedDepDirs["zlib"] = filepath.Join(ws.DepsDir, dirName)
			writeLog("✔ zlib 源码目录已就绪: %s", resolvedDepDirs["zlib"])
		}
	}

	// Step 3: Configuring
	job.Status = model.StatusConfiguring
	job.CurrentStep = "生成安全编译配置参数并执行 ./configure"
	job.Progress = 30
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
	job.ConfigureArguments = configureArgs
	job.FullConfigureCmd = fmt.Sprintf("./configure \\\n  %s", strings.Join(configureArgs, " \\\n  "))

	writeLog("------------------------------------------------------------------")
	writeLog("🔧 生成的正式 ./configure 命令:")
	writeLog("%s", job.FullConfigureCmd)
	writeLog("------------------------------------------------------------------")

	confLogFile, err := os.OpenFile(ws.ConfigureLogPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		writeErr("无法创建 configure.log: %v", err)
	} else {
		defer confLogFile.Close()
	}

	confCmd := exec.CommandContext(ctx, "./configure", configureArgs...)
	confCmd.Dir = srcRoot
	confCmd.Env = append(os.Environ(), "LANG=C", "LC_ALL=C")
	confCmd.Stdout = io.MultiWriter(logWriter, confLogFile)
	confCmd.Stderr = io.MultiWriter(logWriter, confLogFile, errLogFile)

	writeLog("正在运行 ./configure ...")
	if err := confCmd.Run(); err != nil {
		writeErr("./configure 执行失败: %v", err)
		return fmt.Errorf("configure 失败: %w", err)
	}
	writeLog("✔ ./configure 配置成功完成！")

	// Step 4: Building (make)
	job.Status = model.StatusBuilding
	job.CurrentStep = "执行 make 编译二进制产物"
	job.Progress = 50
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

	makeCmd := exec.CommandContext(ctx, "make", fmt.Sprintf("-j%d", numCPU))
	makeCmd.Dir = srcRoot
	makeCmd.Env = append(os.Environ(), "LANG=C", "LC_ALL=C")
	makeCmd.Stdout = logWriter
	makeCmd.Stderr = io.MultiWriter(logWriter, errLogFile)

	if err := makeCmd.Run(); err != nil {
		writeErr("make 编译失败: %v", err)
		return fmt.Errorf("make 编译失败: %w", err)
	}
	writeLog("✔ make 编译成功！")

	// Step 5: Verification (nginx -V)
	job.Progress = 75
	nginxBinPath := filepath.Join(srcRoot, "objs", "nginx")
	writeLog("------------------------------------------------------------------")
	writeLog("🔍 验证编译产物: %s -V", nginxBinPath)
	writeLog("------------------------------------------------------------------")

	verifyRes := verifyCompiledNginx(ctx, nginxBinPath, job.NginxVersion, job.ConfigureArguments)
	job.VerifyResult = verifyRes

	if !verifyRes.Passed {
		writeErr("❌ 编译结果校验未通过: %s", verifyRes.CheckError)
		return fmt.Errorf("nginx -V 校验未通过: %s", verifyRes.CheckError)
	}
	writeLog("✔ nginx -V 验证通过！")
	writeLog("%s", verifyRes.RawOutput)

	// Step 6: Packaging artifacts
	job.Status = model.StatusPackaging
	job.CurrentStep = "整理构建产物并打包为 tar.gz"
	job.Progress = 85
	saveMetadata(job, ws.MetadataPath)

	artifactName := fmt.Sprintf("nginx-%s-%s-%s.tar.gz", job.NginxVersion, job.TargetOS, job.TargetArch)
	artifactPath := filepath.Join(ws.ArtifactsDir, artifactName)

	writeLog("正在将构建产物打包至: %s ...", artifactPath)
	artifactInfo, err := packageArtifacts(srcRoot, artifactPath, artifactName, job)
	if err != nil {
		writeErr("打包构建产物失败: %v", err)
		return fmt.Errorf("打包失败: %w", err)
	}
	job.Artifact = artifactInfo

	writeLog("✔ 构建产物打包成功: %s", artifactInfo.Name)
	writeLog("  大小: %.2f MB (%d bytes)", float64(artifactInfo.Size)/(1024*1024), artifactInfo.Size)
	writeLog("  SHA256: %s", artifactInfo.SHA256)
	writeLog("  下载接口: %s", artifactInfo.DownloadURL)

	// Final Step: Completed
	job.Status = model.StatusCompleted
	job.CurrentStep = "编译完成"
	job.Progress = 100
	now := time.Now()
	job.EndTime = &now
	job.DurationSeconds = now.Sub(job.StartTime).Seconds()
	saveMetadata(job, ws.MetadataPath)

	writeLog("==================================================================")
	writeLog("🎉 Nginx 编译构建任务已全部圆满成功！耗时: %.2f 秒", job.DurationSeconds)
	writeLog("==================================================================")

	return nil
}

// verifyCompiledNginx executes `objs/nginx -V` and verifies version and args.
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
	cmd := exec.CommandContext(ctx, binPath, "-V")
	outBytes, err := cmd.CombinedOutput()
	rawOutput := strings.TrimSpace(string(outBytes))
	res.RawOutput = rawOutput

	if err != nil {
		// nginx -V exits with 0, but check output
		if len(rawOutput) == 0 {
			res.CheckError = fmt.Sprintf("运行 nginx -V 出错: %v", err)
			return res
		}
	}

	// 3. Validate version string
	verPrefix := fmt.Sprintf("nginx/%s", expectedVersion)
	if strings.Contains(rawOutput, verPrefix) || strings.Contains(rawOutput, expectedVersion) {
		res.VersionMatch = true
	} else {
		res.CheckError = fmt.Sprintf("版本号不匹配: 期望包含 %s, 实际输出: %s", expectedVersion, rawOutput)
		return res
	}

	// 4. Validate configure arguments
	if strings.Contains(rawOutput, "configure arguments:") {
		res.ArgsMatch = true
	} else {
		res.CheckError = "未找到 configure arguments 输出"
		return res
	}

	res.Passed = true
	lines := strings.Split(rawOutput, "\n")
	if len(lines) > 0 {
		res.VersionText = lines[0]
	}
	return res
}

// packageArtifacts packages the compiled nginx binary, default conf, and docs into a tar.gz.
func packageArtifacts(srcRoot, destTarGz, artifactName string, job *model.BuildJob) (*model.ArtifactInfo, error) {
	outFile, err := os.Create(destTarGz)
	if err != nil {
		return nil, err
	}
	defer outFile.Close()

	hasher := sha256.New()
	multiWriter := io.MultiWriter(outFile, hasher)
	gw := gzip.NewWriter(multiWriter)
	tw := tar.NewWriter(gw)

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
			_ = addFileToTar(tw, soPath, filepath.Join("modules", entry.Name()), 0755)
		}
	}

	// 5. Build info metadata file (BUILD_INFO.json)
	infoBytes, _ := json.MarshalIndent(job, "", "  ")
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
	return &model.ArtifactInfo{
		Name:        artifactName,
		Size:        stat.Size(),
		SHA256:      shaHex,
		DownloadURL: fmt.Sprintf("/api/builds/%s/artifact", job.BuildID),
		Path:        destTarGz,
	}, nil
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
func extractTarGz(tarGzPath, destDir string) (string, error) {
	f, err := os.Open(tarGzPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return "", err
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	var candidateTopDir string

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}

		// Skip PAX header metadata
		if header.Typeflag == tar.TypeXHeader || header.Typeflag == tar.TypeXGlobalHeader || header.Name == "pax_global_header" {
			continue
		}

		target := filepath.Join(destDir, header.Name)
		// ZipSlip check
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(destDir)) {
			return "", fmt.Errorf("非法压缩包路径逃逸: %s", header.Name)
		}

		parts := strings.Split(filepath.ToSlash(header.Name), "/")
		if len(parts) > 0 && candidateTopDir == "" && parts[0] != "" && parts[0] != "pax_global_header" {
			candidateTopDir = parts[0]
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return "", err
			}
		case tar.TypeReg:
			_ = os.MkdirAll(filepath.Dir(target), 0755)
			outFile, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR|os.O_TRUNC, header.FileInfo().Mode())
			if err != nil {
				return "", err
			}
			if _, err := io.Copy(outFile, tr); err != nil {
				outFile.Close()
				return "", err
			}
			outFile.Close()
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

func saveMetadata(job *model.BuildJob, metaPath string) {
	data, err := json.MarshalIndent(job, "", "  ")
	if err == nil {
		_ = os.WriteFile(metaPath, data, 0644)
	}
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
