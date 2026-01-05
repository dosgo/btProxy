package com.dosgo.proxyServer;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.content.Intent;
import android.content.pm.ServiceInfo;
import android.net.VpnService;
import android.os.Build;
import android.os.ParcelFileDescriptor;
import android.util.Log;

import androidx.core.app.NotificationCompat;

import java.io.*;
import java.nio.ByteBuffer;
import java.util.concurrent.*;
import okhttp3.*;

public class SecureDnsService extends VpnService implements Runnable {
    private static final String TAG = "SecureDns";
    private static final String FAKE_DNS = "10.0.0.53"; // 虚拟 DNS 目标
    private ParcelFileDescriptor vpnInterface;
    private final ExecutorService executor = Executors.newFixedThreadPool(10);
    private final OkHttpClient httpClient = new OkHttpClient();
    private static final String CHANNEL_ID = "vpn_channel";

    private static Thread mThread;
    @Override
    public int onStartCommand(Intent intent, int flags, int startId) {
        createNotificationChannel();

        // 2. 构建通知
        Notification notification = new NotificationCompat.Builder(this, CHANNEL_ID)
                .setContentTitle("安全 DNS 运行中")
                .setContentText("正在保护移动网络 DNS 查询")
                .setSmallIcon(R.drawable.logo) // 确保你资源里有这个图标
                .setPriority(NotificationCompat.PRIORITY_LOW)
                .build();

        // 3. 开启前台服务 (这是出现图标的关键)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
            startForeground(1, notification, ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE);
        } else {
            startForeground(1, notification);
        }
        mThread=new Thread(this, "VpnThread");
        mThread.start();
        return START_STICKY;
    }

    @Override
    public void run() {
        try {
            // 1. 配置 VPN：强制拦截系统 DNS 流量
            VpnService.Builder builder = new Builder();
            builder.setSession("SecureDns")
                   .addAddress("10.0.0.1", 24)
                   .addDnsServer(FAKE_DNS)      // 告诉系统 DNS 是 10.0.0.53
                   .addRoute(FAKE_DNS, 32)      // 只让发往 10.0.0.53 的包进来
                   .addDisallowedApplication(getPackageName()); // 排除自身防止死循环
            
            vpnInterface = builder.establish();
            Log.d(TAG, "VPN Started. Intercepting DNS...");

            FileInputStream in = new FileInputStream(vpnInterface.getFileDescriptor());
            FileOutputStream out = new FileOutputStream(vpnInterface.getFileDescriptor());

            byte[] buffer = new byte[16384];
            while (!Thread.interrupted()) {
                int read = in.read(buffer);
                if (read > 0) {
                    // 2. 处理拦截到的 IP 包
                    handlePacket(ByteBuffer.wrap(buffer, 0, read), out);
                }
            }
        } catch (Exception e) {
            Log.e(TAG, "Error in VPN loop", e);
        } finally {
            stopVpn();
        }
    }

    private void createNotificationChannel() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            NotificationChannel serviceChannel = new NotificationChannel(
                    CHANNEL_ID,
                    "VPN Service Channel",
                    NotificationManager.IMPORTANCE_LOW
            );
            NotificationManager manager = getSystemService(NotificationManager.class);
            if (manager != null) manager.createNotificationChannel(serviceChannel);
        }
    }

    private void handlePacket(ByteBuffer packet, FileOutputStream out) throws IOException {
        // 解析 IPv4 报头 (简单处理)
        int ihl = (packet.get(0) & 0x0F) * 4;
        byte protocol = packet.get(9);

        if (protocol == 17) { // UDP
            int srcPort = Short.toUnsignedInt(packet.getShort(ihl));
            int dstPort = Short.toUnsignedInt(packet.getShort(ihl + 2));

            if (dstPort == 53) {
                // 提取 DNS 内容
                int udpLen = Short.toUnsignedInt(packet.getShort(ihl + 4));
                byte[] dnsData = new byte[udpLen - 8];
                packet.position(ihl + 8);
                packet.get(dnsData);

                // 异步发送 DoH
                ByteBuffer copyPacket = ByteBuffer.allocate(packet.limit());
                packet.rewind();
                copyPacket.put(packet);
                executor.execute(() -> queryDoHAndReply(dnsData, copyPacket, out));
            }
        }
    }

    private void queryDoHAndReply(byte[] query, ByteBuffer origPacket, FileOutputStream out) {
        // 使用阿里云 DoH (也可换成腾讯 https://doh.pub/dns-query)
        RequestBody body = RequestBody.create(query, MediaType.parse("application/dns-message"));
        Request request = new Request.Builder()
                .url("https://dns.alidns.com/dns-query")
                .post(body)
                .build();
        try (Response response = httpClient.newCall(request).execute()) {
            if (response.isSuccessful() && response.body() != null) {
                byte[] answer = response.body().bytes();
                sendUdpReply(answer, origPacket, out);
            }
        } catch (Exception e) {
            Log.e(TAG, "DoH Query Failed", e);
        }
    }

    private void sendUdpReply(byte[] dnsAnswer, ByteBuffer queryPacket, FileOutputStream out) throws IOException {
        int ihl = (queryPacket.get(0) & 0x0F) * 4;
        int totalLen = ihl + 8 + dnsAnswer.length;
        ByteBuffer reply = ByteBuffer.allocate(totalLen);

        // IP Header (简易构造)
        reply.put(queryPacket.array(), 0, ihl);
        reply.putShort(2, (short) totalLen);
        // 交换 IP：原目的(10.0.0.53)变新源，原源变新目的
        for (int i = 0; i < 4; i++) {
            reply.put(12 + i, queryPacket.get(16 + i));
            reply.put(16 + i, queryPacket.get(12 + i));
        }
        // 重置 Checksum
        reply.putShort(10, (short) 0);
        reply.putShort(10, calculateChecksum(reply, ihl));

        // UDP Header
        reply.position(ihl);
        reply.putShort(queryPacket.getShort(ihl + 2)); // Source Port (53)
        reply.putShort(queryPacket.getShort(ihl));     // Dest Port
        reply.putShort((short) (8 + dnsAnswer.length));
        reply.putShort((short) 0); // UDP Checksum 设为 0

        // Payload
        reply.put(dnsAnswer);

        synchronized (out) {
            out.write(reply.array(), 0, totalLen);
        }
    }

    private short calculateChecksum(ByteBuffer buf, int length) {
        int sum = 0;
        int i = 0;
        while (length > 1) {
            sum += Short.toUnsignedInt(buf.getShort(i));
            i += 2;
            length -= 2;
        }
        if (length > 0) sum += (buf.get(i) & 0xFF) << 8;
        while ((sum >> 16) > 0) sum = (sum & 0xFFFF) + (sum >> 16);
        return (short) (~sum);
    }

    private void stopVpn() {
        try { if (vpnInterface != null) vpnInterface.close(); } catch (Exception e) {}
    }

    @Override
    public void onDestroy() {
        // 1. 先标志位中断线程
        if (mThread != null) {
            mThread.interrupt();
        }

        // 2. 停止前台通知，移除通知栏
        stopForeground(STOP_FOREGROUND_REMOVE);

        // 3. 关闭 VPN 接口
        stopVpn();

        super.onDestroy();
    }
}