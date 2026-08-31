# OpenWrt（x86）适配方案与实施计划

> 分支：`openwrt`  
> 状态：**已按确认项执行**  
> 日期：2026-08-28  
> 确认：x86_64 + i386 + ARM32 + ARM64；默认启用/自启；admin/admin；做 LuCI；自动放行防火墙；日志 `/tmp/taosocks`；二进制 `taosocks`

---

## 1. 目标

把现有 Go SOCKS5 服务做成可在 **x86 OpenWrt / LEDE** 上安装的软件包，做到：

1. 生成可直接 `opkg install` 的 **ipk**
2. 用 **UCI（LEDE 配置）** 管理全部运行参数
3. 用 UCI 控制 **是否启动**、**是否开机自启**
4. 安装后可用 `uci` / `/etc/init.d` 运维，不必手改 yaml

目标平台默认：**OpenWrt x86_64**（兼容 Lean LEDE、ImmortalWrt、iStoreOS 等常见 x86 固件）。  
可选附加：**i386（32 位 x86）**，确认后一并出包。

最低系统：带 **procd** 的 OpenWrt/LEDE（17.01 / 18.06 及更新）。x86 固件基本都满足。

---

## 2. 现状（本仓库）

| 项 | 现状 |
|---|---|
| 程序 | 纯 Go SOCKS5，`CGO_ENABLED=0` 可静态交叉编译 |
| 配置 | `config.yaml`：`port` / `skipPrivateCheck` / `logFlag` / `coreNum` / `user.username` / `user.password` |
| 启动参数 | 已有 CLI：`-c` `-port` `-core` `-log` `-skip` `-u` `-p` |
| 现有打包 | Debian + systemd（`deb/`），不适用于 OpenWrt |
| 日志目录 | 写到进程 cwd 下 `./output`，OpenWrt 上不安全、也不合适 |
| 依赖 | `go.mod` 里有 viper/fsnotify，运行路径实际只用 `yaml.v3`，属未使用依赖 |

结论：应用逻辑可复用；主要工作是 **OpenWrt 包装（UCI + procd + ipk）**，并做少量 Go 侧路径修正。

---

## 3. 关键设计决定（请重点看）

### 3.1 配置以 UCI 为唯一来源

OpenWrt/LEDE 标准做法：`/etc/config/taosocks`。

启动时由 init 脚本把 UCI **生成一份临时 yaml**（`/var/etc/taosocks.yaml`），再执行：

```text
taosocks -c /var/etc/taosocks.yaml
```

原因：

- 不改业务协议，继续走现有 `-c` 加载逻辑
- **密码不会出现在 `ps` 命令行里**
- `/etc/config/taosocks` 由 opkg 标为 conffile，升级保留用户配置
- 临时 yaml 放 `/var/etc/`，重启后按 UCI 重新生成

不在运行时直接依赖 `/etc/taosocks/config.yaml`（避免两套配置打架）。

### 3.2 「是否启动」和「是否开机自启」分开

这是 OpenWrt 里两件不同的事：

| UCI 项 | 含义 | 效果 |
|---|---|---|
| `enabled` | 是否真正拉起进程 | `0` 时即使执行了 `start` 也不跑二进制 |
| `auto_start` | 是否开机自启 | `1` → `/etc/init.d/taosocks enable`；`0` → `disable` |

对应命令：

```sh
# 现在启动 / 停止 / 重启
/etc/init.d/taosocks start
/etc/init.d/taosocks stop
/etc/init.d/taosocks restart

# 开机自启开 / 关（也会写回 auto_start，见下）
/etc/init.d/taosocks enable
/etc/init.d/taosocks disable

# 改配置后生效
uci commit taosocks
/etc/init.d/taosocks reload
```

init 脚本在 `start`/`reload` 时读取 `auto_start`，同步 enable/disable，避免 UCI 和 `/etc/rc.d` 不一致。

**默认值（安装后）：**

