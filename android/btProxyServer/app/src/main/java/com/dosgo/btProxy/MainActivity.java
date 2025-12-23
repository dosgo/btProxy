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



    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);

        setContentView(R.layout.activity_main); // 引用上面的 XML


        Button btnStart = findViewById(R.id.btn_start);


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
    }
}