package com.dosgo.proxyServer;

import android.Manifest;
import android.content.Context;
import android.content.Intent;
import android.content.SharedPreferences;
import android.net.VpnService;
import android.os.Build;
import android.os.Bundle;
import android.util.Log;
import android.widget.Button;
import android.widget.EditText;
import android.widget.TextView;

import androidx.activity.result.ActivityResultLauncher;
import androidx.activity.result.contract.ActivityResultContracts;
import androidx.appcompat.app.AppCompatActivity;
import androidx.core.app.ActivityCompat;

public class MainActivity extends AppCompatActivity {

    EditText socksPort;
    Button vpnStart;

    private ActivityResultLauncher<Intent> vpnPermissionLauncher;
    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);

        setContentView(R.layout.activity_main); // 引用上面的 XML


        Button btnStart = findViewById(R.id.btn_start);
        socksPort=findViewById(R.id.et_port);

        // 申请权限 (简化的逻辑)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            ActivityCompat.requestPermissions(this, new String[]{
                    Manifest.permission.BLUETOOTH_CONNECT,
                    Manifest.permission.BLUETOOTH_ADVERTISE
            }, 1);
        }

        btnStart.setOnClickListener(v -> {

            if(Status.isRunning){
                stopService(new Intent(this, BtBridgeService.class));
                btnStart.setText(R.string.startText);
                Status.isRunning=false;

            }else {
                String portStr=socksPort.getText().toString().trim();
                if (!portStr.isEmpty()) {
                    try {
                        int port = Integer.parseInt(portStr);
                        // 在这里使用 port
                        Status.socksPort = port;
                    } catch (NumberFormatException e) {
                        Status.socksPort = 0;
                        // 如果字符串不是有效的数字（例如 "1080a"），会进入这里
                        Log.e("Socks5", "输入的端口格式不正确");
                    }
                }
                Intent intent = new Intent(this, BtBridgeService.class);
                btnStart.setText(R.string.stopText);
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                    startForegroundService(intent);
                } else {
                    startService(intent);
                }
                Status.isRunning=true;
            }
        });

         vpnStart = findViewById(R.id.vpn_start);
        vpnStart.setOnClickListener(v -> {

            if(Status.vpnIsRunning){
                stopService(new Intent(this, CellularVpnService.class));
                Status.vpnIsRunning=false;
                vpnStart.setText(R.string.startVpnText);
            }else {
                prepareVpn();
            }
        });




        vpnPermissionLauncher = registerForActivityResult(
                new ActivityResultContracts.StartActivityForResult(),
                result -> {
                    if (result.getResultCode() == RESULT_OK) {
                        // 用户授权成功，启动服务
                        startSecureDnsService();
                    } else {
                        // 用户拒绝了授权
                        Log.e("VPN", "User denied VPN permission");
                    }
                }
        );

    }



    private void prepareVpn() {
        // 2. 检查权限
        Intent intent = VpnService.prepare(this);
        if (intent != null) {
            // 3. 弹出系统授权对话框
            vpnPermissionLauncher.launch(intent);
        } else {
            // 已经授权过了，直接启动
            startSecureDnsService();
            vpnStart.setText(R.string.stopVpnText);
        }
    }
    private void startSecureDnsService() {
        Intent intent = new Intent(this, CellularVpnService.class);
        startService(intent);
        Status.vpnIsRunning=true;
    }
}