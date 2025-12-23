package server

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// BluetoothMuxHandler 对应 Java 版的 BluetoothMuxHandler
type BluetoothMuxHandler struct {
	btConn io.ReadWriteCloser
	// 用于管理逻辑流 ID (端口) 与本地 Socket 的映射
	streamMap sync.Map
	// 控制并发写入的互斥锁
	writeMutex sync.Mutex
	// 关闭信号
	closeChan chan struct{}
}

// NewBluetoothMuxHandler 创建新的 MuxHandler
func NewBluetoothMuxHandler(btConn io.ReadWriteCloser) *BluetoothMuxHandler {
	return &BluetoothMuxHandler{
		btConn:    btConn,
		closeChan: make(chan struct{}),
	}
}

// Start 启动主循环，解析蓝牙发来的封装包
func (h *BluetoothMuxHandler) Start() {
	go func() {
		defer h.cleanup()

		header := make([]byte, 4)
		for {
			select {
			case <-h.closeChan:
				return
			default:
				// 1. 读取 4 字节头部
				if _, err := io.ReadFull(h.btConn, header); err != nil {
					if err != io.EOF {
						fmt.Printf("读取头部错误: %v\n", err)
					}
					return
				}

				// 解析头部
				id := binary.BigEndian.Uint16(header[0:2])
				length := binary.BigEndian.Uint16(header[2:4])

				// 2. 读取 Payload 数据
				payload := make([]byte, length)
				if length > 0 {
					if _, err := io.ReadFull(h.btConn, payload); err != nil {
						fmt.Printf("读取payload错误: %v\n", err)
						return
					}
				}

				// 3. 分发数据
				h.handleStreamData(uint16(id), payload)
			}
		}
	}()
}

// handleStreamData 处理流数据
func (h *BluetoothMuxHandler) handleStreamData(id uint16, data []byte) {
	// 控制命令
	if id == 0 {
		if len(data) < 8 { // 需要至少 8 字节 (id(2) + ip(4) + port(2))
			fmt.Printf("控制命令数据长度不足: %d\n", len(data))
			return
		}

		// 解析控制命令
		realID := binary.BigEndian.Uint16(data[0:2])
		ipBytes := data[2:6]
		port := binary.BigEndian.Uint16(data[6:8])

		// 构建 IP 地址字符串
		ipStr := fmt.Sprintf("%d.%d.%d.%d", ipBytes[0], ipBytes[1], ipBytes[2], ipBytes[3])
		addr := fmt.Sprintf("%s:%d", ipStr, port)

		// 建立 TCP 连接
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			fmt.Printf("建立TCP连接失败: %v\n", err)
			return
		}

		// 存入路由表
		h.streamMap.Store(realID, conn)

		// 启动反向桥接
		go h.startReverseBridge(realID, conn)
		return
	}

	// 数据帧
	if value, exists := h.streamMap.Load(id); exists {
		if conn, ok := value.(net.Conn); ok && conn != nil {
			// 写入数据到对应的 Socket
			if _, err := conn.Write(data); err != nil {
				fmt.Printf("写入Socket失败: %v\n", err)
				h.streamMap.Delete(id)
				conn.Close()
			}
		}
	}
}

// startReverseBridge 反向桥接：读取本地 Socket 数据并打上 ID 头部发回蓝牙
func (h *BluetoothMuxHandler) startReverseBridge(id uint16, conn net.Conn) {
	defer func() {
		h.streamMap.Delete(id)
		conn.Close()
	}()

	buffer := make([]byte, 1024*4)
	for {
		select {
		case <-h.closeChan:
			return
		default:
			// 设置读取超时，避免阻塞无法退出
			conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))

			n, err := conn.Read(buffer)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					// 超时，继续循环
					continue
				}
				// 其他错误，退出
				if err != io.EOF {
					fmt.Printf("读取Socket错误: %v\n", err)
				}
				return
			}

			if n > 0 {
				// 发送数据帧
				if err := h.sendFrame(id, buffer[:n]); err != nil {
					fmt.Printf("发送帧失败: %v\n", err)
					return
				}
			}
		}
	}
}

// sendFrame 封包发送：[ID(2)][Len(2)][Data...]
// 使用互斥锁保证物理写入的原子性，防止多线程写入导致包头交织
func (h *BluetoothMuxHandler) sendFrame(id uint16, data []byte) error {
	h.writeMutex.Lock()
	defer h.writeMutex.Unlock()

	header := make([]byte, 4)
	binary.BigEndian.PutUint16(header[0:2], id)
	binary.BigEndian.PutUint16(header[2:4], uint16(len(data)))

	// 写入头部
	if _, err := h.btConn.Write(header); err != nil {
		return err
	}

	// 写入数据
	if len(data) > 0 {
		if _, err := h.btConn.Write(data); err != nil {
			return err
		}
	}

	return nil
}

// cleanup 清理资源
func (h *BluetoothMuxHandler) cleanup() {
	h.streamMap.Range(func(key, value interface{}) bool {
		if conn, ok := value.(net.Conn); ok {
			conn.Close()
		}
		h.streamMap.Delete(key)
		return true
	})
}

// Close 关闭处理器
func (h *BluetoothMuxHandler) Close() {
	close(h.closeChan)
	h.cleanup()
}

// 辅助函数：创建控制命令帧
func CreateControlFrame(id uint16, ip net.IP, port uint16) []byte {
	// 控制帧格式: [ID(2)][IP(4)][Port(2)]
	frame := make([]byte, 8)
	binary.BigEndian.PutUint16(frame[0:2], id)

	// 确保是 IPv4
	ipv4 := ip.To4()
	if ipv4 != nil {
		copy(frame[2:6], ipv4)
	}

	binary.BigEndian.PutUint16(frame[6:8], port)
	return frame
}
