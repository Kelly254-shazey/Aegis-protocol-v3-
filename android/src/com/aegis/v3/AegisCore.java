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

    // Canonical JNI Native Bindings
    public static native int startEngineNative(String endpoint, String serverName, int tunFd);
    public static native void updateTelemetryNative(int rsrp, int rsrq, int rssnr, double loss);
    public static native void stopEngineNative();

    // Friendly Wrapper Methods
    public static int start(String endpoint, String serverName, int tunFd) {
        if (!sLibraryLoaded) return -1;
        return startEngineNative(endpoint, serverName, tunFd);
    }

    public static void updateTelemetry(int rsrp, int rsrq, int rssnr, double loss) {
        if (!sLibraryLoaded) return;
        updateTelemetryNative(rsrp, rsrq, rssnr, loss);
    }

    public static void stop() {
        if (!sLibraryLoaded) return;
        stopEngineNative();
    }
}
