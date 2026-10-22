package update

import (
	"testing"
)

func TestVersionGreater(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"v1.9.2", "v1.9.1", true},
		{"1.10.0", "1.9.9", true},
		{"v1.9.2", "v1.9.2", false},
		{"1.9.1", "1.9.2", false},
		{"2.0.0", "1.9.9", true},
		{"1.9.0-rc1", "1.8.0", true},
		{"1.9.0-rc1", "1.9.0-rc2", false},
		{"1.9.0-rc2", "1.9.0-rc1", true},
		{"1.9.0", "1.9.0-rc2", true}, // release outranks prerelease
		{"1.9.0-rc1", "1.9.0", false},
		{"1.9.0+build5", "1.9.0", false}, // build metadata ignored
	}
	for _, c := range cases {
		if got := versionGreater(c.latest, c.current); got != c.want {
			t.Errorf("versionGreater(%q, %q) = %v, want %v", c.latest, c.current, got, c.want)
		}
	}
}

func TestShouldUpdate(t *testing.T) {
	if !shouldUpdate("dev", "v1.9.2") {
		t.Error("dev builds should always report an update")
	}
	if shouldUpdate("v1.9.2", "dev") {
		t.Error("non-dev build newer than dev should not report an update")
	}
	if shouldUpdate("v1.9.2", "v1.9.2") {
		t.Error("same version should not report an update")
	}
}

func TestParseChecksums(t *testing.T) {
	data := []byte("" +
		"aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899  uniterm-linux-amd64-v1.9.2.tar.gz\n" +
		"ffeeddccbbaa00112233445566778899ffeeddccbbaa00112233445566778899 *uniterm-windows-amd64-portable-v1.9.2.zip\n" +
		"not-a-hash file.txt\n")
	sums := parseChecksums(data)
	if got := sums["uniterm-linux-amd64-v1.9.2.tar.gz"]; got != "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899" {
		t.Errorf("unexpected checksum: %q", got)
	}
	if got := sums["uniterm-windows-amd64-portable-v1.9.2.zip"]; got != "ffeeddccbbaa00112233445566778899ffeeddccbbaa00112233445566778899" {
		t.Errorf("star-prefixed name not parsed: %q", got)
	}
	if _, ok := sums["file.txt"]; ok {
		t.Error("invalid hash line should be skipped")
	}
}

