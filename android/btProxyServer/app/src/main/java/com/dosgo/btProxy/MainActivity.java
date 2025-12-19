package com.dosgo.btProxy;

import android.Manifest;
import android.content.Context;
import android.content.Intent;
import android.content.SharedPreferences;
import android.os.Build;
import android.os.Bundle;
import android.widget.Button;
import android.widget.EditText;

import androidx.appcompat.app.AppCompatActivity;
import androidx.core.app.ActivityCompat;

public class MainActivity extends AppCompatActivity {

    private static final String PREFS_NAME = "BtBridgeConfig";
    private static final String KEY_IP = "last_ip";
    private static final String KEY_PORT = "last_port";

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);

        setContentView(R.layout.activity_main); // 引用上面的 XML

        EditText editIp = findViewById(R.id.edit_ip);
        EditText editPort = findViewById(R.id.edit_port);
        Button btnStart = findViewById(R.id.btn_start);

        SharedPreferences prefs = getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE);
        String savedIp = prefs.getString(KEY_IP, "127.0.0.1"); // 默认 127.0.0.1
        int savedPort = prefs.getInt(KEY_PORT, 8022);
        editIp.setText(savedIp);
        editPort.setText(String.valueOf(savedPort));

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
                String ip = editIp.getText().toString();
                String portStr = editPort.getText().toString();
                int port = Integer.parseInt(portStr);

                // --- 2. 保存当前配置到本地 ---
                SharedPreferences.Editor editor = prefs.edit();
                editor.putString(KEY_IP, ip);
                editor.putInt(KEY_PORT, port);
                editor.apply(); // 异步保存

                Intent intent = new Intent(this, BtBridgeService.class);

                btnStart.setText(R.string.stopText);
                intent.putExtra("target_ip", ip);
                intent.putExtra("target_port", port);

                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                    startForegroundService(intent);
                } else {
                    startService(intent);
                }
                Status.isRunning=true;
            }
        });
    }
}