package updater

import "testing"

func TestAssetName(t *testing.T) {
	assetName, err := AssetName(Platform{OS: "darwin", Arch: "arm64"})
	if err != nil {
		t.Fatalf("AssetName() error = %v", err)
	}
	if assetName != "moltnet_darwin_arm64.tar.gz" {
		t.Fatalf("unexpected asset name %q", assetName)
	}
}

func TestAssetNameRejectsUnsupportedPlatform(t *testing.T) {
	if _, err := AssetName(Platform{OS: "windows", Arch: "amd64"}); err == nil {
		t.Fatal("expected unsupported platform error")
	}
}
