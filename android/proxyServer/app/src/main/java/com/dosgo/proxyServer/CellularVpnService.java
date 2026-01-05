package com.dosgo.proxyServer;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.content.Context;
import android.content.Intent;
import android.content.pm.ServiceInfo;
import android.net.ConnectivityManager;
import android.net.Network;
import android.net.NetworkCapabilities;
import android.net.NetworkRequest;
import android.net.VpnService;
import android.os.Build;
import android.os.ParcelFileDescriptor;
import android.util.Log;
import androidx.core.app.NotificationCompat;
import cellularvpn.Cellularvpn;

import java.io.File;

public class CellularVpnService extends VpnService {
    private static final String TAG = "ForceCellular";
    private static final String CHANNEL_ID = "cellular_vpn_channel";
    private ParcelFileDescriptor mInterface;
    private ConnectivityManager cm;
    private Network targetMobileNetwork;
    private int vpnFd;
    @Override
    public int onStartCommand(Intent intent, int flags, int startId) {
        // 1. 立即启动前台通知（Android强制要求，否则不给图标且会崩溃）
        startForegroundService();

        cm = (ConnectivityManager) getSystemService(Context.CONNECTIVITY_SERVICE);
        setupVpn();
        // 2. 请求移动网络并绑定为底层链路
        requestMobileNetwork();

        // 3. 建立VPN接口（激活钥匙图标）


        return START_STICKY;
    }

    private void requestMobileNetwork() {
        NetworkRequest request = new NetworkRequest.Builder()
                .addTransportType(NetworkCapabilities.TRANSPORT_CELLULAR)
                .addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
                .build();

        cm.requestNetwork(request, new ConnectivityManager.NetworkCallback() {
            @Override
            public void onAvailable(Network network) {
                Log.d(TAG, "移动网络已就绪，绑定底层链路");
                // 核心：告诉系统这个VPN是跑在流量上的
                setUnderlyingNetworks(new Network[]{network});
                targetMobileNetwork=network;
                cm.bindProcessToNetwork(network);
                // 1. 获取 Handle
                long handle = network.getNetworkHandle();
                // 3. 启动 Go 栈，传入这个 handle
                // 这里的 startStack 是你 AAR 里的导出函数
                new Thread(() -> {
                    Cellularvpn.startStack(vpnFd, handle);
                }).start();
            }
        });
    }

    private void setupVpn() {
        if (mInterface != null) return;

        try {
            Builder builder = new Builder();

            // 关键：排除掉自己的 App，防止自己的请求也被 VPN 逻辑干扰
            builder.addDisallowedApplication(getPackageName());
            builder.addDisallowedApplication("com.android.networkstack");
// 3. 基础服务框架（许多探测请求会代理给 gms 发起）
            builder.addDisallowedApplication("com.google.android.gms");
            builder.addDisallowedApplication("com.google.android.apps.bard");
            // 排除 Google App (Gemini 的核心载体)
            builder.addDisallowedApplication("com.google.android.googlequicksearchbox");
            builder.addDisallowedApplication("com.microsoft.emmx");
            mInterface = builder.setSession("ForceCellular")
                    .setMtu(1500)
                    .addAddress("10.8.8.1", 24)
                    .addRoute("0.0.0.0", 0)
                    .addDnsServer("223.5.5.5")
                    .establish();
            if (mInterface != null) {
                 vpnFd = mInterface.getFd();

                // 3. 获取 NetworkHandle (如果你需要绑定特定网络，比如蜂窝)

            }
            Log.i(TAG, "VPN 接口已建立，钥匙图标应已出现");
        } catch (Exception e) {
            Log.e(TAG, "建立 VPN 失败", e);
        }
    }



    private void startForegroundService() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            NotificationChannel channel = new NotificationChannel(
                    CHANNEL_ID, "流量切换服务", NotificationManager.IMPORTANCE_LOW);
            NotificationManager nm = getSystemService(NotificationManager.class);
            if (nm != null) nm.createNotificationChannel(channel);
        }

        Notification notification = new NotificationCompat.Builder(this, CHANNEL_ID)
                .setContentTitle("强制流量模式已启动")
                .setContentText("正在通过移动网络优化连接")
                .setSmallIcon(android.R.drawable.ic_dialog_info)
                .build();

        // 适配 Android 14+ 的特殊类型要求
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
            startForeground(2, notification, ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE);
        } else {
            startForeground(2, notification);
        }
    }

    @Override
    public void onDestroy() {
        // 2. 停止前台通知，移除通知栏
        stopForeground(STOP_FOREGROUND_REMOVE);

        try {
            if (mInterface != null) {
                mInterface.close();
                mInterface = null;
            }
        } catch (Exception e) {
            Log.e(TAG, "销毁失败", e);
        }
        super.onDestroy();
    }
}