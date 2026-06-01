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
	"os"
	"runtime"
	"socks5/config"
	"strings"
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

// var userList TODO 对于登录用户，在周期内不进行校验，此时还有一个问题，就是如经路由会有多设备公用同一ip问题（是否试用ip+端口进行配置）
// 试用流控（令牌桶）
func main() {
	flag.StringVar(&c, "c", "", "配置文件")
	//这里也可以注入，如果启用了配置文件也会被覆盖，所以直接写也无妨
	flag.Int64Var(&port, "port", 28000, "端口号")
	flag.IntVar(&core, "core", 0, "线程数量，默认为当前处理器线程的2倍")
	flag.BoolVar(&logFlag, "log", false, "日志,默认关闭")
	flag.BoolVar(&skipPrivateCheck, "skip", false, "局域网免校验,默认关闭")
	flag.StringVar(&username, "u", "admin", "用户名")
	flag.StringVar(&password, "p", "Qwer1234-", "密码")
	flag.Parse()
	if c != "" {
		// 加载配置
		config, err := loadConfig(c)
		if err != nil {
			log.Fatalf("Error loading config: %v", err)
		}
		port = config.Port
		core = config.CoreNum
		logFlag = config.LogFlag
		skipPrivateCheck = config.SkipPrivateCheck
		username = config.User.Username
		password = config.User.Password
	}
	if core == 0 {
		core = runtime.NumCPU() * 2
	}
	fmt.Println(fmt.Sprintf("核心数量: %d ", core))
	server, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		fmt.Printf("Listen failed: %v\n", err)
		return
	}
	var tGroup sync.WaitGroup
	tGroup.Add(core)
	for i := 1; i <= core; i++ {
		fmt.Println(fmt.Sprintf("第【%d】个线程启动", i))
		go multiThread(server, i, tGroup)
	}
	tGroup.Wait()
}
func loadConfig(filename string) (*config.Config, error) {
	// 读取文件内容
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	// 解析 YAML 数据
	var config config.Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal YAML: %w", err)
	}
	return &config, nil
}
func multiThread(listener net.Listener, index int, tGroup sync.WaitGroup) {
	for {
		client, err := listener.Accept()
		if err != nil {
			fmt.Printf("Accept failed: %v\n", err)
			continue
		}
		go process(client, index)
	}
	fmt.Println("【线程】", index, "执行异常！")
	tGroup.Done()
}
func process(client net.Conn, index int) {
	if err := Socks5Auth(client); err != nil { //不是nil说明是存在异常，当前捕获异常并进行处理
		fmt.Println("认证错误:", err)
		if logFlag {
			go WriteFileLog("【ip】"+client.RemoteAddr().String()+"【时间】"+time.Now().Format("2006-01-02 15:04:05")+";认证错误", 2)
		}
		client.Close()
		return
	}
	target, logInfo, err := Socks5Connect(client, index)
	if err != nil { //不是nil说明是存在异常，当前捕获异常并进行处理
		fmt.Println("连接错误:", err)
		if logFlag {
			go WriteFileLog("【ip】"+client.RemoteAddr().String()+"【时间】"+time.Now().Format("2006-01-02 15:04:05")+";连接错误", 2)
		}
		client.Close()
		return
	}
	Socks5Forward(client, logInfo, target)
}
func Socks5Auth(client net.Conn) (err error) {
	buf := make([]byte, 256)
	// 读取 VER 和 NMETHODS
	n, err := io.ReadFull(client, buf[:2])
	if n != 2 {
		return errors.New("reading header: " + err.Error())
	}
	ver, nMethods := int(buf[0]), int(buf[1])
	//只使用socks5协议
	if ver != 5 {
		return errors.New("invalid version")
	}
	// 读取 METHODS 列表
	n, err = io.ReadFull(client, buf[:nMethods])
	if n != nMethods {
		return errors.New("reading methods: " + err.Error())
	}
	//0x05(socks5), 0x00（无需认证）/ 0x02（密码认证）

	if strings.HasPrefix(client.RemoteAddr().String(), "10.10.") && skipPrivateCheck {
		fmt.Println("【内网ip】" + client.RemoteAddr().String() + ";直接放过")
		if logFlag {
			go WriteFileLog("【内网ip】"+client.RemoteAddr().String()+";直接放过", 1)
		}
		n, err = client.Write([]byte{0x05, 0x00})
	} else {
		n, err = client.Write([]byte{0x05, 0x02})
		if n != 2 || err != nil {
			fmt.Println("write rsp err: " + err.Error())
			return errors.New("write rsp err: " + err.Error())
		}
		upBuf := make([]byte, 4096) //
		//0x05(VER)  ULEN(用户名长度1-255字节)  UNAME（用户名） PLEN(密码长度1-255字节)  PASSWD（密码） 总共最长513字节
		n, err = client.Read(upBuf)
		if auth(n, upBuf, client) {
			client.Write([]byte{0x05, 0x00})
		} else {
			client.Write([]byte{0x05, 0x01})
			client.Close()
		}
	}
	return err
}

