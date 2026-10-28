package com.aegis.v3;

import android.app.Activity;
import android.content.Context;
import android.content.Intent;
import android.content.SharedPreferences;
import android.graphics.Color;
import android.graphics.Typeface;
import android.net.VpnService;
import android.os.Bundle;
import android.view.Gravity;
import android.view.View;
import android.widget.Button;
import android.widget.EditText;
import android.widget.LinearLayout;
import android.widget.ScrollView;
import android.widget.TextView;
import android.widget.Toast;

public class MainActivity extends Activity {
    private static final int VPN_REQUEST_CODE = 0xAE63;
    private static final String PREFS_NAME = "aegis_v3_prefs";
    private static final String KEY_ENDPOINT = "relay_endpoint";

    private boolean mIsConnected = false;
    private TextView mStatusBadge;
    private TextView mMetricsText;
    private EditText mEndpointInput;
    private Button mConnectBtn;
    private SharedPreferences mPrefs;

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        mPrefs = getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE);

        // Root ScrollView to fill entire modern Android screen
        ScrollView scrollView = new ScrollView(this);
        scrollView.setLayoutParams(new LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT, LinearLayout.LayoutParams.MATCH_PARENT));
        scrollView.setBackgroundColor(Color.parseColor("#060913"));
        scrollView.setFillViewport(true);

        // Content Layout
        LinearLayout root = new LinearLayout(this);
        root.setOrientation(LinearLayout.VERTICAL);
        root.setLayoutParams(new LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT, LinearLayout.LayoutParams.WRAP_CONTENT));
        root.setPadding(48, 64, 48, 48);
        root.setGravity(Gravity.CENTER_HORIZONTAL);

        // Header Title
        TextView title = new TextView(this);
        title.setText("AEGIS PROTOCOL v3");
        title.setTextColor(Color.WHITE);
        title.setTextSize(22);
        title.setTypeface(null, Typeface.BOLD);
        title.setGravity(Gravity.CENTER);
        root.addView(title);

        TextView subtitle = new TextView(this);
        subtitle.setText("Autonomous Overlay & Transport Accelerator");
        subtitle.setTextColor(Color.parseColor("#818CF8"));
        subtitle.setTextSize(13);
        subtitle.setGravity(Gravity.CENTER);
        subtitle.setPadding(0, 8, 0, 48);
        root.addView(subtitle);

        // Status Badge
        mStatusBadge = new TextView(this);
        mStatusBadge.setText("DISCONNECTED");
        mStatusBadge.setTextColor(Color.parseColor("#EF4444"));
        mStatusBadge.setTextSize(14);
        mStatusBadge.setTypeface(null, Typeface.BOLD);
        mStatusBadge.setBackgroundColor(Color.parseColor("#181424"));
        mStatusBadge.setPadding(32, 20, 32, 20);
        mStatusBadge.setGravity(Gravity.CENTER);
        LinearLayout.LayoutParams badgeParams = new LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT, LinearLayout.LayoutParams.WRAP_CONTENT);
        mStatusBadge.setLayoutParams(badgeParams);
        root.addView(mStatusBadge);

        // Endpoint Config
        TextView endpointLabel = new TextView(this);
        endpointLabel.setText("CLOUD EDGE RELAY (UDP 443)");
        endpointLabel.setTextColor(Color.parseColor("#94A3B8"));
        endpointLabel.setTextSize(11);
        endpointLabel.setPadding(0, 48, 0, 8);
        LinearLayout.LayoutParams labelParams = new LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT, LinearLayout.LayoutParams.WRAP_CONTENT);
        endpointLabel.setLayoutParams(labelParams);
        root.addView(endpointLabel);

        mEndpointInput = new EditText(this);
        mEndpointInput.setText(mPrefs.getString(KEY_ENDPOINT, "192.168.1.119:4433"));
        mEndpointInput.setTextColor(Color.WHITE);
        mEndpointInput.setBackgroundColor(Color.parseColor("#0F172A"));
        mEndpointInput.setPadding(24, 24, 24, 24);
        mEndpointInput.setTextSize(14);
        LinearLayout.LayoutParams inputParams = new LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT, LinearLayout.LayoutParams.WRAP_CONTENT);
        mEndpointInput.setLayoutParams(inputParams);
        root.addView(mEndpointInput);

        // Connect Button
        mConnectBtn = new Button(this);
        mConnectBtn.setText("ACTIVATE SHIELD");
        mConnectBtn.setBackgroundColor(Color.parseColor("#4F46E5"));
        mConnectBtn.setTextColor(Color.WHITE);
        mConnectBtn.setTextSize(15);
        mConnectBtn.setTypeface(null, Typeface.BOLD);
        mConnectBtn.setPadding(0, 32, 0, 32);

        LinearLayout.LayoutParams btnParams = new LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT, LinearLayout.LayoutParams.WRAP_CONTENT);
        btnParams.setMargins(0, 48, 0, 32);
        mConnectBtn.setLayoutParams(btnParams);
        mConnectBtn.setOnClickListener(new View.OnClickListener() {
            @Override
            public void onClick(View v) {
                if (mIsConnected) {
                    disconnectShield();
                } else {
                    prepareAndConnect();
                }
            }
        });
        root.addView(mConnectBtn);

        // Live Metrics Card
        mMetricsText = new TextView(this);
        mMetricsText.setText("• Protocol: RFC 9221 QUIC Datagrams\n• Encryption: ChaCha20-Poly1305 / TLS 1.3\n• FEC Parity: Adaptive (10:1 to 6:4)\n• Congestion Control: Google BBRv3\n• MTU: 1280 (Carrier Fragment-Free)\n• Hotspot Bridge: 0.0.0.0:8080 (WPAD/SOCKS5)");
        mMetricsText.setTextColor(Color.parseColor("#64748B"));
        mMetricsText.setBackgroundColor(Color.parseColor("#080D1A"));
        mMetricsText.setPadding(32, 32, 32, 32);
        mMetricsText.setTextSize(12);
        mMetricsText.setLineSpacing(8, 1);
        LinearLayout.LayoutParams metricsParams = new LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT, LinearLayout.LayoutParams.WRAP_CONTENT);
        mMetricsText.setLayoutParams(metricsParams);
        root.addView(mMetricsText);

        scrollView.addView(root);
        setContentView(scrollView);
    }

    private void prepareAndConnect() {
        String endpoint = mEndpointInput.getText().toString().trim();
        mPrefs.edit().putString(KEY_ENDPOINT, endpoint).apply();

        Intent intent = VpnService.prepare(this);
        if (intent != null) {
            startActivityForResult(intent, VPN_REQUEST_CODE);
        } else {
            onActivityResult(VPN_REQUEST_CODE, RESULT_OK, null);
        }
    }

    @Override
    protected void onActivityResult(int requestCode, int resultCode, Intent data) {
        super.onActivityResult(requestCode, resultCode, data);
        if (requestCode == VPN_REQUEST_CODE) {
            if (resultCode == RESULT_OK) {
                startShieldService();
            } else {
                Toast.makeText(this, "VPN permission required to shield traffic", Toast.LENGTH_SHORT).show();
            }
        }
    }

    private void startShieldService() {
        Intent intent = new Intent(this, AegisVpnService.class);
        intent.setAction(AegisVpnService.ACTION_CONNECT);
        intent.putExtra(AegisVpnService.EXTRA_ENDPOINT, mEndpointInput.getText().toString().trim());
        startService(intent);

        mIsConnected = true;
        mStatusBadge.setText("PROTECTED & ACCELERATED");
        mStatusBadge.setTextColor(Color.parseColor("#10B981"));
        mConnectBtn.setText("DISCONNECT");
        mConnectBtn.setBackgroundColor(Color.parseColor("#DC2626"));
        Toast.makeText(this, "🛡️ Shield Active: All apps protected via Aegis v3", Toast.LENGTH_SHORT).show();
    }

    private void disconnectShield() {
        Intent intent = new Intent(this, AegisVpnService.class);
        intent.setAction(AegisVpnService.ACTION_DISCONNECT);
        startService(intent);

        mIsConnected = false;
        mStatusBadge.setText("DISCONNECTED");
        mStatusBadge.setTextColor(Color.parseColor("#EF4444"));
        mConnectBtn.setText("ACTIVATE SHIELD");
        mConnectBtn.setBackgroundColor(Color.parseColor("#4F46E5"));
    }
}
