package com.dosgo.btProxy;

import java.io.*;
import java.net.*;
import java.util.concurrent.ConcurrentHashMap;

/**
 * MuxHandler 对应 Go 端的 MuxManager
 * 使用 4 字节头部：[ID(2 bytes)][Length(2 bytes)]
 */
public class BluetoothMuxHandler {
    private final InputStream btIn;
    private final OutputStream btOut;
    // 用于管理逻辑流 ID (端口) 与本地 Socket 的映射
    private final ConcurrentHashMap<Integer, Socket> streamMap = new ConcurrentHashMap<>();

    private String targetIp = "127.0.0.1";
    private int targetPort = 8022;
    public BluetoothMuxHandler(InputStream btIn, OutputStream btOut,String _targetIp,int _targetPort ) {
        this.btIn = btIn;
        this.btOut = btOut;
        this.targetIp=_targetIp;
        this.targetPort=_targetPort;
    }

    /**
     * 启动主循环，解析蓝牙发来的封装包
     */
    public void start() {
        new Thread(() -> {
            try {
                byte[] header = new byte[4];
                while (true) {
                    // 1. 读取 4 字节头部
                    readFull(btIn, header);
                    int id = ((header[0] & 0xFF) << 8) | (header[1] & 0xFF);
                    int len = ((header[2] & 0xFF) << 8) | (header[3] & 0xFF);

                    // 2. 读取 Payload 数据
                    byte[] payload = new byte[len];
                    if (len > 0) {
                        readFull(btIn, payload);
                    }

                    // 3. 分发数据
                    handleStreamData(id, payload);
                }
            } catch (IOException e) {
                e.printStackTrace();
                cleanup();
            }
        }).start();
    }

    private void handleStreamData(int id, byte[] data) {
        Socket targetSocket = streamMap.get(id);
        
        // 如果是该 ID 的首个数据包，建立本地连接
        if (targetSocket == null || targetSocket.isClosed()) {
            try {
                // 这里固定连接到本地 SSH 端口或其他服务
                final Socket newSocket = new Socket(this.targetIp, this.targetPort);
                streamMap.put(id, newSocket);
                
                // 启动反向传输：本地 Socket -> 蓝牙
                startReverseBridge(id, newSocket);
                targetSocket = newSocket;
            } catch (IOException e) {
                return;
            }
        }

        try {
            targetSocket.getOutputStream().write(data);
        } catch (IOException e) {
            streamMap.remove(id);
        }
    }

    /**
     * 反向桥接：读取本地 Socket 数据并打上 ID 头部发回蓝牙
     */
    private void startReverseBridge(final int id, final Socket socket) {
        new Thread(() -> {
            try (InputStream in = socket.getInputStream()) {
                byte[] buffer = new byte[1024 * 4];
                int n;
                while ((n = in.read(buffer)) != -1) {
                    sendFrame(id, buffer, n);
                }
            } catch (IOException e) {
                // 错误处理
            } finally {
                streamMap.remove(id);
                try { socket.close(); } catch (IOException ignored) {}
            }
        }).start();
    }

    /**
     * 封包发送：[ID(2)][Len(2)][Data...]
     * 使用 synchronized 保证物理写入的原子性，防止多线程写入导致包头交织
     */
    private synchronized void sendFrame(int id, byte[] data, int len) throws IOException {
        byte[] header = new byte[4];
        header[0] = (byte) (id >> 8);
        header[1] = (byte) id;
        header[2] = (byte) (len >> 8);
        header[3] = (byte) len;

        btOut.write(header);
        btOut.write(data, 0, len);
        btOut.flush();
    }

    private void readFull(InputStream in, byte[] b) throws IOException {
        int offset = 0;
        while (offset < b.length) {
            int n = in.read(b, offset, b.length - offset);
            if (n == -1) throw new EOFException("蓝牙流已断开");
            offset += n;
        }
    }

    private void cleanup() {
        for (Socket s : streamMap.values()) {
            try { s.close(); } catch (IOException ignored) {}
        }
        streamMap.clear();
    }
}