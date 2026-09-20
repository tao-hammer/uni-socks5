# uni-socks5

轻量 SOCKS5 代理。支持直接运行、Debian 包，以及 OpenWrt / LEDE 的 ipk + LuCI。
支持 TCP 转发（CONNECT）和 UDP 中继（UDP ASSOCIATE），QUIC、语音、DNS 等 UDP 流量可以走隧道。

## 交叉编译（直接出二进制）

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
| Windows 32 位 | `windows` | `386` |
| macOS 64 位 | `darwin` | `amd64` |
| ARM 32 位 | `linux` | `arm`（建议 `GOARM=7`） |
| ARM64 | `linux` | `arm64` |

运行：

```sh
./taosocks -c config.yaml
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
| `dist/x86_64/` | x86_64 | `tao-socks_1.0.7-1_x86_64.ipk` |
| `dist/i386/` | 32 位 x86 | `tao-socks_1.0.7-1_i386_pentium4.ipk` |
| `dist/armv7/` | 常见 32 位 ARM | `tao-socks_1.0.7-1_arm_cortex-a7_neon-vfpv4.ipk` |
| `dist/aarch64/` | ARM64 | `tao-socks_1.0.7-1_aarch64_generic.ipk` |

设备架构可在 SSH 里看：

```sh
opkg print-architecture
```

若架构名不完全一致（例如 `aarch64_cortex-a53`、`i386`），可强制安装静态包：

```sh
opkg install tao-socks_1.0.7-1_aarch64_generic.ipk --force-architecture
```

### 安装

把对应平台目录里的 ipk 拷到设备后（只需这一个）：

```sh
opkg install /tmp/tao-socks_1.0.7-1_x86_64.ipk
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
| `open_firewall` | `1` | 是否自动放行端口 |
| `firewall_src` | `wan` | 放行区域：`wan` / `lan` / `*` |

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
