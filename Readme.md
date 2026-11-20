# uni-socks5

轻量 SOCKS5 代理服务器（单文件 Go 实现）。支持直接运行、Debian 包，以及 OpenWrt / LEDE 的 ipk + LuCI。

## 特性

- **CONNECT**：TCP 转发，IPv4 / IPv6 / 域名目标
- **UDP ASSOCIATE**：UDP 中继（FRAG=0），QUIC、语音通话、DNS 等 UDP 流量可过隧道
- RFC 1929 用户名密码认证；`-skip` 可让 RFC1918 私网来源（10/8、172.16/12、192.168/16）免认证
- 认证阶段 30 秒读超时、单连接 panic 兜底、半关闭转发（避免 RST 丢数据）
- UDP 会话与 TCP 控制连接绑定（TCP 断开即结束），空闲超时可用 `-udp-timeout` 调整

## 构建

需要 Go 1.18+。

```sh
export CGO_ENABLED=0
export GOOS=<目标操作系统>
export GOARCH=<目标架构>
go build -ldflags="-s -w" -o taosocks
```

| 目标平台 | `GOOS` | `GOARCH` |
|---|---|---|
| Linux 64 位 | `linux` | `amd64` |
| Linux 32 位 | `linux` | `386` |
| Windows 64 位 | `windows` | `amd64` |
| macOS 64 位 | `darwin` | `amd64` |
| macOS Apple Silicon | `darwin` | `arm64` |
| ARM 32 位 | `linux` | `arm`（建议 `GOARM=7`） |
| ARM64 | `linux` | `arm64` |

## 运行

```sh
./taosocks -c config.yaml
# 或命令行参数
./taosocks -port 28000 -u admin -p 'your-password' -log
```

### 命令行参数

| 参数 | 默认 | 说明 |
|---|---|---|
| `-c` | 空 | 配置文件路径（yaml）。指定后其他参数以配置文件为准 |
| `-port` | `28000` | 监听端口 |
| `-u` / `-p` | `admin` / `Qwer1234-` | 用户名 / 密码 |
| `-log` | 关 | 写文件日志（目录见 `-logdir`） |
| `-logdir` | `output` | 日志目录 |
| `-skip` | 关 | RFC1918 私网来源免认证 |
| `-udp-timeout` | `300` | UDP 会话空闲超时（秒） |
| `-core` | `0` | 兼容保留（Go 运行时自动调度，无实际意义） |
| `-max-conns` | `0` | 最大并发连接数，`0` = 不限制（可选安全组件） |
| `-auth-limit` | `0` | 单 IP 认证失败 N 次后封锁，`0` = 关闭（可选安全组件） |
| `-auth-block` | `300` | 认证封锁时长（秒） |
| `-idle-timeout` | `0` | 连接空闲超时（秒），`0` = 不限制（可选安全组件） |

### 安全说明

- 所有安全组件**默认关闭**，按需开启；开启后启动横幅会打印当前生效值
- 密码使用恒定时间比较；认证阶段有 30 秒读超时；单连接 panic 有兜底
- 建议公网暴露时使用 `-c` 配置文件传密码（`-p` 会出现在进程列表里），并将配置文件权限设为 `600`
- 日志文件权限 `0600`、日志目录 `0755`
- 系统层可配合 fail2ban / 防火墙做额外防护

### 配置文件（config.yaml）

```yaml
port: 28000
skipPrivateCheck: false
logFlag: false
coreNum: 0          # 兼容保留
logDir: output      # 日志目录
udpTimeout: 300     # UDP 会话空闲超时（秒），0 或未设置则用默认值
# —— 可选安全组件，0 = 关闭（默认）——
maxConns: 0         # 最大并发连接数
authLimit: 0        # 单 IP 认证失败 N 次后封锁
authBlock: 300      # 认证封锁时长（秒）
idleTimeout: 0      # 连接空闲超时（秒）
user:
  username: admin
  password: admin
```

## OpenWrt / LEDE

