package comm

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

var modws2_32 = windows.NewLazySystemDLL("ws2_32.dll")
var procConnect = modws2_32.NewProc("connect")

func connectByAddr(macAddrStr string) (ReadWriteCloseWithDeadline, error) {
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
