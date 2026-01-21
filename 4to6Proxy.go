package main

import (
	"flag"
	"io"
	"log"
	"net"
	"os"
	"strings"

	"golang.org/x/net/proxy"
)

func main() {
	proxyPtr := flag.String("proxy", "172.30.16.156:8880", "SOCKS5 proxy address")
	flag.Parse()

	args := flag.Args()
	if len(args) < 2 {
		log.Fatal("Usage: ssh-proxy [-proxy addr:port] <target_host> <target_port>")
	}

	targetHost := args[0]
	targetPort := args[1]

	// 1. 清理数据：去掉可能已经存在的方括号，防止重复添加
	targetHost = strings.Trim(targetHost, "[]")

	// 2. 重新组合：net.JoinHostPort 会根据是否包含冒号自动处理方括号
	// 如果是 IPv6，它会返回 [240e:...]:60022
	targetAddr := net.JoinHostPort(targetHost, targetPort)

	dialer, err := proxy.SOCKS5("tcp", *proxyPtr, nil, proxy.Direct)
	if err != nil {
		log.Fatalf("Failed to create proxy dialer: %v", err)
	}

	// 打印一下最终生成的地址，方便调试（正式使用时可以删掉）
	// log.Printf("Connecting to %s via %s", targetAddr, *proxyPtr)

	conn, err := dialer.Dial("tcp", targetAddr)
	if err != nil {
		log.Fatalf("Failed to connect to %s via proxy: %v", targetAddr, err)
	}
	defer conn.Close()

	done := make(chan struct{}, 2)
	go func() {
		io.Copy(conn, os.Stdin)
		done <- struct{}{}
	}()
	go func() {
		io.Copy(os.Stdout, conn)
		done <- struct{}{}
	}()

	<-done
}
