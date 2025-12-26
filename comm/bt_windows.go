package comm

import (
	"fmt"
	"io"
	"os"
	"syscall"
	"time"
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
	//return &RawBtSocket{fd: fd}, nil
}

type RawBtSocket struct {
	fd windows.Handle
}

func (s *RawBtSocket) Read(p []byte) (int, error) {
	if s.fd == windows.InvalidHandle {
		return 0, io.EOF
	}

	// 构造 windows.Buf 供 syscall 使用
	var buf windows.WSABuf
	buf.Len = uint32(len(p))
	if buf.Len == 0 {
		return 0, nil
	}
	buf.Buf = &p[0]

	var done uint32
	var flags uint32
	// 调用 WSARecv，这是 Windows 处理 Socket 读取最底层的推荐方式
	err := windows.WSARecv(windows.Handle(s.fd), &buf, 1, &done, &flags, nil, nil)
	if err != nil {
		// 如果返回 WSAEWOULDBLOCK，说明暂时没数据
		if err == windows.WSAEWOULDBLOCK {
			return 0, nil
		}
		return 0, err
	}

	// 如果 done 为 0，代表对端关闭了连接
	if done == 0 {
		return 0, io.EOF
	}

	return int(done), nil
}

func (s *RawBtSocket) Write(p []byte) (int, error) {
	if s.fd == windows.InvalidHandle {
		return 0, syscall.EINVAL
	}

	var totalSent int
	for totalSent < len(p) {
		var done uint32
		var buf windows.WSABuf
		remaining := p[totalSent:]
		buf.Len = uint32(len(remaining))
		buf.Buf = &remaining[0]

		// 调用 WSASend，确保它是按照 Socket 协议发送
		err := windows.WSASend(windows.Handle(s.fd), &buf, 1, &done, 0, nil, nil)
		if err != nil {
			if err == windows.WSAEWOULDBLOCK {
				// 缓冲区满了，稍微等一下
				continue
			}
			return totalSent, err
		}

		if done == 0 {
			return totalSent, io.ErrUnexpectedEOF
		}
		totalSent += int(done)
	}
	return totalSent, nil
}

func (s *RawBtSocket) Close() error {
	if s.fd != windows.InvalidHandle {
		windows.Closesocket(s.fd)
		s.fd = windows.InvalidHandle
	}
	return nil
}

// 模拟 Deadline (可选，蓝牙 Socket 建议用 SetSockOpt 设置超时)
func (s *RawBtSocket) SetReadDeadline(t time.Time) error {
	// 实际上可以通过 windows.SetsockoptInt 设置 SO_RCVTIMEO
	return nil
}
func (s *RawBtSocket) SetDeadline(t time.Time) error {
	// 实际上可以通过 windows.SetsockoptInt 设置 SO_RCVTIMEO
	return nil
}

func (s *RawBtSocket) SetWriteDeadline(t time.Time) error {
	// 实际上可以通过 windows.SetsockoptInt 设置 SO_RCVTIMEO
	return nil
}
