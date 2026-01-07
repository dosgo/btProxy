package cellularvpn

/*
#cgo LDFLAGS: -landroid
#include <android/multinetwork.h>
#include <sys/socket.h>
void bind_socket_to_network(int fd, long handle) {
    if (__builtin_available(android 23, *)) {
        android_setsocknetwork((net_handle_t)handle, fd);
    }
}
*/
import "C"

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"syscall"
	"time"

	_ "golang.org/x/mobile/bind"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/link/fdbased"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv6"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
	"gvisor.dev/gvisor/pkg/waiter"
)

var (
	currStack  *stack.Stack
	stopCtx    context.Context
	stopCancel context.CancelFunc
	mu         sync.Mutex // 保证启动和关闭的原子性
)

// android build   gomobile bind -androidapi=23 -target=android -ldflags "-checklinkname=0 -s -w"
func StartStack(fd int, netHandle int64) {

	mu.Lock()
	defer mu.Unlock()

	if currStack != nil {
		internalStop()
	}
	// 2. 初始化全局停止控制
	stopCtx, stopCancel = context.WithCancel(context.Background())

	s := stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{ipv4.NewProtocol, ipv6.NewProtocol},
		TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol, udp.NewProtocol},
	})
	currStack = s

	ep, _ := fdbased.New(&fdbased.Options{
		FDs:            []int{fd},
		MTU:            1500,
		EthernetHeader: false,
	})

	nicID := tcpip.NICID(1)
	s.CreateNIC(nicID, ep)

	addr := tcpip.AddrFrom4([4]byte{10, 0, 0, 1})
	s.AddProtocolAddress(nicID, tcpip.ProtocolAddress{
		Protocol:          ipv4.ProtocolNumber,
		AddressWithPrefix: addr.WithPrefix(),
	}, stack.AddressProperties{})
	s.SetPromiscuousMode(nicID, true) // 混杂模式：接受目标 IP 不是自己的包
	s.SetSpoofing(nicID, true)

	// 修正1：使用正确的 Subnet 构建方式
	defaultRoute, _ := tcpip.NewSubnet(tcpip.AddrFrom4([4]byte{0, 0, 0, 0}), tcpip.MaskFrom("\x00\x00\x00\x00"))
	s.SetRouteTable([]tcpip.Route{
		{Destination: defaultRoute, NIC: nicID},
	})
	fmt.Println("StartStack: 路由表设置完成")
	go handleTCP(s, netHandle, stopCtx)
	go handleUDP(s, netHandle, stopCtx)
}

func StopStack() {
	mu.Lock()
	defer mu.Unlock()
	internalStop()
}

func internalStop() {
	if stopCancel != nil {
		stopCancel() // 通知所有 handle 协程退出
	}
	if currStack != nil {
		// 移除所有网卡并关闭栈，这会强制中断现有的 Endpoint
		currStack.Close()
		currStack = nil
	}
	fmt.Println("StartStack: 协议栈已优雅关闭")
}

func handleTCP(s *stack.Stack, netHandle int64, ctx context.Context) {
	fmt.Printf("handleTCP: netHandle=%d\n", netHandle)
	f := tcp.NewForwarder(s, 0, 1024, func(r *tcp.ForwarderRequest) {
		var wq waiter.Queue
		ep, tcpipErr := r.CreateEndpoint(&wq)
		if tcpipErr != nil {
			// 如果创建失败，返回 false 让协议栈决定是否由其他 handler 处理或丢弃
			return
		}
		r.Complete(false)
		inConn := gonet.NewTCPConn(&wq, ep)
		defer inConn.Close()

		dialer := &net.Dialer{
			Control: func(network, address string, c syscall.RawConn) error {
				return c.Control(func(fd uintptr) {
					if netHandle > 0 {
						C.bind_socket_to_network(C.int(fd), C.long(netHandle))
					}
				})
			},
		}
		// 目标地址直接通过 inConn 拿到
		outConn, err := dialer.Dial("tcp", inConn.LocalAddr().String())
		if err != nil {
			return
		}
		defer outConn.Close()

		go io.Copy(inConn, outConn)
		go func() {
			<-ctx.Done()
			inConn.Close()
			outConn.Close()
		}()
		io.Copy(outConn, inConn)
	})
	s.SetTransportProtocolHandler(tcp.ProtocolNumber, f.HandlePacket)
}

func handleUDP(s *stack.Stack, netHandle int64, ctx context.Context) {
	fmt.Printf("handleUDP: netHandle=%d\n", netHandle)
	// 修正：增加 bool 返回值以匹配 ForwarderHandler 签名
	f := udp.NewForwarder(s, func(r *udp.ForwarderRequest) bool {
		var wq waiter.Queue
		ep, tcpipErr := r.CreateEndpoint(&wq)
		if tcpipErr != nil {
			// 如果创建失败，返回 false 让协议栈决定是否由其他 handler 处理或丢弃
			return false
		}

		inConn := gonet.NewUDPConn(&wq, ep)

		go func() {
			defer inConn.Close()
			target := inConn.LocalAddr().String()

			dialer := &net.Dialer{
				Control: func(network, address string, c syscall.RawConn) error {
					return c.Control(func(fd uintptr) {
						if netHandle > 0 {
							C.bind_socket_to_network(C.int(fd), C.long(netHandle))
						}
					})
				},
			}

			realOutConn, stdErr := dialer.Dial("udp", target)
			if stdErr != nil {
				return
			}
			defer realOutConn.Close()

			ctx, cancel := context.WithCancel(context.Background())
			// 读取回包数据
			go func() {
				buf := make([]byte, 2048)
				for {
					n, readErr := realOutConn.Read(buf)
					if readErr != nil {
						break
					}
					inConn.Write(buf[:n])
				}
				cancel()
			}()
			go func() {
				<-ctx.Done()
				inConn.Close()
				realOutConn.Close()
			}()

			// 发送请求数据
			buf := make([]byte, 2048)
			for {
				select {
				case <-ctx.Done():
					return
				default:
					inConn.SetReadDeadline(time.Now().Add(60 * time.Second))
					n, readErr := inConn.Read(buf)
					if readErr != nil {
						return
					}
					realOutConn.Write(buf[:n])
				}
			}
		}()

		return true // 明确告诉协议栈，这个 UDP 请求我们已经接管了
	})
	s.SetTransportProtocolHandler(udp.ProtocolNumber, f.HandlePacket)
}
