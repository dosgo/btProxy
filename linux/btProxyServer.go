package main

import (
	"dosgo/btProxy/comm/server"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/godbus/dbus/v5"
)

const (
	// 你定义的 UUID
	MY_UUID          = "00001101-0000-1000-8000-00805f9b34fb"
	PROFILE_OBJ_PATH = "/com/example/bluetooth/profile"
)

// BluetoothProfile 实现 org.bluez.Profile1 接口
type BluetoothProfile struct{}

// NewConnection 是核心：当蓝牙连接建立时，BlueZ 会调用此方法并传入 Socket FD
func (p *BluetoothProfile) NewConnection(device dbus.ObjectPath, fd dbus.UnixFD, fdProperties map[string]dbus.Variant) *dbus.Error {
	fmt.Printf("收到新连接，来自设备: %s\n", device)

	// 将 UnixFD 转换为 Go 的 os.File，进而转为 net.Conn
	file := os.NewFile(uintptr(fd), "rfcomm-socket")
	conn, err := net.FileConn(file)
	if err != nil {
		fmt.Printf("无法创建连接对象: %v\n", err)
		return dbus.MakeFailedError(err)
	}

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
