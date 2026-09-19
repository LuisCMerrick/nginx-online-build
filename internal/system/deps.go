package system

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DepCategory represents functional grouping of compilation dependencies.
type DepCategory string

const (
	DepCategoryToolchain DepCategory = "toolchain" // gcc, make, perl, tar, etc.
	DepCategoryCoreLib   DepCategory = "core_lib"  // pcre2, openssl, zlib
	DepCategoryOptLib    DepCategory = "opt_lib"   // libxml2, libxslt, gd, geoip
)

// DependencyItem describes one compilation dependency required by Nginx.
type DependencyItem struct {
	ID          string      `json:"id"`           // e.g. "gcc", "openssl"
	Name        string      `json:"name"`         // Display name
	Category    DepCategory `json:"category"`     // toolchain | core_lib | opt_lib
	Required    bool        `json:"required"`     // true for essential build tools/libs
	Installed   bool        `json:"installed"`    // true if detected on host
	Version     string      `json:"version"`      // detected version if any
	PackageName string      `json:"package_name"` // distro-specific package name
	Description string      `json:"description"`
}

// SystemStatusResponse encapsulates host environment, distro, and dependency checks.
type SystemStatusResponse struct {
	Distro         DistroInfo       `json:"distro"`
	Dependencies   []DependencyItem `json:"dependencies"`
	AllInstalled   bool             `json:"all_installed"`
	MissingCount   int              `json:"missing_count"`
	InstallCommand string           `json:"install_command"`
	IsInstalling   bool             `json:"is_installing"`
}

// PackageMaps defines distro-specific package names for each package manager.
var PackageMaps = map[string]map[string]string{
	"apt": {
		"gcc":        "gcc",
		"g++":        "g++",
		"make":       "make",
		"perl":       "perl",
		"tar":        "tar",
		"curl":       "curl",
		"pcre2":      "libpcre2-dev",
		"openssl":    "libssl-dev",
		"zlib":       "zlib1g-dev",
		"libxml2":    "libxml2-dev",
		"libxslt":    "libxslt1-dev",
		"gd":         "libgd-dev",
		"geoip":      "libgeoip-dev",
	},
	"dnf": {
		"gcc":        "gcc",
		"g++":        "gcc-c++",
		"make":       "make",
		"perl":       "perl",
		"tar":        "tar",
		"curl":       "curl",
		"pcre2":      "pcre2-devel",
		"openssl":    "openssl-devel",
		"zlib":       "zlib-devel",
		"libxml2":    "libxml2-devel",
		"libxslt":    "libxslt-devel",
		"gd":         "gd-devel",
		"geoip":      "geoip-devel",
	},
	"yum": {
		"gcc":        "gcc",
		"g++":        "gcc-c++",
		"make":       "make",
		"perl":       "perl",
		"tar":        "tar",
		"curl":       "curl",
		"pcre2":      "pcre2-devel",
		"openssl":    "openssl-devel",
		"zlib":       "zlib-devel",
		"libxml2":    "libxml2-devel",
		"libxslt":    "libxslt-devel",
		"gd":         "gd-devel",
		"geoip":      "geoip-devel",
	},
	"apk": {
		"gcc":        "gcc",
		"g++":        "g++",
		"make":       "make",
		"perl":       "perl",
		"tar":        "tar",
		"curl":       "curl",
		"pcre2":      "pcre2-dev",
		"openssl":    "openssl-dev",
		"zlib":       "zlib-dev",
		"libxml2":    "libxml2-dev",
		"libxslt":    "libxslt-dev",
		"gd":         "gd-dev",
		"geoip":      "geoip-dev",
	},
	"pacman": {
		"gcc":        "gcc",
		"g++":        "gcc",
		"make":       "make",
		"perl":       "perl",
		"tar":        "tar",
		"curl":       "curl",
		"pcre2":      "pcre2",
		"openssl":    "openssl",
		"zlib":       "zlib",
		"libxml2":    "libxml2",
		"libxslt":    "libxslt",
		"gd":         "gd",
		"geoip":      "geoip",
	},
	"zypper": {
		"gcc":        "gcc",
		"g++":        "gcc-c++",
		"make":       "make",
		"perl":       "perl",
		"tar":        "tar",
		"curl":       "curl",
		"pcre2":      "pcre2-devel",
		"openssl":    "libopenssl-devel",
		"zlib":       "zlib-devel",
		"libxml2":    "libxml2-devel",
		"libxslt":    "libxslt-devel",
		"gd":         "gd-devel",
		"geoip":      "geoip-devel",
	},
}

// DepInstaller handles background package installation and live log broadcasting.
type DepInstaller struct {
	mu           sync.RWMutex
	isInstalling bool
	logLines     []string
	listeners    map[chan string]struct{}
}

