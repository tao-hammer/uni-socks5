# uni-socks5

轻量 SOCKS5 代理服务器（单文件 Go 实现）。

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

## 分支

| 分支 | 说明 |
|---|---|
| `main` | 纯源码，通用 Linux / Windows / macOS |
| `openwrt` | OpenWrt / LEDE 适配：ipk 打包、LuCI 界面、UCI 配置、init 脚本 |
