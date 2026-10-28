package com.aegis.v3;

import android.app.Activity;
import android.app.AlertDialog;
import android.content.ClipData;
import android.content.ClipboardManager;
import android.content.Context;
import android.content.DialogInterface;
import android.content.Intent;
import android.content.SharedPreferences;
import android.graphics.Color;
import android.graphics.Typeface;
import android.graphics.drawable.GradientDrawable;
import android.net.VpnService;
import android.os.Bundle;
import android.util.TypedValue;
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
    private static final String KEY_ACCESS_KEY = "access_pass_key";
    private static final String DEFAULT_ENDPOINT = "192.168.1.119:4433";
    private static final String DEFAULT_KEY = "AEGIS-V3.STARLINK-PREMIUM-PASS";

    private boolean mIsConnected = false;
    private TextView mStatusDot;
    private TextView mStatusText;
    private Button mHeroBtn;
    private EditText mKeyInput;
    private SharedPreferences mPrefs;
    private GradientDrawable mHeroBg;

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        mPrefs = getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE);

        // Apple Deep Black Canvas
        ScrollView scrollView = new ScrollView(this);
        scrollView.setLayoutParams(new LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT, LinearLayout.LayoutParams.MATCH_PARENT));
        scrollView.setBackgroundColor(Color.parseColor("#000000"));
        scrollView.setFillViewport(true);

        LinearLayout root = new LinearLayout(this);
        root.setOrientation(LinearLayout.VERTICAL);
        root.setLayoutParams(new LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT, LinearLayout.LayoutParams.WRAP_CONTENT));
        root.setPadding(dpToPx(24), dpToPx(48), dpToPx(24), dpToPx(48));

        // 1. Apple Header: Top Pill Badge
        TextView starlinkBadge = new TextView(this);
        starlinkBadge.setText("🛰️  STARLINK DIRECT LEO ACTIVE");
        starlinkBadge.setTextColor(Color.parseColor("#38BDF8"));
        starlinkBadge.setTextSize(TypedValue.COMPLEX_UNIT_SP, 11);
        starlinkBadge.setTypeface(null, Typeface.BOLD);
        starlinkBadge.setGravity(Gravity.CENTER);

        GradientDrawable badgeBg = new GradientDrawable();
        badgeBg.setColor(Color.parseColor("#082F49"));
        badgeBg.setCornerRadius(dpToPx(20));
        badgeBg.setStroke(dpToPx(1), Color.parseColor("#0284C7"));
        starlinkBadge.setBackground(badgeBg);
        starlinkBadge.setPadding(dpToPx(16), dpToPx(6), dpToPx(16), dpToPx(6));

        LinearLayout.LayoutParams badgeParams = new LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.WRAP_CONTENT, LinearLayout.LayoutParams.WRAP_CONTENT);
        badgeParams.gravity = Gravity.CENTER_HORIZONTAL;
        badgeParams.setMargins(0, 0, 0, dpToPx(20));
        starlinkBadge.setLayoutParams(badgeParams);

        // Long-click badge to configure relay endpoint if ever needed
        starlinkBadge.setOnLongClickListener(new View.OnLongClickListener() {
            @Override
            public boolean onLongClick(View v) {
                showGatewayConfigDialog();
                return true;
            }
        });
        root.addView(starlinkBadge);

        // App Title
        TextView title = new TextView(this);
        title.setText("Aegis Network");
        title.setTextColor(Color.WHITE);
        title.setTextSize(TypedValue.COMPLEX_UNIT_SP, 28);
        title.setTypeface(null, Typeface.BOLD);
        title.setGravity(Gravity.CENTER);
        root.addView(title);

        TextView subtitle = new TextView(this);
        subtitle.setText("Autonomous Zero-Drop Transport Accelerator");
        subtitle.setTextColor(Color.parseColor("#8E8E93"));
        subtitle.setTextSize(TypedValue.COMPLEX_UNIT_SP, 13);
        subtitle.setGravity(Gravity.CENTER);
        subtitle.setPadding(0, dpToPx(6), 0, dpToPx(44));
        root.addView(subtitle);

        // 2. Apple Hero Switch Section
        LinearLayout heroContainer = new LinearLayout(this);
        heroContainer.setOrientation(LinearLayout.VERTICAL);
        heroContainer.setGravity(Gravity.CENTER);
        heroContainer.setPadding(0, 0, 0, dpToPx(44));

        mHeroBtn = new Button(this);
        mHeroBg = new GradientDrawable();
        mHeroBg.setColor(Color.parseColor("#1C1C1E"));
        mHeroBg.setCornerRadius(dpToPx(40));
        mHeroBg.setStroke(dpToPx(2), Color.parseColor("#5E5CE6"));
        mHeroBtn.setBackground(mHeroBg);
        mHeroBtn.setText("⚡ CONNECT");
        mHeroBtn.setTextColor(Color.WHITE);
        mHeroBtn.setTextSize(TypedValue.COMPLEX_UNIT_SP, 17);
        mHeroBtn.setTypeface(null, Typeface.BOLD);

        LinearLayout.LayoutParams heroParams = new LinearLayout.LayoutParams(
                dpToPx(240), dpToPx(76));
        heroParams.gravity = Gravity.CENTER_HORIZONTAL;
        mHeroBtn.setLayoutParams(heroParams);

        // One-tap connect: No redundant prompt dialogs
        mHeroBtn.setOnClickListener(new View.OnClickListener() {
            @Override
            public void onClick(View v) {
                if (mIsConnected) {
                    disconnectShield();
                } else {
                    prepareAndConnect();
                }
            }
        });
        heroContainer.addView(mHeroBtn);

        // Status Indicator Pill
        LinearLayout statusRow = new LinearLayout(this);
        statusRow.setOrientation(LinearLayout.HORIZONTAL);
        statusRow.setGravity(Gravity.CENTER);
        statusRow.setPadding(0, dpToPx(14), 0, 0);

        mStatusDot = new TextView(this);
        mStatusDot.setText("● ");
        mStatusDot.setTextColor(Color.parseColor("#EF4444"));
        mStatusDot.setTextSize(TypedValue.COMPLEX_UNIT_SP, 13);
        statusRow.addView(mStatusDot);

        mStatusText = new TextView(this);
        mStatusText.setText("Ready to Connect");
        mStatusText.setTextColor(Color.parseColor("#8E8E93"));
        mStatusText.setTextSize(TypedValue.COMPLEX_UNIT_SP, 13);
        mStatusText.setTypeface(null, Typeface.BOLD);
        statusRow.addView(mStatusText);

        heroContainer.addView(statusRow);
        root.addView(heroContainer);

        // 3. Apple Minimalist Grouped Card: Access Pass Key
        LinearLayout keyCard = new LinearLayout(this);
        keyCard.setOrientation(LinearLayout.VERTICAL);
        keyCard.addView(createSectionHeader("AEGIS ACCESS PASS"));

        LinearLayout keyRow = new LinearLayout(this);
        keyRow.setOrientation(LinearLayout.HORIZONTAL);
        keyRow.setGravity(Gravity.CENTER_VERTICAL);

        mKeyInput = new EditText(this);
        mKeyInput.setText(mPrefs.getString(KEY_ACCESS_KEY, DEFAULT_KEY));
        mKeyInput.setTextColor(Color.WHITE);
        mKeyInput.setTextSize(TypedValue.COMPLEX_UNIT_SP, 12);
        mKeyInput.setBackground(null);
        mKeyInput.setPadding(0, dpToPx(8), dpToPx(8), dpToPx(8));
        mKeyInput.setSingleLine(true);
        LinearLayout.LayoutParams keyParams = new LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 1.0f);
        mKeyInput.setLayoutParams(keyParams);
        keyRow.addView(mKeyInput);

        Button pasteBtn = new Button(this);
        pasteBtn.setText("PASTE");
        pasteBtn.setTextSize(TypedValue.COMPLEX_UNIT_SP, 11);
        pasteBtn.setTextColor(Color.parseColor("#5E5CE6"));
        pasteBtn.setTypeface(null, Typeface.BOLD);

        GradientDrawable pasteBg = new GradientDrawable();
        pasteBg.setColor(Color.parseColor("#2C2C2E"));
        pasteBg.setCornerRadius(dpToPx(8));
        pasteBtn.setBackground(pasteBg);
        pasteBtn.setPadding(dpToPx(12), dpToPx(4), dpToPx(12), dpToPx(4));
        pasteBtn.setOnClickListener(new View.OnClickListener() {
            @Override
            public void onClick(View v) {
                ClipboardManager cm = (ClipboardManager) getSystemService(Context.CLIPBOARD_SERVICE);
                if (cm != null && cm.hasPrimaryClip() && cm.getPrimaryClip().getItemCount() > 0) {
                    CharSequence text = cm.getPrimaryClip().getItemAt(0).getText();
                    if (text != null && text.length() > 0) {
                        mKeyInput.setText(text.toString().trim());
                        mPrefs.edit().putString(KEY_ACCESS_KEY, text.toString().trim()).apply();
                        Toast.makeText(MainActivity.this, "Access Key Updated", Toast.LENGTH_SHORT).show();
                    }
                }
            }
        });
        keyRow.addView(pasteBtn);
        keyCard.addView(keyRow);
        root.addView(createAppleCard(keyCard));

        scrollView.addView(root);
        setContentView(scrollView);
    }

    private void showGatewayConfigDialog() {
        AlertDialog.Builder builder = new AlertDialog.Builder(this, android.R.style.Theme_DeviceDefault_Dialog_Alert);
        builder.setTitle("Gateway Configuration");
        final EditText input = new EditText(this);
        input.setText(mPrefs.getString(KEY_ENDPOINT, DEFAULT_ENDPOINT));
        input.setTextColor(Color.WHITE);
        builder.setView(input);
        builder.setPositiveButton("Save", new DialogInterface.OnClickListener() {
            @Override
            public void onClick(DialogInterface dialog, int which) {
                String ep = input.getText().toString().trim();
                if (!ep.isEmpty()) {
                    mPrefs.edit().putString(KEY_ENDPOINT, ep).apply();
                    Toast.makeText(MainActivity.this, "Gateway: " + ep, Toast.LENGTH_SHORT).show();
                }
            }
        });
        builder.setNegativeButton("Cancel", null);
        builder.show();
    }

    private void prepareAndConnect() {
        String accessKey = mKeyInput.getText().toString().trim();
        mPrefs.edit().putString(KEY_ACCESS_KEY, accessKey).apply();

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
                Toast.makeText(this, "Permission required to connect", Toast.LENGTH_SHORT).show();
            }
        }
    }

    private void startShieldService() {
        String endpoint = mPrefs.getString(KEY_ENDPOINT, DEFAULT_ENDPOINT);
        Intent intent = new Intent(this, AegisVpnService.class);
        intent.setAction(AegisVpnService.ACTION_CONNECT);
        intent.putExtra(AegisVpnService.EXTRA_ENDPOINT, endpoint);
        startService(intent);

        mIsConnected = true;
        mHeroBg.setColor(Color.parseColor("#30D158"));
        mHeroBg.setStroke(dpToPx(2), Color.parseColor("#34C759"));
        mHeroBtn.setText("✓ CONNECTED");
        mHeroBtn.setTextColor(Color.BLACK);

        mStatusDot.setTextColor(Color.parseColor("#30D158"));
        mStatusText.setText("Protected via Starlink LEO");
        mStatusText.setTextColor(Color.parseColor("#30D158"));

        Toast.makeText(this, "Connected to Aegis Network", Toast.LENGTH_SHORT).show();
    }

    private void disconnectShield() {
        Intent intent = new Intent(this, AegisVpnService.class);
        intent.setAction(AegisVpnService.ACTION_DISCONNECT);
        startService(intent);

        mIsConnected = false;
        mHeroBg.setColor(Color.parseColor("#1C1C1E"));
        mHeroBg.setStroke(dpToPx(2), Color.parseColor("#5E5CE6"));
        mHeroBtn.setText("⚡ CONNECT");
        mHeroBtn.setTextColor(Color.WHITE);

        mStatusDot.setTextColor(Color.parseColor("#EF4444"));
        mStatusText.setText("Ready to Connect");
        mStatusText.setTextColor(Color.parseColor("#8E8E93"));
    }

    private View createAppleCard(View content) {
        GradientDrawable cardBg = new GradientDrawable();
        cardBg.setColor(Color.parseColor("#1C1C1E"));
        cardBg.setCornerRadius(dpToPx(16));
        cardBg.setStroke(dpToPx(1), Color.parseColor("#2C2C2E"));

        LinearLayout card = new LinearLayout(this);
        card.setOrientation(LinearLayout.VERTICAL);
        card.setBackground(cardBg);
        card.setPadding(dpToPx(18), dpToPx(16), dpToPx(18), dpToPx(16));

        LinearLayout.LayoutParams params = new LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT, LinearLayout.LayoutParams.WRAP_CONTENT);
        params.setMargins(0, 0, 0, dpToPx(16));
        card.setLayoutParams(params);
        card.addView(content);
        return card;
    }

    private TextView createSectionHeader(String title) {
        TextView tv = new TextView(this);
        tv.setText(title);
        tv.setTextColor(Color.parseColor("#8E8E93"));
        tv.setTextSize(TypedValue.COMPLEX_UNIT_SP, 11);
        tv.setTypeface(null, Typeface.BOLD);
        tv.setLetterSpacing(0.08f);
        tv.setPadding(0, 0, 0, dpToPx(10));
        return tv;
    }

    private int dpToPx(int dp) {
        return (int) (dp * getResources().getDisplayMetrics().density + 0.5f);
    }
}
