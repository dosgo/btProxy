package comm

import (
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/tarm/serial"
)

type ReadWriteCloseWithDeadline interface {
	// 读写关闭
	io.ReadWriteCloser
	// 超时控制
	SetDeadline(t time.Time) error
	SetReadDeadline(t time.Time) error
	SetWriteDeadline(t time.Time) error
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
	conn       ReadWriteCloseWithDeadline
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
			//	currConn.SetReadDeadline(time.Now().Add(5 * time.Second))
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
			currConn.SetWriteDeadline(time.Now().Add(5 * time.Second))
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
