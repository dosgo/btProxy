package comm

import (
	"encoding/binary"
	"io"
	"sync"
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
			ch <- payload
		}
		m.mu.RUnlock()
	}
}

// OpenStream 是关键：它返回一个类似流的对象，侵入性极小
func (m *MuxManager) OpenStream(id uint16) io.ReadWriteCloser {
	ch := make(chan []byte, 100)
	m.mu.Lock()
	m.streams[id] = ch
	m.mu.Unlock()
	return &VirtualConn{id: id, manager: m, readCh: ch}
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
	cache   []byte
}

func (v *VirtualConn) Write(p []byte) (int, error) {
	v.manager.writeMu.Lock()
	defer v.manager.writeMu.Unlock()
	// 封装协议头：[ID][Len][Data]
	header := []byte{byte(v.id >> 8), byte(v.id), byte(len(p) >> 8), byte(len(p))}
	if _, err := v.manager.physical.Write(header); err != nil {
		return 0, err
	}
	return v.manager.physical.Write(p)
}

func (v *VirtualConn) Read(p []byte) (int, error) {
	if len(v.cache) == 0 {
		data, ok := <-v.readCh
		if !ok {
			return 0, io.EOF
		}
		v.cache = data
	}
	n := copy(p, v.cache)
	v.cache = v.cache[n:]
	return n, nil
}

func (v *VirtualConn) Close() error { return nil } // 可根据需要发送 Close 帧
