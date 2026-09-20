package main

import (
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"gopkg.in/yaml.v3"
	"io"
	"log"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"socks5/config"
	"strconv"
	"sync"
	"time"
)

var port int64
var logFlag bool //关闭日志输出
var skipPrivateCheck bool
var username string
var password string
var core int
var c string
var udpTimeout int
var logDir string

const authTimeout = 30 * time.Second //认证阶段的读超时，防止半开连接拖死服务

func main() {
	flag.StringVar(&c, "c", "", "配置文件")
	flag.Int64Var(&port, "port", 28000, "端口号")
	flag.IntVar(&core, "core", 0, "线程数量（兼容保留，Go 中无实际意义）")
	flag.BoolVar(&logFlag, "log", false, "日志,默认关闭")
	flag.BoolVar(&skipPrivateCheck, "skip", false, "局域网免校验,默认关闭")
	flag.StringVar(&username, "u", "admin", "用户名")
	flag.StringVar(&password, "p", "Qwer1234-", "密码")
	flag.IntVar(&udpTimeout, "udp-timeout", 300, "UDP 会话空闲超时(秒)")
	flag.StringVar(&logDir, "logdir", "", "日志目录，默认 output")
	flag.Parse()
	if c != "" {
		// 加载配置
		conf, err := loadConfig(c)
		if err != nil {
			log.Fatalf("Error loading config: %v", err)
		}
		port = conf.Port
		core = conf.CoreNum
		logFlag = conf.LogFlag
		skipPrivateCheck = conf.SkipPrivateCheck
		username = conf.User.Username
		password = conf.User.Password
		if conf.LogDir != "" {
			logDir = conf.LogDir
		}
	}
	if logDir == "" {
		logDir = "output"
	}
	if core == 0 {
		core = runtime.NumCPU() * 2
	}
	fmt.Printf("核心数量: %d, UDP空闲超时: %ds, 局域网免认证: %v\n", core, udpTimeout, skipPrivateCheck)

	server, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Fatalf("Listen failed: %v", err)
	}
	fmt.Printf("socks5 服务已启动 :%d\n", port)
	// Go 的 Accept 本身是多路复用的，单个循环即可，无需手动开多个 accept 线程
	for {
		client, err := server.Accept()
		if err != nil {
			log.Printf("Accept failed: %v", err)
			continue
		}
		go process(client)
	}
}

func loadConfig(filename string) (*config.Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	var conf config.Config
	if err := yaml.Unmarshal(data, &conf); err != nil {
		return nil, fmt.Errorf("failed to unmarshal YAML: %w", err)
	}
	return &conf, nil
}

func process(client net.Conn) {
	// 单连接 panic（如畸形包触发的越界）不得拖垮整个服务
	defer func() {
		if r := recover(); r != nil {
			log.Printf("【%s】连接处理panic: %v", client.RemoteAddr(), r)
		}
		client.Close()
	}()

	remote := client.RemoteAddr().String()
	if err := Socks5Auth(client); err != nil {
		log.Printf("【%s】认证错误: %v", remote, err)
		if logFlag {
			go WriteFileLog("【ip】"+remote+"【时间】"+time.Now().Format("2006-01-02 15:04:05")+";认证错误:"+err.Error(), 2)
		}
		return
	}
	target, isUDP, logInfo, err := Socks5Connect(client)
	if err != nil {
		log.Printf("【%s】连接错误: %v", remote, err)
		if logFlag {
			go WriteFileLog("【ip】"+remote+"【时间】"+time.Now().Format("2006-01-02 15:04:05")+";连接错误:"+err.Error(), 2)
		}
		return
	}
	if isUDP {
		// UDP ASSOCIATE 会话已随 TCP 断开而结束
		log.Printf("UDP会话结束: %s", logInfo)
		if logFlag {
			go WriteFileLog("UDP会话结束:"+logInfo, 1)
		}
		return
	}
	Socks5Forward(client, logInfo, target)
}

