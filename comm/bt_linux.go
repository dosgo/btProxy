package comm

import (
	"golang.org/x/sys/unix"
	"os"
	"fmt"
	"net"
)

func parseMAC(macStr string) ([6]byte, error) {
	var b [6]byte
	hw, err := net.ParseMAC(macStr)
	if err != nil {
		return b, err
	}
	// Linux 内核存储蓝牙地址是逆序的 (BD_ADDR)
	for i := 0; i < 6; i++ {
		b[i] = hw[5-i]
	}
	return b, nil
}


func connectByAddr(macAddrStr string) (ReadWriteCloseWithDeadline, error) {
	// 1. 将 MAC 地址字符串转换为 Linux 内核需要的 [6]byte (逆序/小端序)
	// 例如 "AA:BB:CC:DD:EE:FF" -> [0xFF, 0xEE, 0xDD, 0xCC, 0xBB, 0xAA]
	addr, err := parseMAC(macAddrStr)
	if err != nil {
		return nil, err
	}

	// 2. 创建蓝牙 Socket
	// AF_BLUETOOTH = 31
	// SOCK_STREAM = 1
	// BTPROTO_RFCOMM = 3
	for ch := 1; ch <= 5; ch++ {
		fd, err := unix.Socket(unix.AF_BLUETOOTH, unix.SOCK_STREAM, unix.BTPROTO_RFCOMM)
		if err != nil {
			return nil, fmt.Errorf("创建蓝牙 Socket 失败: %v", err)
		}

		// 3. 构建 Linux 的 SockaddrRfcomm 结构体
		// 注意：在 Linux 中，如果你知道 Service UUID，通常需要先通过 SDP 获取 Channel。
		// 但安卓 listenUsingRfcomm 默认通常在 Channel 1。
		sa := &unix.SockaddrRFCOMM{
			Addr:    addr,
			Channel: uint8(ch), // 绝大多数安卓手机默认 SPP 通道是 1
		}

		// 4. 发起连接
		if err := unix.Connect(fd, sa); err == nil {
			rw := os.NewFile(uintptr(fd), "bt_socket")
			return rw, nil
		}
	}

	return nil, fmt.Errorf("无法在任何通道上连接到设备 %s", macAddrStr)
}