var GlobalInstaller = &DepInstaller{
	listeners: make(map[chan string]struct{}),
}

// CheckDependencies performs live system inspection for Nginx toolchain and development headers.
func CheckDependencies() SystemStatusResponse {
	distro := DetectDistro()
	pkgMap := PackageMaps[distro.PackageManager]
	if pkgMap == nil {
		pkgMap = PackageMaps["apt"]
	}

	deps := []DependencyItem{
		// 1. Toolchain
		{
			ID:          "gcc",
			Name:        "C Compiler (gcc)",
			Category:    DepCategoryToolchain,
			Required:    true,
			Description: "Core C language compiler for Nginx binary compilation",
		},
		{
			ID:          "make",
			Name:        "Build Automation (make)",
			Category:    DepCategoryToolchain,
			Required:    true,
			Description: "Standard GNU Make toolchain for Nginx compilation",
		},
		{
			ID:          "perl",
			Name:        "Perl 5 Runtime",
			Category:    DepCategoryToolchain,
			Required:    true,
			Description: "Required for OpenSSL ./Configure and Nginx auto test scripts",
		},
		{
			ID:          "tar",
			Name:        "Tar Archive Tool",
			Category:    DepCategoryToolchain,
			Required:    true,
			Description: "Used to extract official source archives and package final builds",
		},
		// 2. Core Dev Libraries
		{
			ID:          "pcre2",
			Name:        "PCRE2 Development Headers",
			Category:    DepCategoryCoreLib,
			Required:    true,
			Description: "PCRE2 regular expression header files (pcre2.h) for routing & rewrite",
		},
		{
			ID:          "openssl",
			Name:        "OpenSSL Development Headers",
			Category:    DepCategoryCoreLib,
			Required:    true,
			Description: "SSL/TLS cryptography headers (openssl/ssl.h) for HTTPS & Stream SSL",
		},
		{
			ID:          "zlib",
			Name:        "zlib Compression Headers",
			Category:    DepCategoryCoreLib,
			Required:    true,
			Description: "Deflate compression headers (zlib.h) for Gzip modules",
		},
		// 3. Optional Module Libraries
		{
			ID:          "libxml2",
			Name:        "libxml2 Headers (Optional)",
			Category:    DepCategoryOptLib,
			Required:    false,
			Description: "XML parsing library required by --with-http_xslt_module",
		},
		{
			ID:          "libxslt",
			Name:        "libxslt Headers (Optional)",
			Category:    DepCategoryOptLib,
			Required:    false,
			Description: "XSLT transformation library required by --with-http_xslt_module",
		},
		{
			ID:          "gd",
			Name:        "GD Graphics Library (Optional)",
			Category:    DepCategoryOptLib,
			Required:    false,
			Description: "Graphics library required by --with-http_image_filter_module",
		},
		{
			ID:          "geoip",
			Name:        "GeoIP Library (Optional)",
			Category:    DepCategoryOptLib,
			Required:    false,
			Description: "MaxMind GeoIP library required by --with-http_geoip_module",
		},
	}

	missingPkgs := []string{}
	missingCount := 0

	for i := range deps {
		d := &deps[i]
		if pkgName, ok := pkgMap[d.ID]; ok {
			d.PackageName = pkgName
		} else {
			d.PackageName = d.ID
		}

		checkDependency(d)
		if !d.Installed && d.Required {
			missingCount++
			if d.PackageName != "" && !containsStr(missingPkgs, d.PackageName) {
				missingPkgs = append(missingPkgs, d.PackageName)
			}
		}
	}

	installCmd := BuildInstallCommand(distro.PackageManager, missingPkgs, distro.IsRoot)

	GlobalInstaller.mu.RLock()
	isInstalling := GlobalInstaller.isInstalling
	GlobalInstaller.mu.RUnlock()

	return SystemStatusResponse{
		Distro:         distro,
		Dependencies:   deps,
		AllInstalled:   missingCount == 0,
		MissingCount:   missingCount,
		InstallCommand: installCmd,
		IsInstalling:   isInstalling,
	}
}