// Socks5Auth 方法协商 + RFC 1929 用户名密码认证
func Socks5Auth(client net.Conn) error {
	_ = client.SetReadDeadline(time.Now().Add(authTimeout))
	defer func() { _ = client.SetReadDeadline(time.Time{}) }()

	hdr := make([]byte, 2)
	if _, err := io.ReadFull(client, hdr); err != nil {
		return errors.New("reading header: " + err.Error())
	}
	if hdr[0] != 5 {
		return errors.New("invalid version")
	}
	methods := make([]byte, hdr[1])
	if _, err := io.ReadFull(client, methods); err != nil {
		return errors.New("reading methods: " + err.Error())
	}

	// 局域网免认证：-skip 开启且来源为 RFC1918 私网地址
	if skipPrivateCheck && isPrivateRemote(client) {
		fmt.Println("【内网ip】" + client.RemoteAddr().String() + ";直接放过")
		_, err := client.Write([]byte{0x05, 0x00})
		return err
	}

	// 选择用户名密码认证方式
	if _, err := client.Write([]byte{0x05, 0x02}); err != nil {
		return errors.New("write rsp err: " + err.Error())
	}

	// RFC 1929: VER(0x01) ULEN UNAME PLEN PASSWD，逐字段 ReadFull，杜绝半包/越界
	head := make([]byte, 2)
	if _, err := io.ReadFull(client, head); err != nil {
		return errors.New("reading auth header: " + err.Error())
	}
	if head[0] != 1 {
		return errors.New("invalid auth version")
	}
	uname := make([]byte, head[1])
	if _, err := io.ReadFull(client, uname); err != nil {
		return errors.New("reading username: " + err.Error())
	}
	plen := make([]byte, 1)
	if _, err := io.ReadFull(client, plen); err != nil {
		return errors.New("reading password len: " + err.Error())
	}
	passwd := make([]byte, plen[0])
	if _, err := io.ReadFull(client, passwd); err != nil {
		return errors.New("reading password: " + err.Error())
	}

	if string(uname) == username && string(passwd) == password {
		fmt.Println(client.RemoteAddr().String() + ":密码正确")
		if logFlag {
			go WriteFileLog(client.RemoteAddr().String()+":密码正确", 0)
		}
		// ★ 修正：认证子协商的 VER 必须是 0x01（原来写成 0x05，不符合 RFC 1929，
		//   Mihomo 宽容所以能连，严格客户端会判失败）
		_, err := client.Write([]byte{0x01, 0x00})
		return err
	}
	fmt.Println(client.RemoteAddr().String() + ":密码错误")
	if logFlag {
		go WriteFileLog(client.RemoteAddr().String()+":密码错误", 0)
	}
	_, _ = client.Write([]byte{0x01, 0x01})
	return errors.New("auth failed")
}

// isPrivateRemote 判断对端是否为私网地址（10/8、172.16/12、192.168/16）
func isPrivateRemote(client net.Conn) bool {
	ap, err := netip.ParseAddrPort(client.RemoteAddr().String())
	if err != nil {
		return false
	}
	return ap.Addr().IsPrivate()
}

// Socks5Connect 解析 CONNECT / UDP ASSOCIATE 请求。
// 返回 isUDP=true 表示 UDP 会话已完整处理（TCP 断开）并直接结束。
func Socks5Connect(client net.Conn) (net.Conn, bool, string, error) {
	buf := make([]byte, 256)
	if _, err := io.ReadFull(client, buf[:4]); err != nil {
		return nil, false, "", errors.New("read header: " + err.Error())
	}
	//VER sock版本号
	//cmd 0x01=CONNECT, 0x02=BIND, 0x03=UDP ASSOCIATE
	//RSV 保留字段
	//ATYP 地址类型，0x01=IPv4，0x03=域名，0x04=IPv6
	ver, cmd, _, atyp := buf[0], buf[1], buf[2], buf[3]
	if ver != 5 {
		return nil, false, "", errors.New("invalid version")
	}
	if cmd != 1 && cmd != 3 {
		_, _ = client.Write([]byte{0x05, 0x07, 0x00, 0x01, 0, 0, 0, 0, 0, 0}) // command not supported
		return nil, false, "", fmt.Errorf("unsupported cmd: %d", cmd)
	}
	addr, err := readAddr(client, buf, atyp)
	if err != nil {
		return nil, false, "", err
	}
	if _, err := io.ReadFull(client, buf[:2]); err != nil {
		return nil, false, "", errors.New("read port: " + err.Error())
	}
	dstPort := binary.BigEndian.Uint16(buf[:2])
	destAddrPort := net.JoinHostPort(addr, strconv.Itoa(int(dstPort)))
	logInfo := fmt.Sprintf("【来源】%s 【时间】%s 【链接】%s", client.RemoteAddr(), time.Now().Format("2006/1/2 15:04:05"), destAddrPort)

	if cmd == 3 { // UDP ASSOCIATE
		return nil, true, logInfo, handleUDPAssociate(client)
	}

	dest, err := net.DialTimeout("tcp", destAddrPort, 10*time.Second)
	if err != nil {
		_, _ = client.Write([]byte{0x05, 0x05, 0x00, 0x01, 0, 0, 0, 0, 0, 0}) // connection refused
		return nil, false, "", errors.New("dial dst: " + err.Error())
	}
	//0x05（socks5）, 0x00（0成功）, 0x00（预留）, 0x01（地址类型IPv4）, 0,0,0,0, 0,0（端口）
	if _, err := client.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); err != nil {
		dest.Close()
		return nil, false, "", errors.New("write rsp: " + err.Error())
	}
	return dest, false, logInfo, nil
}