// 对用户名和密码进行校验
func auth(n int, info []byte, client net.Conn) bool {
	userFlag := info[1]
	pwdFlag := info[2+userFlag]
	if n < 5 { //小于最小字节肯定错误
		return false
	}
	userByte := info[2 : userFlag+2]
	pwdByte := info[userFlag+3 : userFlag+3+pwdFlag]
	userStr := string(userByte)
	pwdStr := string(pwdByte)
	if userStr == username && pwdStr == password {
		fmt.Println(client.RemoteAddr().String() + ":密码正确")
		if logFlag {
			go WriteFileLog(client.RemoteAddr().String()+":密码正确", 0)
		}
		return true
	} else {
		fmt.Println(client.RemoteAddr().String() + ":密码错误")
		if logFlag {
			go WriteFileLog(client.RemoteAddr().String()+":密码错误", 0)
		}
		return false
	}
}
func Socks5Connect(client net.Conn, index int) (net.Conn, string, error) {
	buf := make([]byte, 256)
	n, err := io.ReadFull(client, buf[:4])
	if n != 4 {
		return nil, "", errors.New("read header: " + err.Error())
	}
	//VER sock版本号
	//cmd 0x01=CONNECT, 0x02=BIND, 0x03=UDP ASSOCIATE
	//RSV 保留字段，现在没卵用
	//ATYP 地址类型，0x01=IPv4，0x03=域名，0x04=IPv6
	//DST.ADDR 目标地址
	//DST.PORT 目标端口
	ver, cmd, _, atyp := buf[0], buf[1], buf[2], buf[3]
	if ver != 5 || cmd != 1 { //仅支持 socks5 仅支持CONNECT TODO 未来支持BIND  UDP ASSOCIATE
		return nil, "", errors.New("invalid ver/cmd")
	}
	addr := ""
	switch atyp {
	case 1:
		n, err = io.ReadFull(client, buf[:4])
		if n != 4 {
			return nil, "", errors.New("invalid IPv4: " + err.Error())
		}
		addr = fmt.Sprintf("%d.%d.%d.%d", buf[0], buf[1], buf[2], buf[3])
	case 3:
		n, err = io.ReadFull(client, buf[:1])
		if n != 1 {
			return nil, "", errors.New("invalid hostname: " + err.Error())
		}
		addrLen := int(buf[0])
		n, err = io.ReadFull(client, buf[:addrLen])
		if n != addrLen {
			return nil, "", errors.New("invalid hostname: " + err.Error())
		}
		addr = string(buf[:addrLen])
	case 4:
		return nil, "", errors.New("IPv6: no supported yet")
	default:
		return nil, "", errors.New("invalid atyp")
	}
	n, err = io.ReadFull(client, buf[:2])
	if n != 2 {
		return nil, "", errors.New("read port: " + err.Error())
	}
	port := binary.BigEndian.Uint16(buf[:2])
	destAddrPort := fmt.Sprintf("%s:%d", addr, port)
	logInfo := fmt.Sprintf("【线程】%d 【来源】%s 【时间】%s 【链接】%s", index, client.RemoteAddr(), time.Now().Format("2006/1/2 15:04:05"), destAddrPort)
	dest, err := net.Dial("tcp", destAddrPort)
	if err != nil {
		return nil, "", errors.New("dial dst: " + err.Error())
	}
	//0x05（socks5）, 0x00（0成功 1失败）, 0x00（预留）, 0x01（地址类型）, 0（ipv4-1）, 0（ipv4-2）, 0（ipv4-3）, 0（ipv4-4）, 0（port-1）, 0（port-2）
	n, err = client.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
	if err != nil {
		dest.Close()
		return nil, "", errors.New("write rsp: " + err.Error())
	}
	return dest, logInfo, nil
}
func WriteFileLog(str string, isAuth int) {
	/******************* 使用 io.WriteString 写入文件 **********************/
	var authName string = ""
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
		log.Fatal(err1.Error())
	}
	defer f1.Close()
	_, err1 = io.WriteString(f1, str+"\r\n") //写入文件(字符串)
	if err1 != nil {
		log.Fatal(err1.Error())
	}
}
func OpenFile(filename string) (*os.File, error) {
	//_, dir, _, _ := runtime.Caller(1)
	dir := "./"
	if !strings.HasSuffix(dir, "/") {
		dir = dir + "/"
	}
	dir = dir + "output"
	if !isExist(dir) {
		createFile(dir)
	}
	fileAllName := dir + "/" + filename
	if _, err := os.Stat(fileAllName); os.IsNotExist(err) {
		//fmt.Println("文件不存在")
		return os.Create(fileAllName) //创建文件
	}
	//fmt.Println("文件存在")
	return os.OpenFile(fileAllName, os.O_APPEND, 0666) //打开文件
}

// 调用os.MkdirAll递归创建文件夹
func createFile(filePath string) error {
	if !isExist(filePath) {
		err := os.MkdirAll(filePath, os.ModePerm)
		return err
	}
	return nil
}

// 判断所给路径文件/文件夹是否存在(返回true是存在)
func isExist(path string) bool {
	_, err := os.Stat(path) //os.Stat获取文件信息
	if err != nil {
		if os.IsExist(err) {
			return true
		}
		return false
	}
	return true
}
func printLog(logInfo string, flag bool, size int) {
	if flag {
		logInfo = fmt.Sprintf(logInfo+" 【上行】%d KB %d B \n", size/1024, size%1024)
	} else {
		logInfo = fmt.Sprintf(logInfo+" 【下行】%d KB %d B \n", size/1024, size%1024)
	}
	fmt.Println(logInfo)
	if logFlag {
		WriteFileLog(logInfo, 1)
	}
}
func Socks5Forward(client net.Conn, logInfo string, target net.Conn) {
	forward := func(src, dest net.Conn, flag bool) {
		defer src.Close()
		defer dest.Close()
		//size 字节数
		size, _ := io.Copy(src, dest)
		go printLog(logInfo, flag, int(size))
	}
	go forward(client, target, true)
	go forward(target, client, false)
}
