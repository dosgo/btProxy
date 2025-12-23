package comm

import (
	"fmt"
	"io"
	"log"
	"net"
	"time"
)

var stopChan chan struct{}

func StartProxy(mux *MuxManager, tcpPort string, remoteAddr string) {
	// 启动 TCP 服务器
	listener, err := net.Listen("tcp", tcpPort)
	if err != nil {
		log.Fatalf("TCP监听失败: %v", err)
	}
	defer listener.Close()

	log.Printf("TCP服务器启动在 %s，等待连接...", tcpPort)

	stopChan = make(chan struct{})

	for {
		select {
		case <-stopChan:
			log.Println("收到关闭信号，退出")
			return
		default:
			// 设置超时以便能检查信号
			listener.(*net.TCPListener).SetDeadline(time.Now().Add(1 * time.Second))
			tcpConn, err := listener.Accept()
			if err != nil {
				if opErr, ok := err.(*net.OpError); ok && opErr.Timeout() {
					continue
				}
				log.Printf("接受连接失败: %v", err)
				continue
			}
			log.Printf("客户端连接: %s", tcpConn.RemoteAddr())
			// 处理连接
			go handleConnection(tcpConn, mux, remoteAddr)
		}
	}
}
func StopProxy() {
	if stopChan != nil {
		close(stopChan)
	}
}

func handleConnection(tcpConn net.Conn, mux *MuxManager, toAddr string) {
	defer tcpConn.Close()

	remoteAddr := tcpConn.RemoteAddr().(*net.TCPAddr)
	portID := uint16(remoteAddr.Port)
	serialPort := mux.OpenStream(portID, toAddr)
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
