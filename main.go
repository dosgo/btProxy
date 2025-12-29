package main

import (
	"dosgo/btProxy/comm"
	"os"
	"os/signal"
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

	btRaw := comm.NewConnectBT("94:d3:31:d3:04:f3")
	//多路复用
	mux := comm.NewMuxManager(btRaw)
	defer mux.CloseBt()
	go comm.StartPortProxy(mux, tcpPort, "127.0.0.1:8023")
	go comm.StartSocksProxy(mux, ":8090")
	<-sigChan
	comm.StopProxy(tcpPort)
}