// readAddr 按 ATYP 读取地址（支持 IPv4 / 域名 / IPv6）
func readAddr(r io.Reader, buf []byte, atyp byte) (string, error) {
	switch atyp {
	case 1:
		if _, err := io.ReadFull(r, buf[:4]); err != nil {
			return "", errors.New("invalid IPv4: " + err.Error())
		}
		return net.IP(buf[:4]).String(), nil
	case 3:
		if _, err := io.ReadFull(r, buf[:1]); err != nil {
			return "", errors.New("invalid hostname len: " + err.Error())
		}
		addrLen := int(buf[0])
		if _, err := io.ReadFull(r, buf[:addrLen]); err != nil {
			return "", errors.New("invalid hostname: " + err.Error())
		}
		return string(buf[:addrLen]), nil
	case 4:
		if _, err := io.ReadFull(r, buf[:16]); err != nil {
			return "", errors.New("invalid IPv6: " + err.Error())
		}
		return net.IP(buf[:16]).String(), nil
	default:
		return "", errors.New("invalid atyp")
	}
}

// handleUDPAssociate 处理 UDP ASSOCIATE：开启 UDP 中继端口，
// 中继存活期与 TCP 连接绑定（TCP 断开即会话结束）
func handleUDPAssociate(client net.Conn) error {
	uc, err := net.ListenUDP("udp", &net.UDPAddr{})
	if err != nil {
		_, _ = client.Write([]byte{0x05, 0x01, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return errors.New("udp listen: " + err.Error())
	}
	udpAddr := uc.LocalAddr().(*net.UDPAddr)
	// 回复中继地址：优先使用 TCP 连接本端所在接口的 IP，客户端才能找到
	reply := make([]byte, 4, 22)
	reply[0], reply[1], reply[2] = 0x05, 0x00, 0x00
	var localIP net.IP
	if ta, ok := client.LocalAddr().(*net.TCPAddr); ok {
		localIP = ta.IP
	}
	if ip4 := localIP.To4(); ip4 != nil {
		reply[3] = 0x01
		reply = append(reply, ip4...)
	} else {
		reply[3] = 0x04
		if localIP == nil {
			localIP = net.IPv6unspecified
		}
		reply = append(reply, localIP.To16()...)
	}
	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, uint16(udpAddr.Port))
	reply = append(reply, portBytes...)
	if _, err := client.Write(reply); err != nil {
		uc.Close()
		return errors.New("write udp rsp: " + err.Error())
	}
	go udpRelay(uc)
	// 阻塞到 TCP 断开/出错，UDP 会话随之结束
	_, err = io.Copy(io.Discard, client)
	return err
}

// udpRelay 在 客户端<->目标 之间转发 UDP 数据报（RFC 1928 UDP 封装，仅支持 FRAG=0）
func udpRelay(uc *net.UDPConn) {
	defer uc.Close()
	buf := make([]byte, 64*1024)
	var clientAddr *net.UDPAddr
	upBytes, downBytes := 0, 0
	idle := time.Duration(udpTimeout) * time.Second
	for {
		_ = uc.SetReadDeadline(time.Now().Add(idle))
		n, addr, err := uc.ReadFromUDP(buf)
		if err != nil {
			printLog(fmt.Sprintf("【UDP】会话结束 上行:%dB 下行:%dB", upBytes, downBytes))
			return
		}
		if clientAddr == nil {
			clientAddr = addr
		}
		if addr.Port == clientAddr.Port && addr.IP.Equal(clientAddr.IP) {
			// 来自客户端：解封装，转发到目标
			payload, dst, err := parseUDPDatagram(buf[:n])
			if err != nil || dst == nil {
				continue
			}
			if m, err := uc.WriteToUDP(payload, dst); err == nil {
				upBytes += m
			}
		} else {
			// 来自目标：封装后回给客户端
			pkt := buildUDPDatagram(addr, buf[:n])
			if m, err := uc.WriteToUDP(pkt, clientAddr); err == nil {
				downBytes += m
			}
		}
	}
}

// parseUDPDatagram 解析客户端 UDP 封装报文: RSV(2) FRAG(1) ATYP ADDR PORT DATA
func parseUDPDatagram(b []byte) ([]byte, *net.UDPAddr, error) {
	if len(b) < 4 {
		return nil, nil, errors.New("udp datagram too short")
	}
	if b[2] != 0 {
		return nil, nil, errors.New("fragmented datagram not supported") // FRAG 必须为 0
	}
	i := 4
	var ip net.IP
	switch b[3] { // ATYP
	case 1: // IPv4
		if len(b) < i+4 {
			return nil, nil, errors.New("short ipv4")
		}
		ip = net.IP(b[i : i+4])
		i += 4
	case 4: // IPv6
		if len(b) < i+16 {
			return nil, nil, errors.New("short ipv6")
		}
		ip = net.IP(b[i : i+16])
		i += 16
	case 3: // 域名
		if len(b) < i+1 {
			return nil, nil, errors.New("short domain")
		}
		l := int(b[i])
		i++
		if len(b) < i+l {
			return nil, nil, errors.New("short domain")
		}
		host := string(b[i : i+l])
		i += l
		ips, err := net.LookupIP(host)
		if err != nil || len(ips) == 0 {
			return nil, nil, errors.New("resolve domain: " + host)
		}
		ip = ips[0]
	default:
		return nil, nil, errors.New("invalid atyp")
	}
	if len(b) < i+2 {
		return nil, nil, errors.New("short port")
	}
	port := binary.BigEndian.Uint16(b[i : i+2])
	i += 2
	return b[i:], &net.UDPAddr{IP: ip, Port: int(port)}, nil
}

// buildUDPDatagram 把目标回包封装成 socks5 UDP 报文
func buildUDPDatagram(src *net.UDPAddr, payload []byte) []byte {
	pkt := make([]byte, 0, 22+len(payload))
	pkt = append(pkt, 0x00, 0x00, 0x00) // RSV + FRAG=0
	if ip4 := src.IP.To4(); ip4 != nil {
		pkt = append(pkt, 0x01)
		pkt = append(pkt, ip4...)
	} else {
		pkt = append(pkt, 0x04)
		pkt = append(pkt, src.IP.To16()...)
	}
	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, uint16(src.Port))
	pkt = append(pkt, portBytes...)
	return append(pkt, payload...)
}

