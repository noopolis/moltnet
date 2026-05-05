package updater

import "context"

type Options struct {
	CheckOnly      bool
	CurrentVersion string
	Detector       InstallDetector
	DryRun         bool
	Platform       Platform
	ReleaseSource  ReleaseSource
	ServerProbe    ServerProbe
	ServerURL      string
	TargetVersion  string
	TempDir        string
	Yes            bool
}

type Result struct {
	AssetName         string
	BackupPath        string
	ChecksumAvailable bool
	CheckOnly         bool
	CurrentVersion    string
	DryRun            bool
	Install           Install
	LatestVersion     string
	MutationRefused   bool
	Server            ServerInfo
	TargetVersion     string
	Updated           bool
	UpdateAvailable   bool
	Warnings          []string
}

type Platform struct {
	Arch string
	OS   string
}

type Install struct {
	Method            InstallMethod
	Path              string
	SelfUpdateAllowed bool
}

type InstallMethod string

const (
	InstallMethodReleaseTarball InstallMethod = "release-tarball"
	InstallMethodSource         InstallMethod = "source"
	InstallMethodContainer      InstallMethod = "container"
	InstallMethodUnknown        InstallMethod = "unknown"
)

type InstallDetector interface {
	DetectInstall(ctx context.Context, currentVersion string) (Install, error)
}

type ReleaseSource interface {
	LatestVersion(ctx context.Context) (string, error)
	Archive(ctx context.Context, version string, assetName string) ([]byte, error)
	Checksums(ctx context.Context, version string) ([]byte, error)
}

type ServerProbe interface {
	ProbeServer(ctx context.Context, serverURL string) (ServerInfo, error)
}

type ServerInfo struct {
	URL     string
	Version string
	Warning string
}
