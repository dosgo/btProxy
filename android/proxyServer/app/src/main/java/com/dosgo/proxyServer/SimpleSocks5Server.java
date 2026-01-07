package com.dosgo.proxyServer;


import android.net.Network;
import android.util.Log;

import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.net.ServerSocket;
import java.net.Socket;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;

/**
 * 简易 SOCKS5 代理服务器类
 * 支持 TCP CONNECT, IPv4 和 域名解析
 */
public class SimpleSocks5Server {
    private static final String TAG = "Socks5Server";
    private final int port;
    private final Network targetNetwork; // 可选：指定出口流量的移动网络
    private ServerSocket serverSocket;
    private ExecutorService threadPool;
    private boolean isRunning = false;


    /**
     * @param port 监听端口
     * @param targetNetwork 如果需要走移动网络，传入获取到的 Network 对象；否则传 null 走系统默认
     */
    public SimpleSocks5Server(int port, Network _targetNetwork) {
        this.port = port;
        this.targetNetwork = _targetNetwork;
        this.threadPool = Executors.newCachedThreadPool();
    }

    public void start() {
        isRunning = true;
        threadPool.execute(() -> {
            try {
                serverSocket = new ServerSocket(port);
                Log.d(TAG, "SOCKS5 服务已启动，监听端口: " + port);
                while (isRunning) {
                    Socket clientSocket = serverSocket.accept();
                    threadPool.execute(() -> handleClient(clientSocket));
                }
            } catch (IOException e) {
                if (isRunning) Log.e(TAG, "服务器异常: " + e.getMessage());
            }
        });
    }

    public void stop() {
        isRunning = false;
        try {
            if (serverSocket != null){
                serverSocket.close();
            }
            threadPool.shutdownNow();
            Log.d(TAG, "SOCKS5 服务已停止");
        } catch (IOException e) {
            e.printStackTrace();
        }
    }

    private void handleClient(Socket client) {
        try (InputStream in = client.getInputStream();
             OutputStream out = client.getOutputStream()) {

            // 1. 握手阶段 (参考 proxy.go 逻辑)
            int version = in.read();
            if (version != 0x05) return;
            int nMethods = in.read();
            in.skip(nMethods); // 跳过支持的方法
            out.write(new byte[]{0x05, 0x00}); // 回应：无需认证

            // 2. 请求阶段
            byte[] header = new byte[4];
            in.read(header);
            int cmd = header[1]; // 0x01 = CONNECT
            int atyp = header[3]; // 地址类型

            String host = "";
            if (atyp == 0x01) { // IPv4
                byte[] addr = new byte[4];
                in.read(addr);
                host = InetAddress.getByAddress(addr).getHostAddress();
            } else if (atyp == 0x03) { // 域名
                int len = in.read();
                byte[] buf = new byte[len];
                in.read(buf);
                host = new String(buf);
            }else if (atyp == 0x04) { // IPv6 (16字节)
                byte[] addrV6 = new byte[16];
                in.read(addrV6);
                // InetAddress 会自动根据字节数组识别为 IPv6
                host = InetAddress.getByAddress(addrV6).getHostAddress();
                // 如果是用于 URL，IPv6 字符串需要加中括号，但 Socket 连接直接传 host 即可
            } else {
                return; // 不支持的地址类型
            }

            byte[] pBuf = new byte[2];
            in.read(pBuf);
            int port = ((pBuf[0] & 0xFF) << 8) | (pBuf[1] & 0xFF);

            // 3. 建立远程连接并转发
            try (Socket target = new Socket()) {
                // 如果指定了移动网络，则绑定 Socket
                if (targetNetwork != null) {
                    System.out.println("socks5 use mobile");
                    targetNetwork.bindSocket(target);
                }

                target.connect(new InetSocketAddress(host, port), 10000);
                out.write(new byte[]{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); // 连接成功响应

                // 双向数据拷贝 (模拟 io.Copy)
                relay(client, target);
            }

        } catch (Exception e) {
            Log.e(TAG, "转发请求失败: " + e.getMessage());
        } finally {
            try { client.close(); } catch (IOException ignored) {}
        }
    }

    private void relay(Socket client, Socket target) throws Exception {
        InputStream inC = client.getInputStream();
        OutputStream outC = client.getOutputStream();
        InputStream inT = target.getInputStream();
        OutputStream outT = target.getOutputStream();

        Thread t = new Thread(() -> pipe(inC, outT)); // Client -> Remote
        t.start();
        pipe(inT, outC); // Remote -> Client
        t.join();
    }

    private void pipe(InputStream in, OutputStream out) {
        byte[] buffer = new byte[16384];
        int len;
        try {
            while ((len = in.read(buffer)) != -1) {
                out.write(buffer, 0, len);
                out.flush();
            }
        } catch (IOException ignored) {}
    }
}