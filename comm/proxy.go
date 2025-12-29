package comm

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
)

var stopChans sync.Map

func StartPortProxy(mux *MuxManager, tcpPort string, remoteAddr string) {
	// 启动 TCP 服务器
	listener, err := net.Listen("tcp", tcpPort)
	if err != nil {
		log.Fatalf("TCP监听失败: %v", err)
	}
	log.Printf("TCP服务器启动在 %s，等待连接...", tcpPort)
	stopChans.Store(tcpPort, listener)

	for {
		tcpConn, err := listener.Accept()
		if err != nil {
			break
		}
		log.Printf("%s客户端连接: %s", tcpPort, tcpConn.RemoteAddr())
		// 处理连接
		go handleConnection(tcpConn, mux, remoteAddr)
	}
}
func StopProxy(tcpPort string) {
	if value, ok := stopChans.Load(tcpPort); ok {
		if listener, ok := value.(net.Listener); ok {
			listener.Close()
		}
	}
}

func handleConnection(tcpConn net.Conn, mux *MuxManager, toAddr string) {
	defer tcpConn.Close()
	serialPort := mux.OpenStream(toAddr)
	if serialPort == nil {
		fmt.Println("无法打开流")
		return
	}
	defer serialPort.Close()
	// TCP → 串口
	go func() {
		_, err := io.Copy(serialPort, tcpConn)
		if err != nil {
			log.Printf("TCP→串口转发错误: %v", err)
		}
	}()

	_, err := io.Copy(tcpConn, serialPort)
	if err != nil {
		log.Printf("串口→TCP转发错误: %v", err)
	}
	log.Printf("连接断开: %s", tcpConn.RemoteAddr())
}

/*socks5*/
func StartSocksProxy(mux *MuxManager, socksPort string) {
	listener, err := net.Listen("tcp", socksPort)
	if err != nil {
		log.Fatalf("SOCKS5 监听失败: %v", err)
	}

	log.Printf("SOCKS5 代理服务器启动在 %s", socksPort)
	stopChans.Store(socksPort, listener)
	for {
		tcpConn, err := listener.Accept()
		if err != nil {
			log.Printf("接受连接失败: %v", err)
			continue
		}
		// 每个连接进入独立的处理逻辑
		go handleSocksConnection(tcpConn, mux)
	}
}

func handleSocksConnection(conn net.Conn, mux *MuxManager) {
	defer conn.Close()

	// --- 1. SOCKS5 认证握手 ---
	// 客户端发送: [VER, NMETHODS, METHODS] (通常是 05 01 00)
	buf := make([]byte, 257)
	if _, err := io.ReadFull(conn, buf[:2]); err != nil {
		return
	}
	nMethods := buf[1]
	if _, err := io.ReadFull(conn, buf[:nMethods]); err != nil {
		return
	}
	// 回应客户端: 无需认证 (05 00)
	conn.Write([]byte{0x05, 0x00})

	// --- 2. 解析客户端请求地址 ---
	// 客户端发送: [VER, CMD, RSV, ATYP, ADDR, PORT]
	if _, err := io.ReadFull(conn, buf[:4]); err != nil {
		return
	}

	var destAddr string
	atyp := buf[3] // 地址类型

	switch atyp {
	case 0x01: // IPv4
		if _, err := io.ReadFull(conn, buf[:4]); err != nil {
			return
		}
		destAddr = net.IP(buf[:4]).String()
	case 0x03: // 域名
		if _, err := io.ReadFull(conn, buf[:1]); err != nil {
			return
		}
		addrLen := buf[0]
		if _, err := io.ReadFull(conn, buf[:addrLen]); err != nil {
			return
		}
		destAddr = string(buf[:addrLen])
	case 0x04: // IPv6
		if _, err := io.ReadFull(conn, buf[:16]); err != nil {
			return
		}
		destAddr = net.IP(buf[:16]).String()
	default:
		return
	}

	// 读取端口 (2字节)
	if _, err := io.ReadFull(conn, buf[:2]); err != nil {
		return
	}
	destPort := binary.BigEndian.Uint16(buf[:2])

	fullTarget := net.JoinHostPort(destAddr, fmt.Sprintf("%d", destPort))

	// --- 3. 建立 Mux 流 ---
	// 这里的 portID 依然可以用本地随机端口，或者自定义逻辑
	serialPort := mux.OpenStream(fullTarget)
	// 注意：确保你的 mux.OpenStream 内部使用了你之前写的带有 flag 的 packPayload

	if serialPort == nil {
		// 告诉客户端连接失败 (SOCKS5 响应: 05 01 00 ...)
		conn.Write([]byte{0x05, 0x01, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	defer serialPort.Close()

	// 告诉客户端连接成功 (SOCKS5 响应: 05 00 00 01 ...)
	conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})

	// --- 4. 双向转发 ---

	go func() {
		io.Copy(serialPort, conn)
	}()

	io.Copy(conn, serialPort)

	localAddr := conn.RemoteAddr().(*net.TCPAddr)
	log.Printf("SOCKS5 代理流关闭: %s -> %s:%d", localAddr, destAddr, destPort)
}
