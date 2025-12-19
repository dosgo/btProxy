package comm

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"sync"
	"time"
	"unsafe"

	"github.com/tarm/serial"
	"golang.org/x/sys/windows"
)

var modws2_32 = windows.NewLazySystemDLL("ws2_32.dll")
var procConnect = modws2_32.NewProc("connect")

func connectByAddr(macAddrStr string) (io.ReadWriteCloser, error) {
	macAddr, err := macToUint64(macAddrStr)
	if err != nil {
		return nil, err
	}
	guid, _ := windows.GUIDFromString("{00001101-0000-1000-8000-00805F9B34FB}")

	// 1. 创建蓝牙 Socket
	fd, err := windows.Socket(32, 1, 3) // AF_BTH, SOCK_STREAM, BTHPROTO_RFCOMM
	if err != nil {
		return nil, err
	}

	// 2. 手动构建 30 字节的 SOCKADDR_BTH 结构，避开 Go 的自动对齐填充
	// 布局: Family(2) + Addr(8) + GUID(16) + Port(4) = 30 bytes
	rawSa := make([]byte, 30)

	// Family: AF_BTH (32)
	*(*uint16)(unsafe.Pointer(&rawSa[0])) = 32
	// Addr: 蓝牙 MAC 地址
	*(*uint64)(unsafe.Pointer(&rawSa[2])) = macAddr
	// UUID: 蓝牙服务的 GUID
	*(*windows.GUID)(unsafe.Pointer(&rawSa[10])) = guid
	// Port: 0 (对于 RFCOMM 必须设为 0，以便通过 GUID 自动查询频道)
	*(*uint32)(unsafe.Pointer(&rawSa[26])) = 0

	// 3. 发起连接
	ptr := uintptr(unsafe.Pointer(&rawSa[0]))
	// 注意长度参数必须是 30
	r1, _, err := procConnect.Call(uintptr(fd), ptr, uintptr(30))
	if r1 != 0 {
		windows.Closesocket(fd)
		return nil, fmt.Errorf("Winsock connect 失败: %v", err)
	}

	// 4. 将句柄封装为 Go 文件对象
	rw := os.NewFile(uintptr(fd), "bt_socket")
	return rw, nil
}

func macToUint64(macStr string) (uint64, error) {
	hw, err := net.ParseMAC(macStr)
	if err != nil {
		return 0, err
	}

	var result uint64
	// 修改位移逻辑：第一个字节 hw[0] 移到高位
	for i := 0; i < 6; i++ {
		result = (result << 8) | uint64(hw[i])
	}
	return result, nil
}

func connectByCom(comName string, baud int) (io.ReadWriteCloser, error) {
	c := &serial.Config{Name: comName, Baud: baud} // COM号看你系统分配的  //COM4 115200
	serialPort, err := serial.OpenPort(c)
	if err != nil {
		return nil, err
	}
	return serialPort, nil
}

func NewConnectBT(macAddrStr string) *ConnectBT {
	return &ConnectBT{macAddrStr: macAddrStr}
}

type ConnectBT struct {
	macAddrStr string
	conn       io.ReadWriteCloser
	mu         sync.Mutex // 保护 conn 的并发访问和重连过程
}

// 你的原始连接逻辑封装
func (a *ConnectBT) connect() error {
	btRaw, err := connectByAddr(a.macAddrStr)
	if err != nil {
		return err
	}
	a.conn = btRaw
	return nil
}

// 实现 io.Reader
func (a *ConnectBT) Read(p []byte) (n int, err error) {
	for {
		a.mu.Lock()
		currConn := a.conn
		a.mu.Unlock()

		if currConn != nil {
			n, err = currConn.Read(p)
			if err == nil || err == io.EOF {
				return n, err
			}
			log.Printf("蓝牙读取失败: %v, 准备重连...", err)
			currConn.Close()
			a.conn = nil
		}

		// 执行重连逻辑
		if err := a.reconnect(); err != nil {
			time.Sleep(1 * time.Second) // 重连失败避退
			continue
		}
	}
}

// 实现 io.Writer
func (a *ConnectBT) Write(p []byte) (n int, err error) {
	for {
		a.mu.Lock()
		currConn := a.conn
		a.mu.Unlock()

		if currConn != nil {
			n, err = currConn.Write(p)
			if err == nil {
				return n, nil
			}
			log.Printf("蓝牙写入失败: %v, 准备重连...", err)
			currConn.Close()
			a.conn = nil
		}

		if err := a.reconnect(); err != nil {
			time.Sleep(1 * time.Second)
			continue
		}
	}
}

// 内部重连方法
func (a *ConnectBT) reconnect() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	log.Printf("正在尝试重连到蓝牙: %s", a.macAddrStr)
	err := a.connect()
	if err != nil {
		log.Printf("重连失败: %v", err)
		return err
	}
	log.Println("蓝牙重连成功")
	return nil
}

func (a *ConnectBT) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.conn != nil {
		return a.conn.Close()
	}
	return nil
}