- `enabled=1`：安装后尝试启动
- `auto_start=1`：默认开机自启（与当前 deb 包 `systemctl enable` 行为一致）

若你希望默认不启动、不自启，确认计划时改这两项即可。

### 3.3 架构与依赖

- 主产物：`GOOS=linux GOARCH=amd64 CGO_ENABLED=0` 静态二进制  
- ipk 的 `Architecture` 必须写 **`x86_64`**（OpenWrt 不用 Debian 的 `amd64`）
- 静态链接 → `Depends` 为空，不绑 musl/glibc 版本，x86 固件更好装
- 编译加 `-ldflags="-s -w"` 缩小体积

可选第二产物：`GOARCH=386` → `Architecture: i386`。

### 3.4 本阶段不做 LuCI 网页

「LEDE 配置」按 **UCI + init.d** 交付，LuCI 应用放到后续（见第 11 节）。  
你确认需要网页配置的话，可以加进本期。

### 3.5 防火墙

本期 **不自动改 firewall**（避免误开 WAN 端口）。  
安装说明里写清：若从 LAN 访问，一般无需改防火墙；若从 WAN 访问，需自行放行 `port`。

可选增强（默认不做）：UCI 增加 `open_firewall`，用 `firewall.user` 或 uci firewall 规则放行。需要的话请在审查时勾上。

---

## 4. UCI 配置清单

文件：`/etc/config/taosocks`

```
config taosocks 'main'
    option enabled '1'
    option auto_start '1'
    option port '28000'
    option skip_private_check '0'
    option log_flag '0'
    option core_num '0'
    option username 'admin'
    option password 'admin'
    option log_dir '/tmp/taosocks'
```

### 4.1 与现有 yaml 的对应

| UCI | 类型 | 默认 | 对应 yaml / 行为 |
|---|---|---|---|
| `enabled` | bool | `1` | **新增**。`0` 不启动进程 |
| `auto_start` | bool | `1` | **新增**。开机是否拉起 init |
| `port` | int | `28000` | `port` |
| `skip_private_check` | bool | `0` | `skipPrivateCheck`（内网 `10.10.*` 免密） |
| `log_flag` | bool | `0` | `logFlag` |
| `core_num` | int | `0` | `coreNum`（`0` = CPU 核数 × 2） |
| `username` | string | `admin` | `user.username` |
| `password` | string | `admin` | `user.password` |
| `log_dir` | string | `/tmp/taosocks` | **新增**。日志目录；`/tmp` 在 OpenWrt 上可写、不占 overlay |

UCI 用下划线命名，符合 OpenWrt 习惯。

### 4.2 生成的临时 yaml 示例

`/var/etc/taosocks.yaml`（每次 start/reload 覆盖）：

```yaml
port: 28000
skipPrivateCheck: false
logFlag: false
coreNum: 0
logDir: /tmp/taosocks
user:
  username: admin
  password: admin
```

### 4.3 常用 uci 操作示例

```sh
uci set taosocks.main.port='1080'
uci set taosocks.main.username='user'
uci set taosocks.main.password='secret'
uci set taosocks.main.enabled='1'
uci set taosocks.main.auto_start='1'
uci commit taosocks
/etc/init.d/taosocks reload
```

---

## 5. 软件包与目录设计

### 5.1 包信息

| 项 | 值 |
|---|---|
| Package | `tao-socks`（与现有 deb 一致） |
| Version | `1.0.0` |
| Release | `1` → 文件名 `tao-socks_1.0.0-1_x86_64.ipk` |
| Section | `net` |
| Maintainer | `taotao` |
| 二进制路径 | `/usr/bin/taosocks` |
| UCI | `/etc/config/taosocks` |
| init | `/etc/init.d/taosocks` |

### 5.2 仓库新增结构（拟）

```text
openwrt/
  files/
    taosocks.init          → /etc/init.d/taosocks
    taosocks.config        → /etc/config/taosocks
  ipk/
    control
    conffiles
    postinst
    prerm
    postrm
scripts/
  build-ipk.ps1            Windows 一键交叉编译 + 打包
  build-ipk.sh             Linux/mac 同等脚本
PLAN.md                    本文件
```

