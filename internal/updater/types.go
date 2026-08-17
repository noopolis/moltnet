package updater

import (
	"context"
	"time"
)

type Options struct {
	CheckOnly      bool
	CommandRunner  CommandRunner
	CurrentVersion string
	Detector       InstallDetector
	DryRun         bool
	LockPath       string
	LockStaleAfter time.Duration
	Platform       Platform
	ReleaseSource  ReleaseSource
	ServerProbe    ServerProbe
	ServerToken    string
	ServerTokenEnv string
	ServerURL      string
	// SourceCheckout is the ldflags-stamped source checkout path from the
	// running binary (main.sourceCheckout in cmd/moltnet), forwarded here so
	// Run can attempt a source-install update. Empty on a binary built
	// without the stamp, which degrades to the plain source-install refusal.
	SourceCheckout string
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
	// RefusalReason is the exact reason a real (mutating) run refused,
	// set whenever MutationRefused is true. It is deliberately kept out of
	// Warnings for a real run: main.go already prints the returned error
	// (this same text) to stderr, so also queuing it as a stdout warning
	// would print the identical sentence twice. --check/--dry-run never set
	// this — a refusal there is informational only and stays in Warnings,
	// since no error is returned to echo it.
	RefusalReason string
	Server        ServerInfo
	// SourceUpdate is set only when the resolved install is a source build
	// with a usable stamped checkout: it carries the git status a source
	// update inspected or acted on. Nil for release-tarball, container, and
	// unknown installs, and for source installs with no usable checkout.
	SourceUpdate    *SourceUpdatePlan
	TargetVersion   string
	Updated         bool
	UpdateAvailable bool
	Warnings        []string
}

type Platform struct {
	Arch string
	OS   string
}

type Install struct {
	Method            InstallMethod
	Path              string
	SelfUpdateAllowed bool
	// SourceCheckout is the source checkout path for a source-method
	// install, populated by Run from Options.SourceCheckout. Empty when the
	// install method is not source, or when the binary carries no stamp.
	SourceCheckout string
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

type ReleaseSourceMetadata struct {
	DownloadBaseURL string
	OwnerRepo       string
}

type ServerProbe interface {
	ProbeServer(ctx context.Context, request ServerProbeRequest) (ServerInfo, error)
}

type ServerProbeRequest struct {
	Token string
	URL   string
}

type ServerInfo struct {
	URL     string
	Version string
	Warning string
}
