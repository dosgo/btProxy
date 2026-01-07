package com.dosgo.proxyServer;

import android.Manifest;
import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.Service;
import android.bluetooth.BluetoothAdapter;
import android.bluetooth.BluetoothServerSocket;
import android.bluetooth.BluetoothSocket;
import android.content.Context;
import android.content.Intent;
import android.content.pm.PackageManager;
import android.content.pm.ServiceInfo;
import android.net.ConnectivityManager;
import android.net.Network;
import android.net.NetworkCapabilities;
import android.net.NetworkRequest;
import android.os.Build;
import android.os.IBinder;

import androidx.core.app.ActivityCompat;

import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.Socket;
import java.util.UUID;

public class BtBridgeService extends Service {
    private static final String CHANNEL_ID = "BtBridgeChannel";
    private static final UUID MY_UUID = UUID.fromString("00001101-0000-1000-8000-00805f9b34fb"); // SPP UUID
    private BluetoothServerSocket serverSocket;

    private boolean isRunning = true;

    private ConnectivityManager connectivityManager;
    private Network mobileNetwork; // 存储拿到的移动网络句柄
    private static final int NOTIFICATION_ID = 10099;

    @Override
    public void onCreate() {
        super.onCreate();
        createNotificationChannel();

        // 2. 获取 Notification 对象
        Notification notification = getNotification("蓝牙隧道已启动，等待连接...");


        if (Build.VERSION.SDK_INT >= 34) {
            startForeground(
                    NOTIFICATION_ID,
                    notification,
                    ServiceInfo.FOREGROUND_SERVICE_TYPE_CONNECTED_DEVICE);
        }else {
            startForeground(
                    NOTIFICATION_ID,
                    notification);
        }


        connectivityManager = (ConnectivityManager) getSystemService(Context.CONNECTIVITY_SERVICE);
        requestMobileNetwork(); // 第一步：准备移动网络

        startBridgeThread();
    }

    private void requestMobileNetwork() {
        NetworkRequest request = new NetworkRequest.Builder()
                .addTransportType(NetworkCapabilities.TRANSPORT_CELLULAR)
                .addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
                .build();

        connectivityManager.requestNetwork(request, new ConnectivityManager.NetworkCallback() {
            @Override
            public void onAvailable( Network network) {
                mobileNetwork = network;
                System.out.println("requestMobileNetwork ok");
            }
        });
    }

    private void startBridgeThread() {
        new Thread(() -> {
            BluetoothAdapter adapter = BluetoothAdapter.getDefaultAdapter();
            try {
                if (ActivityCompat.checkSelfPermission(this, Manifest.permission.BLUETOOTH_CONNECT) != PackageManager.PERMISSION_GRANTED) {
                    System.out.println("startBridgeThread err\r\n");
                    return;
                }
                serverSocket = adapter.listenUsingRfcommWithServiceRecord("SerialPort", MY_UUID);

                while (isRunning) {
                    BluetoothSocket btSocket = serverSocket.accept(); // 阻塞等待连接
                    updateNotification("蓝牙已连接，正在桥接 TCP...");
                    // 1. 实例化处理器
                    BluetoothMuxHandler muxHandler = new BluetoothMuxHandler(btSocket.getInputStream(), btSocket.getOutputStream(),mobileNetwork);

                    // 2. 启动主循环（它会开启后台线程解析 Header）
                    muxHandler.start();
                }
            } catch (IOException e) {
                e.printStackTrace();
            }
        }).start();
    }



    private void createNotificationChannel() {


        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            NotificationChannel channel = new NotificationChannel(
                    CHANNEL_ID,
                    "蓝牙桥接服务",
                    NotificationManager.IMPORTANCE_LOW
            );
            ((NotificationManager) getSystemService(NOTIFICATION_SERVICE))
                    .createNotificationChannel(channel);
        }
    }

    private Notification getNotification(String content) {


        Notification.Builder builder;

        // 针对 Android 8.0 (API 26) 及以上版本，必须关联 Channel ID
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            builder = new Notification.Builder(this, CHANNEL_ID);
        } else {
            // 旧版本不需要 Channel ID
            builder = new Notification.Builder(this);
        }

        return builder
                .setContentTitle("蓝牙TCP代理")
                .setContentText(content)
                // 注意：必须设置小图标，否则某些机型会报错或无法启动前台服务
                .setSmallIcon(R.drawable.logo)
                .setOngoing(true) // 设置为持久通知
                .build();
    }

    private void updateNotification(String content) {
        NotificationManager nm = (NotificationManager) getSystemService(NOTIFICATION_SERVICE);
        nm.notify(1, getNotification(content));
    }




    @Override
    public int onStartCommand(Intent intent, int flags, int startId) {
        return START_STICKY;
    }

    @Override
    public void onDestroy() {
        isRunning = false;
        stopForeground(STOP_FOREGROUND_REMOVE);
        try {
            if (serverSocket != null) serverSocket.close();
        } catch (IOException e){
            e.printStackTrace();
        };

        super.onDestroy();
    }

    @Override
    public IBinder onBind(Intent intent) { return null; }
}