在仓库根目录打包（需 Go 1.18+）：

```powershell
.\scripts\build-ipk.ps1
```

```sh
sh scripts/build-ipk.sh
```

产物按平台放在 `dist/`，每个目录里 **一个 ipk**，已包含 SOCKS 程序和 LuCI 页面：

| 目录 | 适用 | 安装文件 |
|---|---|---|
| `dist/x86_64/` | x86_64 | `tao-socks_1.0.8-1_x86_64.ipk` |
| `dist/i386/` | 32 位 x86 | `tao-socks_1.0.8-1_i386_pentium4.ipk` |
| `dist/armv7/` | 常见 32 位 ARM | `tao-socks_1.0.8-1_arm_cortex-a7_neon-vfpv4.ipk` |
| `dist/aarch64/` | ARM64 | `tao-socks_1.0.8-1_aarch64_generic.ipk` |

设备架构可在 SSH 里看：

```sh
opkg print-architecture
```

若架构名不完全一致（例如 `aarch64_cortex-a53`、`i386`），可强制安装静态包：

```sh
opkg install tao-socks_1.0.8-1_aarch64_generic.ipk --force-architecture
```

### 安装

把对应平台目录里的 ipk 拷到设备后（只需这一个）：

```sh
opkg install /tmp/tao-socks_1.0.8-1_x86_64.ipk
```

默认 **启用并开机自启**，监听 **28000**，账密 **admin / admin**，并在 **wan** 放行该端口。  
请尽快改密码；不需要外网访问时，把防火墙区域改成 `lan` 或关掉自动放行。

LuCI：`服务` → `Tao SOCKS5`。

### UCI

配置文件：`/etc/config/taosocks`

| 选项 | 默认 | 说明 |
|---|---|---|
| `enabled` | `1` | 是否启动进程 |
| `auto_start` | `1` | 是否开机自启 |
| `port` | `28000` | 监听端口 |
| `username` / `password` | `admin` | 认证账密 |
| `skip_private_check` | `0` | RFC1918 私网来源（10.x、172.16-31.x、192.168.x）免认证 |
| `core_num` | `0` | 兼容保留，无实际意义 |
| `log_flag` | `0` | 是否写文件日志 |
| `log_dir` | `/tmp/taosocks` | 日志目录 |
| `udp_timeout` | `300` | UDP 中继会话空闲超时（秒） |
| `max_conns` | `0` | 可选安全组件：最大并发连接数，0=不限制 |
| `auth_limit` | `0` | 可选安全组件：单 IP 认证失败 N 次后封锁，0=关闭 |
| `auth_block` | `300` | 认证封锁时长（秒），到期自动解除 |
| `idle_timeout` | `0` | 可选安全组件：认证后连接空闲超时（秒），0=不限制 |
| `open_firewall` | `1` | 是否自动放行端口 |
| `firewall_src` | `wan` | 放行区域：`wan` / `lan` / `*` |

安全组件（`max_conns` / `auth_limit` / `auth_block` / `idle_timeout`）**默认关闭**，0 = 不启用；按需通过 LuCI 或 UCI 开启。密码使用恒定时间比较，认证阶段有 30 秒读超时。

```sh
uci set taosocks.main.port='1080'
uci set taosocks.main.password='your-password'
uci commit taosocks
/etc/init.d/taosocks reload
```

```sh
/etc/init.d/taosocks start
/etc/init.d/taosocks stop
/etc/init.d/taosocks enable
/etc/init.d/taosocks disable
```

### 关于 UDP 中继

UDP ASSOCIATE 的中继端口是**临时端口**（每次会话随机）。局域网内的客户端使用 UDP 中继无任何障碍；但**外网客户端**经防火墙访问时，UDP 数据报到达临时端口会被默认丢弃——如需外网 UDP，除放行 TCP 端口外，还要在外层（如 frp、路由器端口转发）显式转发 UDP，或将使用场景限制在局域网内。

### 卸载

```sh
opkg remove tao-socks
```
