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
import android.os.IBinder;
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
    private static final int NOTIFICATION_ID = 18100;
    private SimpleSocks5Server socksServer;
    private int vpnFd;


    @Override
    public void onCreate() {
        super.onCreate();
        createNotificationChannel();

        // 2. 获取 Notification 对象
        Notification notification = getNotification();


        if (Build.VERSION.SDK_INT >= 34) {
            startForeground(
                    NOTIFICATION_ID,
                    notification,
                    ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE);
        }else {
            startForeground(
                    NOTIFICATION_ID,
                    notification);
        }

        cm = (ConnectivityManager) getSystemService(Context.CONNECTIVITY_SERVICE);


    }
    @Override
    public int onStartCommand(Intent intent, int flags, int startId) {
        setupVpn();
        // 2. 请求移动网络并绑定为底层链路
        requestMobileNetwork();
        System.out.println("onStartCommand ok");
        return START_NOT_STICKY;
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

                System.out.println("socksPort:"+ Status.socksPort);
                if( Status.socksPort>0) {
                    System.out.println("socksPort2:"+ Status.socksPort);
                    socksServer = new SimpleSocks5Server( Status.socksPort, targetMobileNetwork);
                    socksServer.start();
                }
              //  cm.bindProcessToNetwork(network);
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

            // 在 setupVpn 时，重点排除这个
            builder.addDisallowedApplication("com.android.captiveportallogin");
            mInterface = builder.setSession("ForceCellular")
                    .setMtu(1500)
                    .addAddress("10.8.8.1", 24)
                    .addRoute("0.0.0.0", 0)
                    .addRoute("::", 0)
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





    private void createNotificationChannel() {


        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            NotificationChannel channel = new NotificationChannel(
                    CHANNEL_ID,
                    "流量切换服务",
                    NotificationManager.IMPORTANCE_LOW
            );
            ((NotificationManager) getSystemService(NOTIFICATION_SERVICE))
                    .createNotificationChannel(channel);
        }
    }

    private Notification getNotification() {


        Notification.Builder builder;

        // 针对 Android 8.0 (API 26) 及以上版本，必须关联 Channel ID
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            builder = new Notification.Builder(this, CHANNEL_ID);
        } else {
            // 旧版本不需要 Channel ID
            builder = new Notification.Builder(this);
        }

        return builder
                .setContentTitle("强制流量模式已启动")
                .setContentText("正在通过移动网络优化连接")
                // 注意：必须设置小图标，否则某些机型会报错或无法启动前台服务
                .setSmallIcon(R.drawable.logo)
                .setOngoing(true) // 设置为持久通知
                .build();
    }

    @Override
    public void onDestroy() {
        
        // 2. 停止前台通知，移除通知栏
       // stopForeground(STOP_FOREGROUND_REMOVE);

        System.out.println("CellularVpnService onDestroy");
        super.onDestroy();
        /*
        try {
            if (mInterface != null) {
                mInterface.close();
                mInterface = null;
            }
        } catch (Exception e) {
            Log.e(TAG, "销毁失败", e);
        }
        if (socksServer != null) {
            socksServer.stop();
        }

        Cellularvpn.stopStack();

         */
    }


    @Override
    public IBinder onBind(Intent intent) { return null; }
}