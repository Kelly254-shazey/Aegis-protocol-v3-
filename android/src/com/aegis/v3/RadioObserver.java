package com.aegis.v3;

import android.content.Context;
import android.os.Build;
import android.telephony.CellInfo;
import android.telephony.CellInfoLte;
import android.telephony.CellInfoNr;
import android.telephony.CellSignalStrengthLte;
import android.telephony.CellSignalStrengthNr;
import android.telephony.PhoneStateListener;
import android.telephony.SignalStrength;
import android.telephony.TelephonyCallback;
import android.telephony.TelephonyManager;
import android.util.Log;

import java.util.List;

public class RadioObserver {
    private static final String TAG = "AegisRadioObserver";

    private final Context mContext;
    private final TelephonyManager mTelephonyManager;
    private boolean mIsObserving = false;

    // Last known metrics
    private int mLastRSRP = -95;
    private int mLastRSRQ = -10;
    private int mLastRSSNR = 15;

    public RadioObserver(Context context) {
        this.mContext = context;
        this.mTelephonyManager = (TelephonyManager) context.getSystemService(Context.TELEPHONY_SERVICE);
    }

    public synchronized void start() {
        if (mIsObserving || mTelephonyManager == null) return;

        try {
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
                mTelephonyManager.registerTelephonyCallback(
                        mContext.getMainExecutor(),
                        new TelephonyCallback() {
                            // Android 12+ Callback
                        }
                );
            } else {
                mTelephonyManager.listen(mLegacyListener, PhoneStateListener.LISTEN_SIGNAL_STRENGTHS | PhoneStateListener.LISTEN_CELL_INFO);
            }
            mIsObserving = true;
            Log.i(TAG, "Cellular radio telemetry observer active");
        } catch (SecurityException se) {
            Log.w(TAG, "Location permission not granted for fine cellular metrics: " + se.getMessage());
        }
    }

    public synchronized void stop() {
        if (!mIsObserving || mTelephonyManager == null) return;
        try {
            mTelephonyManager.listen(mLegacyListener, PhoneStateListener.LISTEN_NONE);
            mIsObserving = false;
        } catch (Exception ignored) {}
    }

    private final PhoneStateListener mLegacyListener = new PhoneStateListener() {
        @Override
        public void onSignalStrengthsChanged(SignalStrength signalStrength) {
            super.onSignalStrengthsChanged(signalStrength);
            processSignalStrength(signalStrength);
        }

        @Override
        public void onCellInfoChanged(List<CellInfo> cellInfo) {
            super.onCellInfoChanged(cellInfo);
            if (cellInfo != null && !cellInfo.isEmpty()) {
                for (CellInfo info : cellInfo) {
                    if (info instanceof CellInfoLte) {
                        CellSignalStrengthLte lte = ((CellInfoLte) info).getCellSignalStrength();
                        mLastRSRP = lte.getRsrp();
                        mLastRSRQ = lte.getRsrq();
                        mLastRSSNR = lte.getRssnr();
                        dispatchTelemetry();
                        break;
                    }
                }
            }
        }
    };

    private void processSignalStrength(SignalStrength signalStrength) {
        if (signalStrength == null) return;
        int level = signalStrength.getLevel(); // 0 (None) to 4 (Great)
        double estimatedLoss = (level <= 1) ? 8.0 : (level == 2 ? 3.0 : 0.2);

        // Feed to Go engine
        AegisCore.updateTelemetry(mLastRSRP, mLastRSRQ, mLastRSSNR, estimatedLoss);
    }

    private void dispatchTelemetry() {
        double estimatedLoss = (mLastRSRP < -110) ? 10.0 : (mLastRSRP < -100 ? 4.0 : 0.5);
        AegisCore.updateTelemetry(mLastRSRP, mLastRSRQ, mLastRSSNR, estimatedLoss);
    }
}
