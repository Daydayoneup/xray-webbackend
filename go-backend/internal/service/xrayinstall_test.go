package service

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func makeXrayZip(t *testing.T, files map[string]string) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, _ := zw.Create(name)
		w.Write([]byte(body))
	}
	zw.Close()
	return buf.Bytes()
}

func dgstFor(data []byte) []byte {
	sum := sha256.Sum256(data)
	return []byte("MD5= x\nSHA1= y\nSHA2-256= " + hex.EncodeToString(sum[:]) + "\nSHA2-512= z\n")
}

func TestInstallXrayZip(t *testing.T) {
	data := makeXrayZip(t, map[string]string{"xray": "bin", "geoip.dat": "ip", "geosite.dat": "site", "README.md": "x"})
	dir := filepath.Join(t.TempDir(), "bin")
	bin, err := InstallXrayZip(data, dgstFor(data), dir, "xray")
	if err != nil {
		t.Fatal(err)
	}
	if bin != filepath.Join(dir, "xray") {
		t.Fatalf("bin = %s", bin)
	}
	st, _ := os.Stat(bin)
	if st.Mode().Perm()&0100 == 0 {
		t.Fatal("xray should be executable")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "geosite.dat")); string(b) != "site" {
		t.Fatal("geosite.dat not extracted")
	}
	if _, err := os.Stat(filepath.Join(dir, "README.md")); err == nil {
		t.Fatal("unexpected file extracted")
	}
}

func TestInstallXrayZipChecksum(t *testing.T) {
	data := makeXrayZip(t, map[string]string{"xray": "bin"})
	bad := dgstFor([]byte("other"))
	if _, err := InstallXrayZip(data, bad, t.TempDir(), "xray"); err == nil || !strings.Contains(err.Error(), "校验失败") {
		t.Fatalf("want checksum error, got %v", err)
	}
	if _, err := InstallXrayZip(data, []byte("nothing"), t.TempDir(), "xray"); err == nil {
		t.Fatal("missing digest should fail")
	}
	noBin := makeXrayZip(t, map[string]string{"geoip.dat": "x"})
	if _, err := InstallXrayZip(noBin, dgstFor(noBin), t.TempDir(), "xray"); err == nil {
		t.Fatal("zip without xray should fail")
	}
}

func TestXrayReleaseAsset(t *testing.T) {
	cases := map[[2]string]string{
		{"linux", "amd64"}: "Xray-linux-64.zip", {"linux", "arm64"}: "Xray-linux-arm64-v8a.zip",
		{"linux", "arm"}: "Xray-linux-arm32-v7a.zip", {"darwin", "arm64"}: "Xray-macos-arm64-v8a.zip",
	}
	for in, want := range cases {
		if got, err := XrayReleaseAsset(in[0], in[1]); err != nil || got != want {
			t.Errorf("%v: got %s %v", in, got, err)
		}
	}
	if _, err := XrayReleaseAsset("plan9", "amd64"); err == nil {
		t.Error("plan9 should be unsupported")
	}
}

func TestDownloadXrayGHProxy(t *testing.T) {
	data := makeXrayZip(t, map[string]string{XrayBinName("linux"): "bin", "xray.exe": "bin"})
	var urls []string
	get := func(u string) ([]byte, error) {
		urls = append(urls, u)
		if strings.HasSuffix(u, ".dgst") {
			return dgstFor(data), nil
		}
		return data, nil
	}
	if _, err := DownloadXray(get, "https://mirror.example/", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(urls[0], "https://mirror.example/https://github.com/XTLS/Xray-core/releases/latest/download/Xray-") {
		t.Fatalf("url = %s", urls[0])
	}
}