func TestClassifyAsset(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"uniterm-windows-amd64-installer-v1.9.2.exe", "installer"},
		{"uniterm-windows-arm64-portable-v1.9.2.zip", "portable"},
		{"uniterm-darwin-arm64-v1.9.2.zip", "portable"},
		{"uniterm-linux-amd64-v1.9.2.tar.gz", "binary-tar.gz"},
		{"uniterm-darwin-arm64-v1.9.2.dmg", "other"},
		{"uniterm-linux-amd64-v1.9.2.deb", "other"},
		{"uniterm-linux-amd64-v1.9.2.rpm", "other"},
	}
	for _, c := range cases {
		if got := classifyAsset(c.name); got != c.want {
			t.Errorf("classifyAsset(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestAssetMatchesPlatform(t *testing.T) {
	cases := []struct {
		os, arch, name string
		want           bool
	}{
		{"darwin", "arm64", "uniterm-darwin-arm64-v1.9.2.zip", true},
		{"darwin", "arm64", "uniterm-darwin-amd64-v1.9.2.zip", false},
		{"linux", "amd64", "uniterm-linux-amd64-v1.9.2.tar.gz", true},
		{"linux", "arm64", "uniterm-linux-amd64-v1.9.2.tar.gz", false},
		{"windows", "amd64", "uniterm-windows-amd64-portable-v1.9.2.zip", true},
		{"windows", "arm64", "uniterm-windows-amd64-portable-v1.9.2.zip", false},
		{"freebsd", "amd64", "uniterm-linux-amd64-v1.9.2.tar.gz", false},
		// The uniterm- prefix anchors the match; a name that merely contains
		// "<os>-<arch>-" must not match.
		{"windows", "amd64", "other-uniterm-windows-amd64-portable-v1.9.2.zip", false},
		{"windows", "amd64", "checksums.txt", false},
	}
	for _, c := range cases {
		if got := assetMatchesPlatform(c.os, c.arch, c.name); got != c.want {
			t.Errorf("assetMatchesPlatform(%q, %q, %q) = %v, want %v", c.os, c.arch, c.name, got, c.want)
		}
	}
}

func TestCandidatesForOrdering(t *testing.T) {
	const base = "https://github.com/r1ckcs/uniterm/releases/download/v1.9.2/"
	rel := &release{Assets: []releaseAsset{
		{Name: "uniterm-windows-amd64-portable-v1.9.2.zip", BrowserDownloadURL: base + "uniterm-windows-amd64-portable-v1.9.2.zip"},
		{Name: "uniterm-windows-amd64-installer-v1.9.2.exe", BrowserDownloadURL: base + "uniterm-windows-amd64-installer-v1.9.2.exe"},
		{Name: "uniterm-darwin-amd64-v1.9.2.dmg", BrowserDownloadURL: base + "x.dmg"},
	}}
	sums := map[string]string{
		"uniterm-windows-amd64-installer-v1.9.2.exe": "hash1",
		"uniterm-windows-amd64-portable-v1.9.2.zip":  "hash2",
	}

	// Installer channel: installer kind first, then portable.
	got := candidatesFor("windows", "amd64", ChannelInstaller, rel, sums)
	if len(got) != 2 {
		t.Fatalf("want 2 candidates, got %d: %+v", len(got), got)
	}
	if got[0].Kind() != "installer" || got[0].SHA256 != "hash1" || got[0].Source != SourceGitHub {
		t.Errorf("first candidate should be the installer with its hash, got %+v", got[0])
	}
	if got[1].Kind() != "portable" {
		t.Errorf("second candidate should be portable, got %+v", got[1])
	}
	// Portable channel: no installer candidates at all.
	got = candidatesFor("windows", "amd64", ChannelPortable, rel, sums)
	if len(got) != 1 || got[0].Kind() != "portable" {
		t.Fatalf("portable channel must only return portable assets: %+v", got)
	}
	// darwin: the .zip is the update payload (dmg is "other").
	rel.Assets = append(rel.Assets,
		releaseAsset{Name: "uniterm-darwin-amd64-v1.9.2.zip", BrowserDownloadURL: base + "uniterm-darwin-amd64-v1.9.2.zip"})
	sums["uniterm-darwin-amd64-v1.9.2.zip"] = "hash3"
	got = candidatesFor("darwin", "amd64", ChannelPortable, rel, sums)
	if len(got) != 1 || got[0].Name != "uniterm-darwin-amd64-v1.9.2.zip" {
		t.Errorf("darwin portable should match only the .zip asset, got %+v", got)
	}
	// linux: the bare-binary tar.gz is the update payload (deb/rpm are "other").
	rel.Assets = append(rel.Assets,
		releaseAsset{Name: "uniterm-linux-amd64-v1.9.2.tar.gz", BrowserDownloadURL: base + "uniterm-linux-amd64-v1.9.2.tar.gz"},
		releaseAsset{Name: "uniterm-linux-amd64-v1.9.2.deb", BrowserDownloadURL: base + "linux.deb"})
	sums["uniterm-linux-amd64-v1.9.2.tar.gz"] = "hash4"
	got = candidatesFor("linux", "amd64", ChannelPortable, rel, sums)
	if len(got) != 1 || got[0].Name != "uniterm-linux-amd64-v1.9.2.tar.gz" {
		t.Errorf("linux portable should match only the .tar.gz asset, got %+v", got)
	}
}

func TestCandidatesForDropsUnverifiedAssets(t *testing.T) {
	rel := &release{Assets: []releaseAsset{
		// No signed checksum.
		{Name: "uniterm-linux-amd64-v9.tar.gz", BrowserDownloadURL: "https://github.com/r1ckcs/uniterm/releases/download/v9/uniterm-linux-amd64-v9.tar.gz"},
		// Hashed, but served from the upstream repository.
		{Name: "uniterm-linux-amd64-v9b.tar.gz", BrowserDownloadURL: "https://github.com/ys-ll/uniterm/releases/download/v9/uniterm-linux-amd64-v9b.tar.gz"},
		// Hashed, but served from the old Gitee mirror.
		{Name: "uniterm-linux-amd64-v9c.tar.gz", BrowserDownloadURL: "https://gitee.com/ys-l/uniterm/releases/download/v9/uniterm-linux-amd64-v9c.tar.gz"},
	}}
	sums := map[string]string{"uniterm-linux-amd64-v9b.tar.gz": "h", "uniterm-linux-amd64-v9c.tar.gz": "h"}
	if got := candidatesFor("linux", "amd64", ChannelPortable, rel, sums); len(got) != 0 {
		t.Fatalf("unverified or off-repo assets must not be offered: %+v", got)
	}
}

// Kind mirrors classifyAsset for test assertions.
func (a UpdateAsset) Kind() string { return classifyAsset(a.Name) }
