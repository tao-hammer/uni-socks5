#!/usr/bin/env python3
"""Cross-compile taosocks and pack OpenWrt ipk files."""

from __future__ import annotations

import gzip
import io
import os
import stat
import subprocess
import sys
import tarfile
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
DIST = ROOT / "dist"
VERSION = "1.0.8-1"

# goarch / goarm -> OpenWrt Architecture field
TARGETS = [
    {"goarch": "amd64", "goarm": None, "ipk_arch": "x86_64"},
    {"goarch": "386", "goarm": None, "ipk_arch": "i386_pentium4"},
    {"goarch": "arm", "goarm": "7", "ipk_arch": "arm_cortex-a7_neon-vfpv4"},
    {"goarch": "arm64", "goarm": None, "ipk_arch": "aarch64_generic"},
]


def run(cmd: list[str], env: dict[str, str] | None = None) -> None:
    merged = os.environ.copy()
    if env:
        merged.update(env)
    print("+", " ".join(cmd))
    subprocess.check_call(cmd, cwd=ROOT, env=merged)


def tar_add_bytes(tar: tarfile.TarFile, name: str, data: bytes, mode: int = 0o644) -> None:
    info = tarfile.TarInfo(name=name)
    info.size = len(data)
    info.mode = mode
    info.uid = 0
    info.gid = 0
    info.uname = "root"
    info.gname = "root"
    info.mtime = int(time.time())
    tar.addfile(info, io.BytesIO(data))


def tar_add_file(tar: tarfile.TarFile, name: str, path: Path, mode: int | None = None) -> None:
    data = path.read_bytes()
    if mode is None:
        mode = 0o755 if path.stat().st_mode & stat.S_IXUSR else 0o644
        if path.suffix in {".init", ""} and path.name in {"postinst", "prerm", "postrm", "taosocks.init"}:
            mode = 0o755
    tar_add_bytes(tar, name, data, mode)


def make_targz(members: list[tuple[str, bytes, int]]) -> bytes:
    raw = io.BytesIO()
    with tarfile.open(fileobj=raw, mode="w") as tar:
        for name, data, mode in members:
            tar_add_bytes(tar, name, data, mode)
    return gzip.compress(raw.getvalue(), mtime=0)


def write_ar(path: Path, members: list[tuple[str, bytes]]) -> None:
    """GNU ar used by opkg (global header + 60-byte file headers)."""
    with path.open("wb") as f:
        f.write(b"!<arch>\n")
        for name, data in members:
            header = (
                name.encode("ascii")[:16].ljust(16)
                + b"0           "
                + b"0     "
                + b"0     "
                + b"100644  "
                + f"{len(data)}".encode("ascii").ljust(10)
                + b"`\n"
            )
            if len(header) != 60:
                raise RuntimeError(f"bad ar header length {len(header)} for {name}")
            f.write(header)
            f.write(data)
            if len(data) % 2 == 1:
                f.write(b"\n")


def read_control_template(path: Path, arch: str, installed_size: int) -> bytes:
    text = path.read_text(encoding="utf-8")
    text = text.replace("%%ARCH%%", arch)
    if "Installed-Size:" not in text:
        lines = text.splitlines()
        out = []
        for line in lines:
            out.append(line)
            if line.startswith("Architecture:"):
                out.append(f"Installed-Size: {installed_size}")
        text = "\n".join(out) + "\n"
    else:
        text = text.replace("%%SIZE%%", str(installed_size))
    if not text.endswith("\n"):
        text += "\n"
    return text.encode("utf-8")


def build_binary(goarch: str, goarm: str | None, dest: Path) -> None:
    dest.parent.mkdir(parents=True, exist_ok=True)
    env = {
        "CGO_ENABLED": "0",
        "GOOS": "linux",
        "GOARCH": goarch,
    }
    if goarm:
        env["GOARM"] = goarm
    run(
        [
            "go",
            "build",
            "-trimpath",
            "-ldflags=-s -w",
            "-o",
            str(dest),
        ],
        env=env,
    )