func checkDependency(d *DependencyItem) {
	switch d.ID {
	case "gcc":
		if p, err := exec.LookPath("gcc"); err == nil {
			d.Installed = true
			d.Version = getCmdVersion(p, "-dumpversion")
		}
	case "make":
		if p, err := exec.LookPath("make"); err == nil {
			d.Installed = true
			d.Version = getCmdFirstLine(p, "--version")
		}
	case "perl":
		if p, err := exec.LookPath("perl"); err == nil {
			d.Installed = true
			d.Version = getCmdFirstLine(p, "-v")
		}
	case "tar":
		if _, err := exec.LookPath("tar"); err == nil {
			d.Installed = true
		}
	case "pcre2":
		d.Installed = checkHeaderExists("pcre2.h") || checkPkgConfig("libpcre2-8")
	case "openssl":
		d.Installed = checkHeaderExists("openssl/ssl.h") || checkPkgConfig("openssl")
	case "zlib":
		d.Installed = checkHeaderExists("zlib.h") || checkPkgConfig("zlib")
	case "libxml2":
		d.Installed = checkHeaderExists("libxml/parser.h") || checkHeaderExists("libxml2/libxml/parser.h") || checkPkgConfig("libxml-2.0")
	case "libxslt":
		d.Installed = checkHeaderExists("libxslt/xslt.h") || checkPkgConfig("libxslt")
	case "gd":
		d.Installed = checkHeaderExists("gd.h") || checkPkgConfig("gdlib")
	case "geoip":
		d.Installed = checkHeaderExists("GeoIP.h") || checkPkgConfig("geoip")
	}
}

func checkHeaderExists(header string) bool {
	includeDirs := []string{
		"/usr/include",
		"/usr/local/include",
		"/usr/include/x86_64-linux-gnu",
		"/usr/include/aarch64-linux-gnu",
		"/usr/include/arm-linux-gnueabihf",
	}

	for _, dir := range includeDirs {
		target := filepath.Join(dir, header)
		if _, err := os.Stat(target); err == nil {
			return true
		}
	}
	return false
}

func checkPkgConfig(pkg string) bool {
	if _, err := exec.LookPath("pkg-config"); err != nil {
		return false
	}
	cmd := exec.Command("pkg-config", "--exists", pkg)
	return cmd.Run() == nil
}

func getCmdVersion(path string, arg string) string {
	out, err := exec.Command(path, arg).Output()
	if err != nil {
		return "detected"
	}
	return strings.TrimSpace(string(out))
}

func getCmdFirstLine(path string, arg string) string {
	out, err := exec.Command(path, arg).Output()
	if err != nil {
		return "detected"
	}
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		text := strings.TrimSpace(scanner.Text())
		if text != "" {
			return text
		}
	}
	return "detected"
}

// BuildInstallCommand creates the shell command required to install packages for the given distro.
func BuildInstallCommand(pkgMgr string, pkgs []string, isRoot bool) string {
	if len(pkgs) == 0 {
		return ""
	}

	prefix := ""
	if !isRoot {
		prefix = "sudo "
	}

	joined := strings.Join(pkgs, " ")

	switch pkgMgr {
	case "apt":
		return fmt.Sprintf("%sapt-get update && %sapt-get install -y --no-install-recommends %s", prefix, prefix, joined)
	case "dnf":
		return fmt.Sprintf("%sdnf install -y %s", prefix, joined)
	case "yum":
		return fmt.Sprintf("%syum install -y %s", prefix, joined)
	case "apk":
		return fmt.Sprintf("%sapk add --no-cache %s", prefix, joined)
	case "pacman":
		return fmt.Sprintf("%spacman -Sy --noconfirm %s", prefix, joined)
	case "zypper":
		return fmt.Sprintf("%szypper install -y %s", prefix, joined)
	default:
		return fmt.Sprintf("# Please install packages: %s", joined)
	}
}

// InstallDependencies runs the installation asynchronously, broadcasting output via channel.
func (inst *DepInstaller) InstallDependencies() error {
	inst.mu.Lock()
	if inst.isInstalling {
		inst.mu.Unlock()
		return fmt.Errorf("已有正在执行的依赖安装任务，请稍候")
	}
	inst.isInstalling = true
	inst.logLines = []string{}
	inst.mu.Unlock()

	status := CheckDependencies()
	if status.AllInstalled {
		inst.mu.Lock()
		inst.isInstalling = false
		inst.mu.Unlock()
		return fmt.Errorf("所有核心依赖项均已就绪，无需重复安装")
	}

	if !status.Distro.CanInstall {
		inst.mu.Lock()
		inst.isInstalling = false
		inst.mu.Unlock()
		return fmt.Errorf("当前运行用户权限不足 (非 root 且无无密 sudo 权限)，无法自动安装。请手动在服务器执行: %s", status.InstallCommand)
	}

	go inst.runInstall(status.Distro.PackageManager, status.Dependencies, status.Distro.IsRoot)
	return nil
}

