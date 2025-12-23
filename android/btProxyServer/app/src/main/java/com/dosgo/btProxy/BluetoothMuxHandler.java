package com.dosgo.btProxy;

import java.io.*;
import java.net.*;
import java.nio.ByteBuffer;
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


    public BluetoothMuxHandler(InputStream btIn, OutputStream btOut ) {
        this.btIn = btIn;
        this.btOut = btOut;
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
        //控制命令
        if(id==0){
            // 1. 使用 ByteBuffer 包装 payload
            ByteBuffer bb = ByteBuffer.wrap(data);
            // 2. 像点菜一样读取数据，自动处理字节序
            int realId = bb.getShort() & 0xFFFF; // 读取 2 字节 ID

            // 3. 提取 4 字节 IP 并直接转为 InetAddress
            byte[] ipBytes = new byte[4];
            bb.get(ipBytes); // 读取接下来的 4 字节
            try {
                String ip = InetAddress.getByAddress(ipBytes).getHostAddress();
                int port = bb.getShort() & 0xFFFF; // 读取 2 字节 Port
                // 存入路由表
                final Socket newSocket = new Socket(ip, port);
                streamMap.put(realId, newSocket);
                startReverseBridge(realId, newSocket);
            } catch (IOException e) {
                e.printStackTrace();
            }
            return;
        }
        //数据
        Socket targetSocket = streamMap.get(id);
        if(targetSocket!=null&& !targetSocket.isClosed()){
            try {
                targetSocket.getOutputStream().write(data);
            } catch (IOException e) {
                streamMap.remove(id);
                try { targetSocket.close(); } catch (IOException ignored) {}
            }
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