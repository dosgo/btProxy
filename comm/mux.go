package comm

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"bytes"
	"sync"
	"time"
)

// MuxManager 负责管理那个唯一的蓝牙物理连接
type MuxManager struct {
	physical        io.ReadWriteCloser
	streams         map[uint16]chan []byte // 每个ID对应一个数据管道
	streamsLastTime sync.Map
	mu              sync.RWMutex
	writeMu         sync.Mutex // 物理写锁，保证Header和Data不被拆散
}

func NewMuxManager(p io.ReadWriteCloser) *MuxManager {
	m := &MuxManager{
		physical: p,
		streams:  make(map[uint16]chan []byte),
	}
	go m.readLoop() // 启动后台“拆包”协程
	go m.checkActive()
	return m
}

// readLoop 在底层做手脚：解析 ID，把数据塞进正确的通道
func (m *MuxManager) readLoop() {
	header := make([]byte, 4) // 1字节ID + 2字节长度
	for {
		if _, err := io.ReadFull(m.physical, header); err != nil {
			fmt.Printf("Mux读取头部失败: %v，等待重试...\n", err)
			time.Sleep(time.Second * 1)
			continue // 不要 return，继续循环等待 ConnectBT 重连成功
		}

		id := binary.BigEndian.Uint16(header[0:2])
		dataLen := binary.BigEndian.Uint16(header[2:4])
		if dataLen > 2048 {
			fmt.Printf("数据帧长度错误: %d\n", dataLen)
			m.physical.Close()
			time.Sleep(time.Second * 1)
			continue
		}
		payload := make([]byte, dataLen)
		//fmt.Printf("id:%d dataLen:%d payloadLen:%d\r\n", id, dataLen, len(payload))
		if _, err := io.ReadFull(m.physical, payload); err != nil {
			fmt.Printf("Mux读取载荷失败: %v\n", err)
			continue
		}

		m.mu.RLock()
		if ch, ok := m.streams[id]; ok {
			m.streamsLastTime.Store(id, time.Now().Unix())
			select {
			case ch <- payload:
				// 成功
			case <-time.After(time.Millisecond * 200):
				// 半秒钟还没发进去，说明这个流彻底堵死了，关闭或记录错误
				fmt.Printf("警告: 流 %d 阻塞超时，丢弃数据包\r\n", id)
			}
		}
		m.mu.RUnlock()
	}
}

// OpenStream 是关键：它返回一个类似流的对象，侵入性极小
func (m *MuxManager) OpenStream(id uint16, remoteAddr string) io.ReadWriteCloser {
	ch := make(chan []byte, 1024)
	m.mu.Lock()
	m.streams[id] = ch
	m.mu.Unlock()
	host, portStr, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return nil
	}
	port, _ := strconv.Atoi(portStr)
	// 解析 IP 地址
	ip := net.ParseIP(host)
	if ip == nil {
		return nil
	}

	payload := new(bytes.Buffer)
	//id
	binary.Write(payload, binary.BigEndian, id)
	// 判断是 IPv4 还是 IPv6
	if ip4 := ip.To4(); ip4 != nil {
		// IPv4: 2(id) + 4(ip) + 2(port) = 8 bytes
		payload.Write(ip4)
	} else {
		// IPv6: 2(id) + 16(ip) + 2(port) = 20 bytes (注：有些协议习惯对齐，这里按实长 20 字节)
		payload.Write(ip.To16())
	}
	//port
	binary.Write(payload, binary.BigEndian, uint16(port))

	//包头的id是0表示新连接
	if _, err := m.writePacket(0, payload.Bytes()); err != nil {
		return nil
	}

	return &VirtualConn{id: id, manager: m, readCh: ch}
}

func (m *MuxManager) writePacket(id uint16, data []byte) (int, error) {
	m.writeMu.Lock()
	defer m.writeMu.Unlock()

	// 高性能优化：使用固定数组减少内存分配
	header := [4]byte{}
	binary.BigEndian.PutUint16(header[0:2], id)
	binary.BigEndian.PutUint16(header[2:4], uint16(len(data)))
	if _, err := m.physical.Write(header[:]); err != nil {
		return 0, err
	}
	m.streamsLastTime.Store(id, time.Now().Unix())

	return m.physical.Write(data)
}

func (m *MuxManager) checkActive() {

	for {
		m.mu.Lock()
		for id := range m.streams {
			if value, ok := m.streamsLastTime.Load(id); ok {
				lastTime := value.(int64)
				if lastTime+120 < time.Now().Unix() && lastTime > 0 {
					if ch, ok := m.streams[id]; ok {
						fmt.Printf("close id:%d\r\n", id)
						close(ch)
						delete(m.streams, id)
					}
				}
			}
		}
		m.mu.Unlock()
		time.Sleep(time.Second * 30)
	}
}

// Closebt 关闭物理蓝牙连接
func (m *MuxManager) CloseBt() {
	if m.physical != nil {
		m.physical.Close()
	}
}

// VirtualConn 实现了 io.ReadWriteCloser，业务代码可以直接 io.Copy 它
type VirtualConn struct {
	id       uint16
	manager  *MuxManager
	readCh   chan []byte
	cacheBuf []byte // 新增：用于暂存未读完的数据
}

func (v *VirtualConn) Write(p []byte) (int, error) {
	//包头id大于0表示是数据包
	return v.manager.writePacket(v.id, p)
}

func (v *VirtualConn) Read(p []byte) (int, error) {
	// 1. 如果上次还有残留数据，先读残留的
	if len(v.cacheBuf) > 0 {
		n := copy(p, v.cacheBuf)
		v.cacheBuf = v.cacheBuf[n:]
		fmt.Printf("1111\r\n")
		return n, nil
	}

	// 2. 阻塞等待新数据
	data, ok := <-v.readCh
	if !ok {
		return 0, io.EOF
	}

	// 3. 拷贝数据
	n := copy(p, data)
	// 4. 如果 p 装不下，把剩下的存入 v.buf
	if n < len(data) {
		v.cacheBuf = append(v.cacheBuf, data[n:]...)
		fmt.Printf("eeee\r\n")
	}
	return n, nil
}

func (v *VirtualConn) Close() error {
	v.manager.mu.Lock()
	if _, ok := v.manager.streams[v.id]; ok {
		delete(v.manager.streams, v.id)
		close(v.readCh)
	}
	v.manager.mu.Unlock()
	return nil
}
