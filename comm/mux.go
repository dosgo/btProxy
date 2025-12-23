package comm

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"
)

// MuxManager 负责管理那个唯一的蓝牙物理连接
type MuxManager struct {
	physical io.ReadWriteCloser
	streams  map[uint16]chan []byte // 每个ID对应一个数据管道
	mu       sync.RWMutex
	writeMu  sync.Mutex // 物理写锁，保证Header和Data不被拆散
}

func NewMuxManager(p io.ReadWriteCloser) *MuxManager {
	m := &MuxManager{
		physical: p,
		streams:  make(map[uint16]chan []byte),
	}
	go m.readLoop() // 启动后台“拆包”协程
	return m
}

// readLoop 在底层做手脚：解析 ID，把数据塞进正确的通道
func (m *MuxManager) readLoop() {
	header := make([]byte, 4) // 1字节ID + 2字节长度
	for {
		if _, err := io.ReadFull(m.physical, header); err != nil {
			return
		}

		id := binary.BigEndian.Uint16(header[0:2])
		dataLen := binary.BigEndian.Uint16(header[2:4])
		payload := make([]byte, dataLen)
		if _, err := io.ReadFull(m.physical, payload); err != nil {
			return
		}

		m.mu.RLock()
		if ch, ok := m.streams[id]; ok {
			select {
			case ch <- payload:
				// 成功
			case <-time.After(time.Millisecond * 500):
				// 半秒钟还没发进去，说明这个流彻底堵死了，关闭或记录错误
				fmt.Printf("警告: 流 %d 阻塞超时，丢弃数据包\n", id)
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
	ip := net.ParseIP(host).To4()
	// 构造 8 字节的固定载荷
	payload := make([]byte, 8)
	binary.BigEndian.PutUint16(payload[0:2], id)                          // 本地端口,也就是id
	binary.BigEndian.PutUint32(payload[2:6], binary.BigEndian.Uint32(ip)) // 目标IP
	binary.BigEndian.PutUint16(payload[6:8], uint16(port))                // 目标端口
	//包头的id是0表示新连接
	if _, err := m.writePacket(0, payload); err != nil {
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
	return m.physical.Write(data)
}

// Closebt 关闭物理蓝牙连接
func (m *MuxManager) CloseBt() {
	if m.physical != nil {
		m.physical.Close()
	}
}

// VirtualConn 实现了 io.ReadWriteCloser，业务代码可以直接 io.Copy 它
type VirtualConn struct {
	id      uint16
	manager *MuxManager
	readCh  chan []byte
}

func (v *VirtualConn) Write(p []byte) (int, error) {
	//包头id大于0表示是数据包
	return v.manager.writePacket(v.id, p)
}

func (v *VirtualConn) Read(p []byte) (int, error) {
	data, ok := <-v.readCh
	if !ok {
		return 0, io.EOF
	}
	n := copy(p, data)
	return n, nil
}

func (v *VirtualConn) Close() error {

	v.manager.mu.Lock()
	if _, ok := v.manager.streams[v.id]; ok {
		delete(v.manager.streams, v.id)
	}
	v.manager.mu.Unlock()
	close(v.readCh)
	return nil
}
