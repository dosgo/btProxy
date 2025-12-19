package main

import (
	"dosgo/btProxy/comm"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"time"
)

var mux *comm.MuxManager

func main() {
	// 配置
	tcpPort := ":8888"
	//	comPort := "COM4"
	//baudRate := 115200

	// 信号处理
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, os.Kill)

	// 启动 TCP 服务器
	listener, err := net.Listen("tcp", tcpPort)
	if err != nil {
		log.Fatalf("TCP监听失败: %v", err)
	}
	defer listener.Close()

	log.Printf("TCP服务器启动在 %s，等待连接...", tcpPort)

	btRaw := comm.NewConnectBT("94:d3:31:d3:04:f3")

	//多路复用
	mux = comm.NewMuxManager(btRaw)

	for {
		select {
		case <-sigChan:
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
			go handleConnection(tcpConn)
		}
	}
}

func handleConnection(tcpConn net.Conn) {
	defer tcpConn.Close()

	remoteAddr := tcpConn.RemoteAddr().(*net.TCPAddr)
	portID := uint16(remoteAddr.Port)
	serialPort := mux.OpenStream(portID)

	// 使用 io.Copy 进行双向转发
	done := make(chan bool, 2)

	// TCP → 串口
	go func() {
		_, err := io.Copy(serialPort, tcpConn)
		if err != nil {
			log.Printf("TCP→串口转发错误: %v", err)
		}
		done <- true
	}()

	// 串口 → TCP
	go func() {
		_, err := io.Copy(tcpConn, serialPort)
		if err != nil {
			log.Printf("串口→TCP转发错误: %v", err)
		}
		done <- true
	}()

	// 等待任意一个方向完成
	<-done

	// 关闭连接
	tcpConn.Close()
	serialPort.Close()

	// 确保另一个协程也退出
	time.Sleep(100 * time.Millisecond)

	log.Printf("连接断开: %s", tcpConn.RemoteAddr())
}