现有 `deb/` **不动**，Debian 安装方式继续可用。

### 5.3 ipk 内部布局

```text
tao-socks_1.0.0-1_x86_64.ipk    (ar 归档)
├── debian-binary               "2.0\n"
├── control.tar.gz
│   ├── control
│   ├── conffiles
│   ├── postinst
│   ├── prerm
│   └── postrm
└── data.tar.gz
    ├── usr/bin/taosocks
    ├── etc/init.d/taosocks
    └── etc/config/taosocks
```

`conffiles` 只列：

```text
/etc/config/taosocks
```

压缩用 **tar.gz**（兼容旧 LEDE / 新 OpenWrt，避免部分机子不认 xz）。

### 5.4 安装脚本行为

**postinst**（真机安装，跳过 `IPKG_INSTROOT` 交叉根）：

1. `chmod 755` 二进制和 init
2. 读 UCI：`auto_start=1` 则 `enable`，否则 `disable`
3. `enabled=1` 则 `start`，否则不启动

**prerm**：`stop` + `disable`

**postrm**（`remove` 而非 `upgrade`）：删除 `/var/etc/taosocks.yaml` 和日志目录；**不删** `/etc/config/taosocks`（conffile，由 opkg 问用户）。

---

## 6. 程序改动（尽量小）

### 6.1 必须

1. **日志目录可配置**  
   - `Config` 增加 `logDir`  
   - CLI 增加 `-logdir`  
   - `OpenFile()` 不再写死 `./output`  
   - OpenWrt 默认 `/tmp/taosocks`

2. **写日志失败不要把进程打死**  
   当前 `WriteFileLog` 里 `log.Fatal` 会把整个代理杀掉。改为打 stderr 后返回。

### 6.2 建议（工作量小，建议做）

3. **去掉未使用的 viper/fsnotify**  
   减小体积、加快编译。确认无引用后从 `go.mod` 清理。

### 6.3 明确不做（除非你要求）

- 不改 SOCKS5 协议与认证逻辑  
- 不新增 IPv6 CONNECT（代码里仍是未支持）  
- 不做 LuCI  
- 不自动改防火墙  
- 不改 `10.10.*` 内网免密规则（保持原样）

---

## 7. init 脚本要点

`#!/bin/sh /etc/rc.common`

- `START=99` `STOP=10` `USE_PROCD=1`
- `start_service`：
  - `config_load taosocks`
  - `enabled!=1` 则直接 return
  - 确保 `/var/etc` 存在，写出 yaml
  - `procd` 启动 `/usr/bin/taosocks -c /var/etc/taosocks.yaml`
  - `respawn`（崩溃自动拉起，替代 systemd `Restart=always`）
  - stdout/stderr 进 logd（`logread` 可看）
- `service_triggers`：`procd_add_reload_trigger taosocks`
- `reload_service`：stop + start（重新生成 yaml）
- `boot()` 或 `start_service` 末尾按 `auto_start` 校正 enable 状态（避免只改 UCI 却仍开机启动）

---

## 8. 构建与产物

在 Windows（当前环境）用 PowerShell：

```powershell
.\scripts\build-ipk.ps1
# 可选
.\scripts\build-ipk.ps1 -Arch 386
```

步骤：

1. `go version` 检查（需要 Go 1.18+，与 `go.mod` 一致）
2. `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o .../usr/bin/taosocks`
3. 组装 data 树、control 树
4. 打 `control.tar.gz` / `data.tar.gz`（系统自带 `tar`）
5. 写成 ar 格式 `.ipk`（Windows 不一定有 `ar`，脚本内用少量 Python/.NET 写 ar，不依赖 MSYS）
6. 输出到 `dist/tao-socks_1.0.0-1_x86_64.ipk`

`dist/` 加入 `.gitignore`。ipk **可以提交或只本地生成**，默认不把二进制 ipk 推进 git。