func WriteFileLog(str string, isAuth int) {
	var authName string
	switch isAuth {
	case 0:
		authName = "auth"
	case 1:
		authName = ""
	case 2:
		authName = "error"
	}
	f1, err1 := OpenFile(time.Now().Format("20060102") + authName + ".log")
	if err1 != nil {
		log.Printf("打开日志文件失败: %v", err1) // ★ 修正：原来用 log.Fatal，写日志失败会直接杀掉整个服务
		return
	}
	defer f1.Close()
	if _, err1 = io.WriteString(f1, str+"\r\n"); err1 != nil {
		log.Printf("写日志失败: %v", err1)
	}
}

func OpenFile(filename string) (*os.File, error) {
	dir := logDir
	if dir == "" {
		dir = "output"
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	// ★ 修正：原来只有 O_APPEND 没有写权限位，部分系统上写入即报错
	return os.OpenFile(filepath.Join(dir, filename), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0666)
}

func printLog(logInfo string) {
	fmt.Println(logInfo)
	if logFlag {
		WriteFileLog(logInfo, 1)
	}
}

// Socks5Forward 双向转发 TCP 数据（阻塞至双向都结束）
func Socks5Forward(client net.Conn, logInfo string, target net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)
	forward := func(dst, src net.Conn, flag bool) {
		defer wg.Done()
		size, _ := io.Copy(dst, src)
		// 半关闭：先排空单向缓冲的数据，再发 EOF，避免 close 触发 RST 丢数据
		if tc, ok := dst.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
		go printLog(fmt.Sprintf("%s 【%s】%d KB %d B", logInfo, map[bool]string{true: "上行", false: "下行"}[flag], size/1024, size%1024))
	}
	go forward(client, target, true)  // target -> client
	go forward(target, client, false) // client -> target
	wg.Wait()
	client.Close()
	target.Close()
}
