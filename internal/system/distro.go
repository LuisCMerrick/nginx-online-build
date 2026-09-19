package system

import (
	"bufio"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// DistroInfo contains detected Linux distribution and package manager metadata.
type DistroInfo struct {
	OS             string `json:"os"`              // "linux", "darwin", etc.
	Arch           string `json:"arch"`            // "amd64", "arm64", etc.
	DistroID       string `json:"distro_id"`       // "debian", "ubuntu", "centos", "alpine", etc.
	DistroName     string `json:"distro_name"`     // "Debian GNU/Linux 13 (trixie)"
	VersionID      string `json:"version_id"`      // "13", "24.04", etc.
	PackageManager string `json:"package_manager"` // "apt", "dnf", "yum", "apk", "pacman", "zypper", "unknown"
	IsRoot         bool   `json:"is_root"`
	HasSudo        bool   `json:"has_sudo"`
	CanInstall     bool   `json:"can_install"`
}

// DetectDistro inspects /etc/os-release and available tools to identify the Linux distribution.
func DetectDistro() DistroInfo {
	info := DistroInfo{
		OS:         runtime.GOOS,
		Arch:       runtime.GOARCH,
		DistroID:   "unknown",
		DistroName: runtime.GOOS,
		IsRoot:     os.Geteuid() == 0,
	}

	// Check if sudo is available without password
	if !info.IsRoot {
		if _, err := exec.LookPath("sudo"); err == nil {
			cmd := exec.Command("sudo", "-n", "true")
			if err := cmd.Run(); err == nil {
				info.HasSudo = true
			}
		}
	}

	info.CanInstall = info.IsRoot || info.HasSudo

	if info.OS != "linux" {
		return info
	}

	// Parse /etc/os-release
	f, err := os.Open("/etc/os-release")
	if err == nil {
		defer f.Close()
		scanner := bufio.NewScanner(f)
		data := make(map[string]string)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				val := strings.Trim(parts[1], `"'`)
				data[parts[0]] = val
			}
		}

		if name, ok := data["PRETTY_NAME"]; ok && name != "" {
			info.DistroName = name
		} else if name, ok := data["NAME"]; ok && name != "" {
			info.DistroName = name
		}

		if id, ok := data["ID"]; ok && id != "" {
			info.DistroID = strings.ToLower(id)
		}

		if ver, ok := data["VERSION_ID"]; ok {
			info.VersionID = ver
		}

		// Also check ID_LIKE for derivatives
		idLike := strings.ToLower(data["ID_LIKE"])

		// Detect package manager based on ID / ID_LIKE
		switch {
		case matchesDistro(info.DistroID, idLike, "debian", "ubuntu", "kali", "raspbian", "linuxmint", "deepin", "armbian"):
			if checkTool("apt-get") {
				info.PackageManager = "apt"
			}
		case matchesDistro(info.DistroID, idLike, "fedora", "rhel", "centos", "rocky", "almalinux", "ol", "amzn"):
			if checkTool("dnf") {
				info.PackageManager = "dnf"
			} else if checkTool("yum") {
				info.PackageManager = "yum"
			}
		case matchesDistro(info.DistroID, idLike, "alpine"):
			if checkTool("apk") {
				info.PackageManager = "apk"
			}
		case matchesDistro(info.DistroID, idLike, "arch", "manjaro", "endeavouros"):
			if checkTool("pacman") {
				info.PackageManager = "pacman"
			}
		case matchesDistro(info.DistroID, idLike, "suse", "opensuse", "opensuse-leap", "opensuse-tumbleweed", "sles"):
			if checkTool("zypper") {
				info.PackageManager = "zypper"
			}
		}
	}

	// Fallback detection by available package manager binary if not detected yet
	if info.PackageManager == "" || info.PackageManager == "unknown" {
		switch {
		case checkTool("apt-get"):
			info.PackageManager = "apt"
			if info.DistroID == "unknown" {
				info.DistroID = "debian"
				info.DistroName = "Debian / Ubuntu compatible"
			}
		case checkTool("dnf"):
			info.PackageManager = "dnf"
			if info.DistroID == "unknown" {
				info.DistroID = "rhel"
				info.DistroName = "RHEL / Fedora compatible"
			}
		case checkTool("yum"):
			info.PackageManager = "yum"
			if info.DistroID == "unknown" {
				info.DistroID = "rhel"
				info.DistroName = "CentOS / RHEL compatible"
			}
		case checkTool("apk"):
			info.PackageManager = "apk"
			if info.DistroID == "unknown" {
				info.DistroID = "alpine"
				info.DistroName = "Alpine Linux"
			}
		case checkTool("pacman"):
			info.PackageManager = "pacman"
			if info.DistroID == "unknown" {
				info.DistroID = "arch"
				info.DistroName = "Arch Linux"
			}
		case checkTool("zypper"):
			info.PackageManager = "zypper"
			if info.DistroID == "unknown" {
				info.DistroID = "opensuse"
				info.DistroName = "openSUSE / SLES"
			}
		default:
			info.PackageManager = "unknown"
		}
	}

	return info
}

func matchesDistro(id string, idLike string, candidates ...string) bool {
	for _, c := range candidates {
		if id == c || strings.Contains(idLike, c) {
			return true
		}
	}
	return false
}

func checkTool(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
