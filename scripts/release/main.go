// Release tooling uses only the standard library; run from the repository root.
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var targets = []string{"linux_amd64", "linux_arm64", "darwin_amd64", "darwin_arm64"}
var members = []string{"feeder", "LICENSE", "README.md", "BUILD.json"}

func validVersion(v string) bool {
	return regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-rc\.[1-9][0-9]*)?$`).MatchString(v)
}
func asset(v, t string) string { return "feeder_" + v + "_" + t + ".tar.gz" }
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "release:", err)
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) < 2 {
		return errors.New("usage: go run ./scripts/release build|validate|formula --version vX.Y.Z[-rc.N] --out /absolute/directory")
	}
	f := flag.NewFlagSet("release", flag.ContinueOnError)
	v := f.String("version", "", "release version (required)")
	out := f.String("out", "", "absolute artifact directory (build requires a new directory)")
	if err := f.Parse(os.Args[2:]); err != nil {
		return err
	}
	if f.NArg() != 0 || !validVersion(*v) || !filepath.IsAbs(*out) {
		return errors.New("supply valid --version and absolute --out, with no extra arguments")
	}
	switch os.Args[1] {
	case "build":
		return build(*out, *v)
	case "validate":
		return validate(*out, *v)
	case "formula":
		if err := validate(*out, *v); err != nil {
			return err
		}
		return formula(os.Stdout, *out, *v)
	default:
		return errors.New("unknown operation")
	}
}

// Refuse any existing output, including a source/state directory or symlink.
func newOutput(dir string) error {
	if !filepath.IsAbs(dir) {
		return errors.New("output must be absolute")
	}
	return os.Mkdir(dir, 0700)
}
func command(name string, args ...string) (string, error) {
	b, e := exec.Command(name, args...).Output()
	return strings.TrimSpace(string(b)), e
}
func build(out, v string) error {
	commit, err := command("git", "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	dirty, err := command("git", "status", "--porcelain", "--untracked-files=normal")
	if err != nil {
		return err
	}
	if dirty != "" {
		return errors.New("build requires a clean checkout (commit scoped changes first)")
	}
	toolchain, err := command("go", "version")
	if err != nil {
		return err
	}
	if err = newOutput(out); err != nil {
		return err
	}
	stage, err := os.MkdirTemp("", "feeder-package-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	for _, name := range []string{"LICENSE", "README.md"} {
		b, e := os.ReadFile(name)
		if e != nil {
			return e
		}
		if name == "README.md" {
			b = append([]byte("Documentation snapshot (follow repository links online): https://github.com/ContractorKeith/feeder/blob/"+commit+"/README.md\n\n"), b...)
		}
		if e = os.WriteFile(filepath.Join(stage, name), b, 0644); e != nil {
			return e
		}
	}
	sums := ""
	for _, target := range targets {
		parts := strings.Split(target, "_")
		c := exec.Command("go", "build", "-trimpath", "-buildvcs=false", "-ldflags=-s -w -buildid= -X main.version="+v+"+"+commit, "-o", filepath.Join(stage, "feeder"), "./cmd/feeder")
		c.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+parts[0], "GOARCH="+parts[1], "GOAMD64=v1", "GOARM64=v8.0", "GOFLAGS=")
		c.Stdout = os.Stderr
		c.Stderr = os.Stderr
		if err = c.Run(); err != nil {
			return err
		}
		metadata := map[string]string{"version": v, "commit": commit, "target": target, "go": toolchain, "cgo_enabled": "0"}
		b, e := json.MarshalIndent(metadata, "", "  ")
		if e != nil {
			return e
		}
		if e = os.WriteFile(filepath.Join(stage, "BUILD.json"), append(b, '\n'), 0644); e != nil {
			return e
		}
		name := asset(v, target)
		if err = archive(filepath.Join(out, name), stage); err != nil {
			return err
		}
		hash, e := fileHash(filepath.Join(out, name))
		if e != nil {
			return e
		}
		sums += hash + "  " + name + "\n"
	}
	if err = os.WriteFile(filepath.Join(out, "SHA256SUMS"), []byte(sums), 0644); err != nil {
		return err
	}
	return validate(out, v)
}
func archive(path, stage string) (err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer func() {
		if e := f.Close(); err == nil {
			err = e
		}
	}()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, name := range members {
		b, e := os.ReadFile(filepath.Join(stage, name))
		if e != nil {
			return e
		}
		mode := int64(0644)
		if name == "feeder" {
			mode = 0755
		}
		h := &tar.Header{Name: name, Mode: mode, Size: int64(len(b)), ModTime: time.Unix(0, 0), Format: tar.FormatUSTAR}
		if e = tw.WriteHeader(h); e != nil {
			return e
		}
		if _, e = tw.Write(b); e != nil {
			return e
		}
	}
	if err = tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}
func fileHash(path string) (string, error) {
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return "", e
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func validate(dir, v string) error {
	b, err := os.ReadFile(filepath.Join(dir, "SHA256SUMS"))
	if err != nil {
		return err
	}
	lines := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	if len(lines) != len(targets) {
		return errors.New("expected exactly four checksum rows")
	}
	for i, target := range targets {
		name := asset(v, target)
		hash, e := fileHash(filepath.Join(dir, name))
		if e != nil {
			return e
		}
		if lines[i] != hash+"  "+name {
			return fmt.Errorf("checksum mismatch: %s", name)
		}
		if e = validateArchive(filepath.Join(dir, name)); e != nil {
			return fmt.Errorf("%s: %w", name, e)
		}
		if e = validateBuild(filepath.Join(dir, name), v, target); e != nil {
			return fmt.Errorf("%s: %w", name, e)
		}
	}
	return nil
}
func validateArchive(path string) error {
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	gz, e := gzip.NewReader(f)
	if e != nil {
		return e
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for _, name := range members {
		h, e := tr.Next()
		if e != nil {
			return e
		}
		mode := int64(0644)
		if name == "feeder" {
			mode = 0755
		}
		if h.Name != name || h.Typeflag != tar.TypeReg || h.Mode != mode || h.Size == 0 {
			return errors.New("invalid archive layout or mode")
		}
		if _, e = io.Copy(io.Discard, tr); e != nil {
			return e
		}
	}
	if _, e = tr.Next(); e != io.EOF {
		return errors.New("extra archive members")
	}
	// Drain gzip to verify its trailer checksum as well as the tar stream.
	if _, e = io.Copy(io.Discard, gz); e != nil {
		return e
	}
	return nil
}
func formula(w io.Writer, dir, v string) error {
	fmt.Fprintf(w, "class Feeder < Formula\n  desc \"Personal source-linked feed digests\"\n  homepage \"https://github.com/ContractorKeith/feeder\"\n  version %q\n  license \"MIT\"\n", strings.TrimPrefix(v, "v"))
	for _, osName := range []string{"darwin", "linux"} {
		block := "on_macos"
		if osName == "linux" {
			block = "on_linux"
		}
		fmt.Fprintf(w, "\n  %s do\n", block)
		for _, arch := range []string{"arm64", "amd64"} {
			condition := "Hardware::CPU.arm?"
			if arch == "amd64" {
				condition = "Hardware::CPU.intel?"
			}
			name := asset(v, osName+"_"+arch)
			hash, e := fileHash(filepath.Join(dir, name))
			if e != nil {
				return e
			}
			fmt.Fprintf(w, "    if %s\n      url %q\n      sha256 %q\n    end\n", condition, "https://github.com/ContractorKeith/feeder/releases/download/"+v+"/"+name, hash)
		}
		fmt.Fprintln(w, "  end")
	}
	fmt.Fprintln(w, "\n  def install\n    bin.install \"feeder\"\n    doc.install \"BUILD.json\", \"README.md\"\n  end\n\n  test do\n    assert_match version.to_s, shell_output(\"#{bin}/feeder --home #{testpath}/state version\")\n    system bin/\"feeder\", \"--home\", testpath/\"state\", \"init\", \"--timezone\", \"UTC\"\n  end\nend")
	return nil
}

// Verify actual embedded Go settings and metadata without executing foreign code.
func validateBuild(path, version, target string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var binary []byte
	metadata := map[string]string{}
	for {
		h, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return e
		}
		if h.Name == "feeder" {
			binary, e = io.ReadAll(io.LimitReader(tr, 128<<20))
			if e != nil {
				return e
			}
		}
		if h.Name == "BUILD.json" {
			if e = json.NewDecoder(io.LimitReader(tr, 8192)).Decode(&metadata); e != nil {
				return e
			}
		}
	}
	if metadata["version"] != version || metadata["target"] != target || metadata["cgo_enabled"] != "0" || !regexp.MustCompile(`^[a-f0-9]{40}$`).MatchString(metadata["commit"]) || metadata["go"] == "" {
		return errors.New("invalid build metadata")
	}
	info, err := buildinfo.Read(bytes.NewReader(binary))
	if err != nil {
		return err
	}
	settings := map[string]string{}
	for _, setting := range info.Settings {
		settings[setting.Key] = setting.Value
	}
	parts := strings.Split(target, "_")
	if settings["CGO_ENABLED"] != "0" || settings["GOOS"] != parts[0] || settings["GOARCH"] != parts[1] || !bytes.Contains(binary, []byte(version+"+"+metadata["commit"])) {
		return errors.New("binary build settings do not match metadata")
	}
	return nil
}
