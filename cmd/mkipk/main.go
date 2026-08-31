package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const version = "1.0.6-1"

type target struct {
	Folder  string
	GOARCH  string
	GOARM   string
	IPKArch string
}

var targets = []target{
	{Folder: "x86_64", GOARCH: "amd64", IPKArch: "x86_64"},
	{Folder: "i386", GOARCH: "386", IPKArch: "i386_pentium4"},
	{Folder: "armv7", GOARCH: "arm", GOARM: "7", IPKArch: "arm_cortex-a7_neon-vfpv4"},
	{Folder: "aarch64", GOARCH: "arm64", IPKArch: "aarch64_generic"},
}

func main() {
	root := repoRoot()
	dist := filepath.Join(root, "dist")
	buildDir := filepath.Join(dist, "build")
	must(os.MkdirAll(buildDir, 0755))

	for _, t := range targets {
		dir := filepath.Join(dist, t.Folder)
		must(os.MkdirAll(dir, 0755))
		bin := filepath.Join(buildDir, "taosocks-"+t.IPKArch)
		must(buildBinary(root, bin, t))
		out := filepath.Join(dir, fmt.Sprintf("tao-socks_%s_%s.ipk", version, t.IPKArch))
		must(packMainIPK(root, bin, t.IPKArch, out))
		fmt.Printf("built %s (%d bytes)\n", out, sizeOf(out))
	}

	fmt.Println("done")
}

func repoRoot() string {
	wd, err := os.Getwd()
	must(err)
	if _, err := os.Stat(filepath.Join(wd, "go.mod")); err == nil {
		return wd
	}
	exe, err := os.Executable()
	must(err)
	return filepath.Clean(filepath.Join(filepath.Dir(exe), "..", ".."))
}

func buildBinary(root, dest string, t target) error {
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags=-s -w", "-o", dest)
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"CGO_ENABLED=0",
		"GOOS=linux",
		"GOARCH="+t.GOARCH,
	)
	if t.GOARM != "" {
		cmd.Env = append(cmd.Env, "GOARM="+t.GOARM)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	fmt.Printf("+ GOARCH=%s GOARM=%s go build -o %s\n", t.GOARCH, t.GOARM, dest)
	return cmd.Run()
}

func packMainIPK(root, binary, arch, out string) error {
	initScript := unixBytes(filepath.Join(root, "openwrt", "files", "taosocks.init"))
	defaults := unixBytes(filepath.Join(root, "openwrt", "files", "taosocks.defaults"))
	uciDefault := unixBytes(filepath.Join(root, "openwrt", "files", "taosocks.uci-defaults"))
	uciConf := unixBytes(filepath.Join(root, "openwrt", "files", "taosocks.config"))
	binData, err := os.ReadFile(binary)
	if err != nil {
		return err
	}
	controller := unixBytes(filepath.Join(root, "openwrt", "luci", "luasrc", "controller", "taosocks.lua"))
	cbi := unixBytes(filepath.Join(root, "openwrt", "luci", "luasrc", "model", "cbi", "taosocks.lua"))
	acl := unixBytes(filepath.Join(root, "openwrt", "luci", "root", "usr", "share", "rpcd", "acl.d", "luci-app-taosocks.json"))
	ensureCfg := unixBytes(filepath.Join(root, "openwrt", "files", "ensure-config-file.sh"))
	dataTar, err := makeTarGZ(dataEntries([]tarEntry{
		{Name: "./usr/bin/taosocks", Body: binData, Mode: 0755},
		{Name: "./etc/init.d/taosocks", Body: initScript, Mode: 0755},
		{Name: "./etc/uci-defaults/99-taosocks", Body: uciDefault, Mode: 0755},
		{Name: "./usr/lib/taosocks/ensure-uci.sh", Body: defaults, Mode: 0755},
		{Name: "./usr/lib/taosocks/ensure-config-file.sh", Body: ensureCfg, Mode: 0755},
		{Name: "./usr/share/taosocks/taosocks.config", Body: uciConf, Mode: 0644},
		{Name: "./usr/lib/lua/luci/controller/taosocks.lua", Body: controller, Mode: 0644},
		{Name: "./usr/lib/lua/luci/model/cbi/taosocks.lua", Body: cbi, Mode: 0644},
		{Name: "./usr/share/rpcd/acl.d/luci-app-taosocks.json", Body: acl, Mode: 0644},
	}))
	if err != nil {
		return err
	}
	installed := (len(binData) + len(initScript) + len(defaults) + len(uciDefault) + len(ensureCfg) + len(uciConf) + len(controller) + len(cbi) + len(acl) + 1023) / 1024
	control := controlFile(unixBytes(filepath.Join(root, "openwrt", "ipk", "control")), arch, installed)
	controlTar, err := makeTarGZ([]tarEntry{
		{Name: "control", Body: control, Mode: 0644},
		{Name: "preinst", Body: unixBytes(filepath.Join(root, "openwrt", "files", "ensure-config-file.sh")), Mode: 0755},
		{Name: "postinst", Body: unixBytes(filepath.Join(root, "openwrt", "ipk", "postinst")), Mode: 0755},
		{Name: "prerm", Body: unixBytes(filepath.Join(root, "openwrt", "ipk", "prerm")), Mode: 0755},
		{Name: "postrm", Body: unixBytes(filepath.Join(root, "openwrt", "ipk", "postrm")), Mode: 0755},
	})
	if err != nil {
		return err
	}
	return writeOpenWrtIPK(out, []byte("2.0\n"), controlTar, dataTar)
}