---

## 9. 设备上的安装与验证

把 ipk 拷到路由器后：

```sh
opkg install /tmp/tao-socks_1.0.0-1_x86_64.ipk

uci show taosocks
/etc/init.d/taosocks status   # 若固件支持
ps | grep taosocks
netstat -lntp | grep 28000    # 或 logread | grep taosocks
```

开关示例：

```sh
# 关掉进程，但保留开机自启配置
uci set taosocks.main.enabled='0'
uci commit taosocks
/etc/init.d/taosocks stop

# 关闭开机自启
uci set taosocks.main.auto_start='0'
uci commit taosocks
/etc/init.d/taosocks disable
```

卸载：

```sh
opkg remove tao-socks
```

---

## 10. 实施步骤（确认后按此执行）

1. 增加 `logDir` 配置与 CLI，修正日志路径；去掉 `log.Fatal`  
2. 视审查结果清理未使用依赖  
3. 编写 UCI 默认文件、procd init、control/postinst/prerm/postrm  
4. 编写 `scripts/build-ipk.ps1`（及 `.sh`）  
5. 在本机交叉编译并生成 `dist/*.ipk`  
6. 更新 `Readme.md`：OpenWrt 安装、UCI 项、启停与自启  
7. **不自动 commit / 不自动推送**；你检查通过后再说是否提交、是否 push 到 `openwrt` 分支

---

## 11. 范围外 / 可后续做

| 项 | 说明 |
|---|---|
| LuCI 页面 | `luci-app-taosocks`，网页改端口/账密/启停/自启 |
| 自动放行防火墙 | UCI `open_firewall` |
| 监听地址 | 目前是 `:%d`（全接口）；可加 `listen`/`bind` |
| 多用户 | 代码里仍是单用户 |
| IPv6 | CONNECT 仍不支持 |
| OpenWrt SDK Makefile | 给官方 buildroot 用；本期用离线 ipk 即可，x86 静态包足够 |
| 非 x86（arm/arm64 路由器） | 同一套脚本换 `GOARCH` 即可，本期不做除非你要 |

---

## 12. 风险与约束

| 风险 | 处理 |
|---|---|
| 本机是 Windows，无法在这里实机装 OpenWrt 验证 | 先保证 ipk 结构、权限、脚本语法正确；你在 x86 盒子上 `opkg install` 验收 |
| 部分极老固件 opkg 较挑剔 | 用 gzip + 标准 control 字段；`Architecture: x86_64` |
| 日志写 overlay 撑满闪存 | 默认 `/tmp/taosocks`（内存盘） |
| 密码在 UCI 明文 | 与 OpenWrt 多数软件包相同；权限 `600` 的 config 文件 |
| 静态 Go 二进制数 MB | x86 磁盘通常够用；strip 后一般可接受 |

---

## 13. 需要你确认的点

请直接改意见或回复编号：

1. **架构**：只做 x86_64，还是 x86_64 + i386 两个 ipk？  
   **建议：只做 x86_64。**
2. **默认启停**：安装后默认 `enabled=1` 且 `auto_start=1` 是否可以？  
   **建议：可以（与当前 deb 一致）。**
3. **默认密码**：UCI 默认仍 `admin/admin`，还是改成更强的默认值？  
   **建议：保持 admin/admin，与现网 yaml 一致，由你安装后修改。**
4. **本期要不要 LuCI？**  
   **建议：不要，先 UCI。**
5. **本期要不要自动开防火墙端口？**  
   **建议：不要。**
6. **日志目录**：默认 `/tmp/taosocks` 还是 `/var/log/taosocks`？  
   **建议：`/tmp/taosocks`（OpenWrt 更稳妥）。**
7. **二进制名**：`/usr/bin/taosocks` 还是继续叫 `socks5`？  
   **建议：`taosocks`，和包名、init 名一致。**

---

确认以上方案（或给出修改）后，再按第 10 节执行，不提前改代码、不推送。
