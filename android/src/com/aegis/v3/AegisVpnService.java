package com.aegis.v3;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.content.Context;
import android.content.Intent;
import android.net.VpnService;
import android.os.Build;
import android.os.ParcelFileDescriptor;
import android.util.Log;

import java.io.IOException;
import java.net.DatagramSocket;

public class AegisVpnService extends VpnService {
    private static final String TAG = "AegisVpnService";
    private static final String CHANNEL_ID = "aegis_vpn_channel";
    private static final int NOTIFICATION_ID = 3001;

    public static final String ACTION_CONNECT = "com.aegis.v3.CONNECT";
    public static final String ACTION_DISCONNECT = "com.aegis.v3.DISCONNECT";
    public static final String EXTRA_ENDPOINT = "extra_endpoint";
    public static final String EXTRA_SERVER_NAME = "extra_server_name";

    private ParcelFileDescriptor mInterface;
    private RadioObserver mRadioObserver;
    private DatagramSocket mTransportSocket;
    private boolean mIsRunning = false;

    @Override
    public void onCreate() {
        super.onCreate();
        mRadioObserver = new RadioObserver(this);
    }

    @Override
    public int onStartCommand(Intent intent, int flags, int startId) {
        if (intent == null) return START_NOT_STICKY;

        String action = intent.getAction();
        if (ACTION_CONNECT.equals(action)) {
            String endpoint = intent.getStringExtra(EXTRA_ENDPOINT);
            String serverName = intent.getStringExtra(EXTRA_SERVER_NAME);
            if (endpoint == null) endpoint = "172.209.217.140:443";
            if (serverName == null) serverName = "172.209.217.140";
            connectTunnel(endpoint, serverName);
        } else if (ACTION_DISCONNECT.equals(action)) {
            disconnectTunnel();
        }

        return START_STICKY;
    }

    private synchronized void connectTunnel(String endpoint, String serverName) {
        if (mIsRunning) return;

        createNotificationChannel();
        startForeground(NOTIFICATION_ID, buildForegroundNotification());

        try {
            // 1. Establish Virtual Layer 3 Network Interface
            Builder builder = new Builder();
            builder.setMtu(1280) // RFC 9221 / IPv6 safe MTU (prevents fragmentation)
                   .addAddress("10.77.77.2", 24)
                   .addRoute("0.0.0.0", 0)       // Capture all IPv4 traffic
                   .addRoute("::", 0)            // Capture all IPv6 traffic
                   .addDnsServer("1.1.1.1")
                   .addDnsServer("8.8.8.8")
                   .setSession("Aegis Shield v3")
                   .setBlocking(true);

            mInterface = builder.establish();
            if (mInterface == null) {
                Log.e(TAG, "Failed to establish VPN interface");
                stopSelf();
                return;
            }

            // 2. Prevent Loopback Deadlock on the Outbound Socket
            mTransportSocket = new DatagramSocket();
            protect(mTransportSocket);

            int tunFd = mInterface.detachFd();

            // 3. Start Native Go Aegis Core Engine
            int res = AegisCore.start(endpoint, serverName, tunFd);
            if (res != 0) {
                Log.e(TAG, "Native AegisCore engine failed to initialize: " + res);
                disconnectTunnel();
                return;
            }

            // 4. Start Radio Telemetry Observer
            mRadioObserver.start();

            mIsRunning = true;
            Log.i(TAG, "🛡️ Aegis Protocol v3 Tunnel ACTIVE via " + endpoint);

        } catch (Exception e) {
            Log.e(TAG, "Tunnel startup exception: " + e.getMessage(), e);
            disconnectTunnel();
        }
    }

    private synchronized void disconnectTunnel() {
        if (!mIsRunning) return;

        if (mRadioObserver != null) {
            mRadioObserver.stop();
        }

        AegisCore.stop();

        if (mTransportSocket != null) {
            mTransportSocket.close();
            mTransportSocket = null;
        }

        if (mInterface != null) {
            try {
                mInterface.close();
            } catch (IOException ignored) {}
            mInterface = null;
        }

        mIsRunning = false;
        stopForeground(true);
        stopSelf();
        Log.i(TAG, "Aegis Protocol v3 Tunnel STOPPED");
    }

    private void createNotificationChannel() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            NotificationChannel channel = new NotificationChannel(
                    CHANNEL_ID,
                    "Aegis Shield Active",
                    NotificationManager.IMPORTANCE_LOW
            );
            channel.setDescription("Maintains encrypted tunnel and FEC parity monitoring");
            NotificationManager nm = getSystemService(NotificationManager.class);
            if (nm != null) nm.createNotificationChannel(channel);
        }
    }

    private Notification buildForegroundNotification() {
        Intent notificationIntent = new Intent(this, MainActivity.class);
        PendingIntent pendingIntent = PendingIntent.getActivity(
                this, 0, notificationIntent,
                PendingIntent.FLAG_IMMUTABLE
        );

        Notification.Builder builder = Build.VERSION.SDK_INT >= Build.VERSION_CODES.O ?
                new Notification.Builder(this, CHANNEL_ID) :
                new Notification.Builder(this);

        return builder.setContentTitle("Aegis Protocol v3 Active")
                      .setContentText("Shielding all apps with Reed-Solomon FEC + QUIC")
                      .setSmallIcon(android.R.drawable.ic_dialog_info)
                      .setContentIntent(pendingIntent)
                      .setOngoing(true)
                      .build();
    }

    @Override
    public void onDestroy() {
        disconnectTunnel();
        super.onDestroy();
    }
}