func dataEntries(files []tarEntry) []tarEntry {
	seen := map[string]bool{".": true}
	var dirs []tarEntry
	for _, f := range files {
		parts := strings.Split(strings.TrimPrefix(filepath.ToSlash(f.Name), "./"), "/")
		cur := "."
		for i := 0; i < len(parts)-1; i++ {
			cur += "/" + parts[i]
			if seen[cur] {
				continue
			}
			seen[cur] = true
			dirs = append(dirs, tarEntry{Name: cur + "/", Mode: 0755, Dir: true})
		}
	}
	return append(dirs, files...)
}

type tarEntry struct {
	Name string
	Body []byte
	Mode int64
	Dir  bool
}

func makeTar(entries []tarEntry) ([]byte, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	epoch := time.Unix(0, 0)
	for _, e := range entries {
		hdr := &tar.Header{
			Name:     e.Name,
			Mode:     e.Mode,
			Uid:      0,
			Gid:      0,
			ModTime:  epoch,
			Format:   tar.FormatGNU,
		}
		if e.Dir {
			hdr.Typeflag = tar.TypeDir
			hdr.Size = 0
			if !strings.HasSuffix(hdr.Name, "/") {
				hdr.Name += "/"
			}
		} else {
			hdr.Typeflag = tar.TypeReg
			hdr.Size = int64(len(e.Body))
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, err
		}
		if !e.Dir {
			if _, err := tw.Write(e.Body); err != nil {
				return nil, err
			}
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func gzipBytes(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	gz.Header.ModTime = time.Time{}
	gz.Header.OS = 3
	if _, err := gz.Write(data); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func makeTarGZ(entries []tarEntry) ([]byte, error) {
	raw, err := makeTar(entries)
	if err != nil {
		return nil, err
	}
	return gzipBytes(raw)
}

func writeOpenWrtIPK(path string, debian, controlTar, dataTar []byte) error {
	// OpenWrt ipkg-build: tar ./debian-binary ./data.tar.gz ./control.tar.gz | gzip -n
	outer, err := makeTar([]tarEntry{
		{Name: "./debian-binary", Body: debian, Mode: 0644},
		{Name: "./data.tar.gz", Body: dataTar, Mode: 0644},
		{Name: "./control.tar.gz", Body: controlTar, Mode: 0644},
	})
	if err != nil {
		return err
	}
	gz, err := gzipBytes(outer)
	if err != nil {
		return err
	}
	return os.WriteFile(path, gz, 0644)
}

func controlFile(tmpl []byte, arch string, installed int) []byte {
	text := strings.ReplaceAll(string(tmpl), "%%ARCH%%", arch)
	if !strings.Contains(text, "Installed-Size:") {
		lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
		var out []string
		for _, line := range lines {
			out = append(out, line)
			if strings.HasPrefix(line, "Architecture:") {
				out = append(out, "Installed-Size: "+strconv.Itoa(installed))
			}
		}
		text = strings.Join(out, "\n") + "\n"
	}
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return []byte(text)
}

func unixBytes(path string) []byte {
	b, err := os.ReadFile(path)
	must(err)
	return bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
}

func sizeOf(path string) int64 {
	st, err := os.Stat(path)
	must(err)
	return st.Size()
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
