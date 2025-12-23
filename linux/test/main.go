package main

import (
	"fmt"
	"log"
	"net"
)

func main() {
	const MY_UUID = "00001101-0000-1000-8000-00805f9b34fb"

	// 1. 开启监听 (对应 Android 的 listenUsingInsecureRfcommWithServiceRecord)
	listener, err := ListenRFCOMM(MY_UUID)
	if err != nil {
		log.Fatalf("启动失败: %v", err)
	}
	defer listener.Close()

	fmt.Println("蓝牙 Bridge 已启动，等待连接...")

	isRunning := true
	for isRunning {
		// 2. 阻塞等待连接 (对应 serverSocket.accept())
		btSocket, err := listener.Accept()
		if err != nil {
			fmt.Printf("Accept 错误: %v\n", err)
			break
		}

		fmt.Printf("蓝牙已连接，远端地址: %s\n", btSocket.RemoteAddr())

		// 3. 实例化处理器并启动 (对应你的 MuxHandler)
		// btSocket 已经实现了 net.Conn，包含了 Read/Write
		go func(conn net.Conn) {
			defer conn.Close()
			// BluetoothMuxHandler muxHandler = new BluetoothMuxHandler(conn, conn);
			// muxHandler.start();
			fmt.Println("开始桥接处理...")
			// ... 处理逻辑 ...
		}(btSocket)
	}
}
