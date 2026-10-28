package com.aegis.v3;

import android.util.Log;

public class AegisCore {
    private static final String TAG = "AegisCoreJNI";
    private static boolean sLibraryLoaded = false;

    static {
        try {
            System.loadLibrary("aegis");
            sLibraryLoaded = true;
            Log.i(TAG, "Native libaegis.so loaded successfully");
        } catch (UnsatisfiedLinkError e) {
            Log.e(TAG, "Failed to load libaegis.so: " + e.getMessage());
            sLibraryLoaded = false;
        }
    }

    public static boolean isLoaded() {
        return sLibraryLoaded;
    }

    public static native int startEngine(String endpoint, String serverName, int tunFd);
    public static native void updateRadioTelemetry(int rsrp, int rsrq, int rssnr, double loss);
    public static native void stopEngine();
}
