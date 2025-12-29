package main

import (
	"dosgo/btProxy/comm/server"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	// 你定义的 UUID
	MY_UUID          = "00001101-0000-1000-8000-00805f9b34fb"
	PROFILE_OBJ_PATH = "/com/dosgo/bluetooth/profile"
)

// BluetoothProfile 实现 org.bluez.Profile1 接口
type BluetoothProfile struct{}

// NewConnection 是核心：当蓝牙连接建立时，BlueZ 会调用此方法并传入 Socket FD
func (p *BluetoothProfile) NewConnection(device dbus.ObjectPath, fd dbus.UnixFD, fdProperties map[string]dbus.Variant) *dbus.Error {
	fmt.Printf("收到新连接，来自设备: %s\n", device)
	conn := NewBluetoothConn(fd)
	// 异步处理桥接逻辑，不要阻塞 D-Bus 回调线程
	go handleBridge(conn)
	return nil
}

func (p *BluetoothProfile) RequestDisconnection(device dbus.ObjectPath) *dbus.Error {
	fmt.Printf("设备断开连接: %s\n", device)
	return nil
}

func (p *BluetoothProfile) Release() *dbus.Error {
	return nil
}

func handleBridge(conn net.Conn) {
	defer conn.Close()
	fmt.Println("蓝牙桥接线程启动...")

	handler := server.NewBluetoothMuxHandler(conn)
	handler.Start()

	// 保持连接
	select {}
}

func main() {
	// 1. 连接到系统总线 (System Bus)
	conn, err := dbus.SystemBus()
	if err != nil {
		log.Fatalf("无法连接到 System Bus: %v", err)
	}
	defer conn.Close()
	// 2. 导出 Profile 对象，供 BlueZ 回调
	profile := &BluetoothProfile{}
	err = conn.Export(profile, PROFILE_OBJ_PATH, "org.bluez.Profile1")
	if err != nil {
		log.Fatalf("导出对象失败: %v", err)
	}

	// 3. 向 BlueZ ProfileManager1 注册该 Profile
	obj := conn.Object("org.bluez", "/org/bluez")
	options := map[string]dbus.Variant{
		"Name":    dbus.MakeVariant("SerialPort"),
		"Role":    dbus.MakeVariant("server"),
		"Channel": dbus.MakeVariant(uint16(1)), // RFCOMM 端口
	}

	err = obj.Call("org.bluez.ProfileManager1.RegisterProfile", 0,
		dbus.ObjectPath(PROFILE_OBJ_PATH), MY_UUID, options).Store()
	if err != nil {
		log.Fatalf("注册 Profile 失败: %v", err)
	}

	fmt.Printf("蓝牙服务已通过 D-Bus 注册，正在监听 UUID: %s\n", MY_UUID)

	// 4. 等待信号退出
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	fmt.Println("正在注销服务...")
	obj.Call("org.bluez.ProfileManager1.UnregisterProfile", 0, dbus.ObjectPath(PROFILE_OBJ_PATH))
}

// BluetoothConn 实现 net.Conn 接口的蓝牙连接
type BluetoothConn struct {
	fd   dbus.UnixFD
	file *os.File
}

// NewBluetoothConn 创建蓝牙连接
func NewBluetoothConn(fd dbus.UnixFD) *BluetoothConn {
	file := os.NewFile(uintptr(fd), "bluetooth-socket")
	return &BluetoothConn{
		fd:   fd,
		file: file,
	}
}

// Read 实现 io.Reader
func (c *BluetoothConn) Read(b []byte) (n int, err error) {
	return c.file.Read(b)
}

// Write 实现 io.Writer
func (c *BluetoothConn) Write(b []byte) (n int, err error) {
	return c.file.Write(b)
}

// Close 实现 io.Closer
func (c *BluetoothConn) Close() error {
	if c.file != nil {
		return c.file.Close()
	}
	// return syscall.Close(c.fd)
	return nil
}

// LocalAddr 返回本地地址（蓝牙连接无此概念）
func (c *BluetoothConn) LocalAddr() net.Addr {
	return &BluetoothAddr{Address: "local-bluetooth"}
}

// RemoteAddr 返回远程地址
func (c *BluetoothConn) RemoteAddr() net.Addr {
	return &BluetoothAddr{Address: "remote-bluetooth"}
}

// SetDeadline 设置截止时间
func (c *BluetoothConn) SetDeadline(t time.Time) error {
	// 对于蓝牙连接，可能需要特殊处理
	return nil // 或不实现
}

// SetReadDeadline 设置读截止时间
func (c *BluetoothConn) SetReadDeadline(t time.Time) error {
	return nil
}

// SetWriteDeadline 设置写截止时间
func (c *BluetoothConn) SetWriteDeadline(t time.Time) error {
	return nil
}

// BluetoothAddr 蓝牙地址实现
type BluetoothAddr struct {
	Address string
}

func (a *BluetoothAddr) Network() string { return "bluetooth" }
func (a *BluetoothAddr) String() string  { return a.Address }