def pack_main_ipk(binary: Path, ipk_arch: str) -> Path:
    init_script = (ROOT / "openwrt" / "files" / "taosocks.init").read_bytes().replace(b"\r\n", b"\n")
    uci_conf = (ROOT / "openwrt" / "files" / "taosocks.config").read_bytes().replace(b"\r\n", b"\n")
    bin_data = binary.read_bytes()

    data_members = [
        ("./usr/bin/taosocks", bin_data, 0o755),
        ("./etc/init.d/taosocks", init_script, 0o755),
        ("./etc/config/taosocks", uci_conf, 0o600),
    ]
    data_tar = make_targz(data_members)
    installed_size = (len(bin_data) + len(init_script) + len(uci_conf) + 1023) // 1024

    control = read_control_template(ROOT / "openwrt" / "ipk" / "control", ipk_arch, installed_size)
    conffiles = (ROOT / "openwrt" / "ipk" / "conffiles").read_bytes().replace(b"\r\n", b"\n")
    postinst = (ROOT / "openwrt" / "ipk" / "postinst").read_bytes().replace(b"\r\n", b"\n")
    prerm = (ROOT / "openwrt" / "ipk" / "prerm").read_bytes().replace(b"\r\n", b"\n")
    postrm = (ROOT / "openwrt" / "ipk" / "postrm").read_bytes().replace(b"\r\n", b"\n")

    control_members = [
        ("./control", control, 0o644),
        ("./conffiles", conffiles, 0o644),
        ("./postinst", postinst, 0o755),
        ("./prerm", prerm, 0o755),
        ("./postrm", postrm, 0o755),
    ]
    control_tar = make_targz(control_members)

    out = DIST / f"tao-socks_{VERSION}_{ipk_arch}.ipk"
    write_ar(
        out,
        [
            ("debian-binary", b"2.0\n"),
            ("control.tar.gz", control_tar),
            ("data.tar.gz", data_tar),
        ],
    )
    print(f"built {out} ({out.stat().st_size} bytes)")
    return out


def pack_luci_ipk() -> Path:
    controller = (ROOT / "openwrt" / "luci" / "luasrc" / "controller" / "taosocks.lua").read_bytes().replace(b"\r\n", b"\n")
    cbi = (ROOT / "openwrt" / "luci" / "luasrc" / "model" / "cbi" / "taosocks.lua").read_bytes().replace(b"\r\n", b"\n")
    acl = (ROOT / "openwrt" / "luci" / "root" / "usr" / "share" / "rpcd" / "acl.d" / "luci-app-taosocks.json").read_bytes().replace(b"\r\n", b"\n")

    data_members = [
        ("./usr/lib/lua/luci/controller/taosocks.lua", controller, 0o644),
        ("./usr/lib/lua/luci/model/cbi/taosocks.lua", cbi, 0o644),
        ("./usr/share/rpcd/acl.d/luci-app-taosocks.json", acl, 0o644),
    ]
    data_tar = make_targz(data_members)
    installed_size = (len(controller) + len(cbi) + len(acl) + 1023) // 1024

    control = read_control_template(ROOT / "openwrt" / "luci" / "ipk" / "control", "all", installed_size)
    postinst = (ROOT / "openwrt" / "luci" / "ipk" / "postinst").read_bytes().replace(b"\r\n", b"\n")
    prerm = (ROOT / "openwrt" / "luci" / "ipk" / "prerm").read_bytes().replace(b"\r\n", b"\n")
    control_tar = make_targz(
        [
            ("./control", control, 0o644),
            ("./postinst", postinst, 0o755),
            ("./prerm", prerm, 0o755),
        ]
    )

    out = DIST / f"luci-app-taosocks_{VERSION}_all.ipk"
    write_ar(
        out,
        [
            ("debian-binary", b"2.0\n"),
            ("control.tar.gz", control_tar),
            ("data.tar.gz", data_tar),
        ],
    )
    print(f"built {out} ({out.stat().st_size} bytes)")
    return out


def main() -> int:
    DIST.mkdir(parents=True, exist_ok=True)
    build_dir = DIST / "build"
    build_dir.mkdir(parents=True, exist_ok=True)

    for t in TARGETS:
        binary = build_dir / f"taosocks-{t['ipk_arch']}"
        build_binary(t["goarch"], t["goarm"], binary)
        pack_main_ipk(binary, t["ipk_arch"])

    pack_luci_ipk()
    print("done")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except subprocess.CalledProcessError as exc:
        print(f"command failed: {exc}", file=sys.stderr)
        raise SystemExit(exc.returncode)
