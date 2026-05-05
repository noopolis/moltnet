package updater

import (
	"fmt"
	"runtime"
)

func CurrentPlatform() Platform {
	return Platform{OS: runtime.GOOS, Arch: runtime.GOARCH}
}

func (p Platform) WithDefaults() Platform {
	if p.OS == "" {
		p.OS = runtime.GOOS
	}
	if p.Arch == "" {
		p.Arch = runtime.GOARCH
	}
	return p
}

func AssetName(platform Platform) (string, error) {
	platform = platform.WithDefaults()
	if !supportedOS(platform.OS) {
		return "", fmt.Errorf("unsupported operating system %q for Moltnet release assets", platform.OS)
	}
	if !supportedArch(platform.Arch) {
		return "", fmt.Errorf("unsupported architecture %q for Moltnet release assets", platform.Arch)
	}
	return fmt.Sprintf("moltnet_%s_%s.tar.gz", platform.OS, platform.Arch), nil
}

func supportedOS(value string) bool {
	switch value {
	case "darwin", "linux":
		return true
	default:
		return false
	}
}

func supportedArch(value string) bool {
	switch value {
	case "amd64", "arm64":
		return true
	default:
		return false
	}
}
