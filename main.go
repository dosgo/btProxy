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
	go comm.StartProxy(tcpPort, "94:d3:31:d3:04:f3")
	<-sigChan
	comm.StopProxy()
}
