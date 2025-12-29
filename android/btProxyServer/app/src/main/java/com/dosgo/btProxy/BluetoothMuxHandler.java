package com.dosgo.btProxy;

import java.io.*;
import java.net.*;
import java.nio.ByteBuffer;
import java.nio.charset.StandardCharsets;
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
                byte[] payload = new byte[1024*33];
                while (true) {
                    // 1. 读取 4 字节头部
                    readFull(btIn, header,4);
                    int id = ((header[0] & 0xFF) << 8) | (header[1] & 0xFF);
                    int len = ((header[2] & 0xFF) << 8) | (header[3] & 0xFF);

                    // 2. 读取 Payload 数据
                   // byte[] payload = new byte[len];
                    if (len > 0) {
                        
                        readFull(btIn, payload,len);
                    }

                    // 3. 分发数据
                    handleStreamData(id, payload,len);
                }
            } catch (IOException e) {
                e.printStackTrace();
                cleanup();
            }
        }).start();
    }

    private void handleStreamData(int id, byte[] data,int len) {
        //控制命令
        if(id==0){
            // 1. 使用 ByteBuffer 包装 payload
            ByteBuffer bb = ByteBuffer.wrap(data, 0, len);
            // 2. 像点菜一样读取数据，自动处理字节序
            int realId = bb.getShort() & 0xFFFF; // 读取 2 字节 ID

            int  flag=bb.get() & 0xFF;
            System.out.println("flag:"+flag);
            String ip="";
            //域名
            if(flag==0x03){
                byte[] stringBytes = new byte[len-5];
                // 3. 从 ByteBuffer 中批量读取字节
                bb.get(stringBytes);
                // 4. 转换为字符串 (建议显式指定编码，通常是 UTF-8)
                ip = new String(stringBytes, StandardCharsets.UTF_8);
            }else {
                int ipSize = (flag == 0x02) ? 16 : 4;
                byte[] ipBytes = new byte[ipSize];
                bb.get(ipBytes); // 读取接下来的 4 字节
                try {
                     ip = InetAddress.getByAddress(ipBytes).getHostAddress();
                } catch (IOException e) {
                    e.printStackTrace();
                }
            }
            try {
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
                targetSocket.getOutputStream().write(data,0,len);
            } catch (IOException e) {
                e.printStackTrace();
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
                byte[] buffer = new byte[1024*8];
                int n;
                while ((n = in.read(buffer)) != -1) {
                    sendFrame(id, buffer, n);
                }
            } catch (IOException e) {
                e.printStackTrace();
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
        byte[] frame = new byte[4 + len];
        ByteBuffer bb = ByteBuffer.wrap(frame);
        System.out.println("sendFrame len:"+len);

        // 2. 显式设置大端序 (Big Endian)
        bb.order(java.nio.ByteOrder.BIG_ENDIAN);

        // 3. 写入 2 字节 ID 和 2 字节 长度
        bb.putShort((short) id);
        bb.putShort((short) len);

        // 4. 写入剩余的数据载荷
        bb.put(data, 0, len);
        btOut.write(frame);
        btOut.flush();
    }

    private void readFull(InputStream in, byte[] b,int len) throws IOException {
        int offset = 0;
        while (offset <len) {
            int n = in.read(b, offset, len - offset);
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