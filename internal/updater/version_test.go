package updater

import "testing"

func TestCompareVersionsToleratesVPrefix(t *testing.T) {
	comparison, err := CompareVersions("v0.1.10", "0.1.2")
	if err != nil {
		t.Fatalf("CompareVersions() error = %v", err)
	}
	if comparison <= 0 {
		t.Fatalf("expected v0.1.10 to be greater than 0.1.2, got %d", comparison)
	}
}

func TestCompareVersionsHandlesPrerelease(t *testing.T) {
	comparison, err := CompareVersions("0.2.0-rc.1", "v0.2.0")
	if err != nil {
		t.Fatalf("CompareVersions() error = %v", err)
	}
	if comparison >= 0 {
		t.Fatalf("expected prerelease to compare lower than release, got %d", comparison)
	}
}

func TestIsDevelopmentVersion(t *testing.T) {
	if !IsDevelopmentVersion("0.0.0-dev") {
		t.Fatal("expected 0.0.0-dev to be a development version")
	}
	if IsDevelopmentVersion("v0.2.0") {
		t.Fatal("expected v0.2.0 to be a release version")
	}
}