func (inst *DepInstaller) runInstall(pkgMgr string, deps []DependencyItem, isRoot bool) {
	defer func() {
		inst.mu.Lock()
		inst.isInstalling = false
		inst.mu.Unlock()
	}()

	var pkgs []string
	for _, d := range deps {
		if !d.Installed && d.PackageName != "" && !containsStr(pkgs, d.PackageName) {
			pkgs = append(pkgs, d.PackageName)
		}
	}

	if len(pkgs) == 0 {
		inst.broadcast("✔ 所有依赖项已存在，安装流程结束。")
		return
	}

	inst.broadcast(fmt.Sprintf("🚀 开始自动安装缺失依赖项 (包管理器: %s)...", pkgMgr))
	inst.broadcast(fmt.Sprintf("📦 待安装软件包: %s", strings.Join(pkgs, " ")))

	var cmd *exec.Cmd

	switch pkgMgr {
	case "apt":
		args := []string{"install", "-y", "--no-install-recommends"}
		args = append(args, pkgs...)
		// Run apt-get update first
		inst.broadcast("🔄 正在更新本地软件包索引 (apt-get update)...")
		upCmd := exec.Command("apt-get", "update")
		if !isRoot {
			upCmd = exec.Command("sudo", "apt-get", "update")
		}
		_ = inst.runCommandWithOutput(upCmd)

		cmd = exec.Command("apt-get", args...)
		if !isRoot {
			cmd = exec.Command("sudo", append([]string{"apt-get"}, args...)...)
		}

	case "dnf":
		args := append([]string{"install", "-y"}, pkgs...)
		cmd = exec.Command("dnf", args...)
		if !isRoot {
			cmd = exec.Command("sudo", append([]string{"dnf"}, args...)...)
		}

	case "yum":
		args := append([]string{"install", "-y"}, pkgs...)
		cmd = exec.Command("yum", args...)
		if !isRoot {
			cmd = exec.Command("sudo", append([]string{"yum"}, args...)...)
		}

	case "apk":
		args := append([]string{"add", "--no-cache"}, pkgs...)
		cmd = exec.Command("apk", args...)
		if !isRoot {
			cmd = exec.Command("sudo", append([]string{"apk"}, args...)...)
		}

	case "pacman":
		args := append([]string{"-Sy", "--noconfirm"}, pkgs...)
		cmd = exec.Command("pacman", args...)
		if !isRoot {
			cmd = exec.Command("sudo", append([]string{"pacman"}, args...)...)
		}

	case "zypper":
		args := append([]string{"install", "-y"}, pkgs...)
		cmd = exec.Command("zypper", args...)
		if !isRoot {
			cmd = exec.Command("sudo", append([]string{"zypper"}, args...)...)
		}

	default:
		inst.broadcast("❌ 未知或不受支持的包管理器，无法自动安装。")
		return
	}

	err := inst.runCommandWithOutput(cmd)
	if err != nil {
		inst.broadcast(fmt.Sprintf("❌ 依赖安装失败: %v", err))
	} else {
		inst.broadcast("🎉 依赖项安装完成！正在复核环境...")
		time.Sleep(500 * time.Millisecond)
		recheck := CheckDependencies()
		if recheck.AllInstalled {
			inst.broadcast("✔ 复核通过：所有 Nginx 核心编译依赖项已全部就绪！")
		} else {
			inst.broadcast(fmt.Sprintf("⚠️ 仍有 %d 项依赖未就绪，请检查安装日志。", recheck.MissingCount))
		}
	}
}

func (inst *DepInstaller) runCommandWithOutput(cmd *exec.Cmd) error {
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout // merge stderr into stdout

	if err := cmd.Start(); err != nil {
		return err
	}

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		inst.broadcast(line)
	}

	return cmd.Wait()
}

func (inst *DepInstaller) broadcast(msg string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()

	inst.logLines = append(inst.logLines, msg)
	for ch := range inst.listeners {
		select {
		case ch <- msg:
		default:
		}
	}
}

// Subscribe returns a channel receiving real-time install logs.
func (inst *DepInstaller) Subscribe() chan string {
	inst.mu.Lock()
	defer inst.mu.Unlock()

	ch := make(chan string, 100)
	inst.listeners[ch] = struct{}{}
	return ch
}

// Unsubscribe removes a log listener channel.
func (inst *DepInstaller) Unsubscribe(ch chan string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()

	delete(inst.listeners, ch)
	close(ch)
}

// GetRecentLogs returns current cached installation log buffer.
func (inst *DepInstaller) GetRecentLogs() []string {
	inst.mu.RLock()
	defer inst.mu.RUnlock()

	cp := make([]string, len(inst.logLines))
	copy(cp, inst.logLines)
	return cp
}

func containsStr(slice []string, val string) bool {
	for _, item := range slice {
		if item == val {
			return true
		}
	}
	return false
}
