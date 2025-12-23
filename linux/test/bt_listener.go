package main

import (
	"fmt"
	"net"
	"os"
	"sync"

	"github.com/godbus/dbus/v5"
)

// RFCOMMListener 实现了 net.Listener 接口
type RFCOMMListener struct {
	conn       *dbus.Conn
	path       dbus.ObjectPath
	uuid       string
	acceptChan chan net.Conn
	errChan    chan error
	closed     bool
	mu         sync.Mutex
}

// 内部 Profile 对象，用于导出给 D-Bus 回调
type bluetoothProfile struct {
	l *RFCOMMListener
}

// NewConnection 是由 BlueZ 调用的回调函数
func (p *bluetoothProfile) NewConnection(device dbus.ObjectPath, fd dbus.UnixFD, fdProperties map[string]dbus.Variant) *dbus.Error {
	// 将 UnixFD 转换为 net.Conn
	file := os.NewFile(uintptr(fd), "rfcomm-socket")
	conn, err := net.FileConn(file)
	if err != nil {
		return dbus.MakeFailedError(err)
	}

	// 发送到 Accept 队列
	p.l.acceptChan <- conn
	return nil
}

func (p *bluetoothProfile) RequestDisconnection(device dbus.ObjectPath) *dbus.Error { return nil }
func (p *bluetoothProfile) Release() *dbus.Error                                    { return nil }

// ListenRFCOMM 创建并注册一个蓝牙 RFCOMM 监听器
func ListenRFCOMM(uuid string) (*RFCOMMListener, error) {
	dconn, err := dbus.SystemBus()
	if err != nil {
		return nil, err
	}

	path := dbus.ObjectPath("/com/bridge/profile/" + os.Getenv("USER")) // 唯一路径
	l := &RFCOMMListener{
		conn:       dconn,
		path:       path,
		uuid:       uuid,
		acceptChan: make(chan net.Conn),
		errChan:    make(chan error, 1),
	}

	// 1. 导出对象
	p := &bluetoothProfile{l: l}
	err = dconn.Export(p, path, "org.bluez.Profile1")
	if err != nil {
		return nil, err
	}

	// 2. 向 BlueZ 注册 Profile
	obj := dconn.Object("org.bluez", "/org/bluez")
	options := map[string]dbus.Variant{
		"Name":    dbus.MakeVariant("GoBluetoothBridge"),
		"Role":    dbus.MakeVariant("server"),
		"Channel": dbus.MakeVariant(uint16(1)), // RFCOMM 默认通道
	}

	err = obj.Call("org.bluez.ProfileManager1.RegisterProfile", 0, path, uuid, options).Store()
	if err != nil {
		return nil, fmt.Errorf("RegisterProfile failed: %v", err)
	}

	return l, nil
}

// Accept 阻塞等待直到新的蓝牙连接进入
func (l *RFCOMMListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.acceptChan:
		return conn, nil
	case err := <-l.errChan:
		return nil, err
	}
}

// Close 注销 Profile 并关闭连接
func (l *RFCOMMListener) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true

	// 注销服务
	obj := l.conn.Object("org.bluez", "/org/bluez")
	l.conn.Export(nil, l.path, "org.bluez.Profile1") // 移除导出
	obj.Call("org.bluez.ProfileManager1.UnregisterProfile", 0, l.path)

	close(l.acceptChan)
	l.errChan <- fmt.Errorf("listener closed")
	return l.conn.Close()
}

func (l *RFCOMMListener) Addr() net.Addr {
	return &btAddr{uuid: l.uuid}
}

type btAddr struct{ uuid string }

func (a *btAddr) Network() string { return "bluetooth_rfcomm" }
func (a *btAddr) String() string  { return a.uuid }
