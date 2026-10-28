package com.aegis.v3;

import android.util.Log;

import java.io.BufferedReader;
import java.io.InputStream;
import java.io.InputStreamReader;
import java.io.OutputStream;
import java.net.InetAddress;
import java.net.ServerSocket;
import java.net.Socket;
import java.nio.charset.StandardCharsets;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.atomic.AtomicBoolean;

/**
 * HotspotBridge runs a local HTTP/SOCKS5 proxy and WPAD/PAC server on port 8080.
 * Tethered laptops and devices on the phone's Wi-Fi hotspot route through this bridge
 * to benefit from the active Aegis Protocol v3 accelerated overlay tunnel.
 */
public class HotspotBridge {
    private static final String TAG = "AegisHotspotBridge";
    public static final int PROXY_PORT = 8080;

    private ServerSocket mServerSocket;
    private final ExecutorService mThreadPool;
    private final AtomicBoolean mIsRunning = new AtomicBoolean(false);

    public HotspotBridge() {
        mThreadPool = Executors.newCachedThreadPool();
    }

    public synchronized void start() {
        if (mIsRunning.get()) return;

        try {
            mServerSocket = new ServerSocket(PROXY_PORT, 50, InetAddress.getByName("0.0.0.0"));
            mIsRunning.set(true);
            Log.i(TAG, "🛡️ Hotspot Bridge listening on 0.0.0.0:" + PROXY_PORT);

            mThreadPool.execute(new Runnable() {
                @Override
                public void run() {
                    while (mIsRunning.get()) {
                        try {
                            Socket clientSocket = mServerSocket.accept();
                            mThreadPool.execute(new ClientHandler(clientSocket));
                        } catch (Exception e) {
                            if (!mIsRunning.get()) break;
                            Log.w(TAG, "Hotspot accept error: " + e.getMessage());
                        }
                    }
                }
            });
        } catch (Exception e) {
            Log.e(TAG, "Failed to start Hotspot Bridge: " + e.getMessage(), e);
        }
    }

    public synchronized void stop() {
        mIsRunning.set(false);
        if (mServerSocket != null) {
            try {
                mServerSocket.close();
            } catch (Exception ignored) {}
            mServerSocket = null;
        }
        Log.i(TAG, "Hotspot Bridge stopped.");
    }

    public boolean isRunning() {
        return mIsRunning.get();
    }

    private static class ClientHandler implements Runnable {
        private final Socket mClientSocket;

        ClientHandler(Socket socket) {
            mClientSocket = socket;
        }

        @Override
        public void run() {
            try {
                InputStream in = mClientSocket.getInputStream();
                OutputStream out = mClientSocket.getOutputStream();

                // Peek first byte to distinguish SOCKS5 (0x05) from HTTP
                mClientSocket.setSoTimeout(10000);
                int firstByte = in.read();
                if (firstByte == -1) {
                    mClientSocket.close();
                    return;
                }

                if (firstByte == 0x05) {
                    handleSocks5(firstByte, in, out);
                } else {
                    handleHttp(firstByte, in, out);
                }
            } catch (Exception e) {
                // Connection closed or timed out
            } finally {
                try {
                    mClientSocket.close();
                } catch (Exception ignored) {}
            }
        }

        private void handleHttp(int firstByte, InputStream in, OutputStream out) throws Exception {
            StringBuilder lineBuf = new StringBuilder();
            lineBuf.append((char) firstByte);

            int b;
            while ((b = in.read()) != -1) {
                if (b == '\n') break;
                if (b != '\r') lineBuf.append((char) b);
            }

            String requestLine = lineBuf.toString();
            String[] parts = requestLine.split(" ");
            if (parts.length < 2) return;

            String method = parts[0];
            String uri = parts[1];

            // Serve WPAD / PAC script for 1-click zero-config on laptops
            if (uri.endsWith("/wpad.dat") || uri.endsWith("/proxy.pac")) {
                String pacScript = "function FindProxyForURL(url, host) { return 'PROXY 192.168.43.1:8080; DIRECT'; }\n";
                byte[] pacBytes = pacScript.getBytes(StandardCharsets.UTF_8);
                String response = "HTTP/1.1 200 OK\r\n" +
                                 "Content-Type: application/x-ns-proxy-autoconfig\r\n" +
                                 "Content-Length: " + pacBytes.length + "\r\n" +
                                 "Connection: close\r\n\r\n";
                out.write(response.getBytes(StandardCharsets.UTF_8));
                out.write(pacBytes);
                out.flush();
                return;
            }

            // HTTP CONNECT Tunneling (HTTPS)
            if ("CONNECT".equalsIgnoreCase(method)) {
                String[] hostPort = uri.split(":");
                String host = hostPort[0];
                int port = hostPort.length > 1 ? Integer.parseInt(hostPort[1]) : 443;

                // Drain remaining HTTP headers
                BufferedReader reader = new BufferedReader(new InputStreamReader(in));
                String header;
                while ((header = reader.readLine()) != null) {
                    if (header.isEmpty()) break;
                }

                // Connect outbound (captured by VpnService)
                Socket targetSocket = new Socket(host, port);
                out.write("HTTP/1.1 200 Connection Established\r\n\r\n".getBytes(StandardCharsets.UTF_8));
                out.flush();

                spliceSockets(mClientSocket, targetSocket);
            }
        }

        private void handleSocks5(int firstByte, InputStream in, OutputStream out) throws Exception {
            int nmethods = in.read();
            byte[] methods = new byte[nmethods];
            int read = in.read(methods);
            if (read < nmethods) return;

            // NO AUTH (0x00)
            out.write(new byte[]{0x05, 0x00});
            out.flush();

            // Request header
            byte[] header = new byte[4];
            if (in.read(header) < 4) return;

            if (header[1] != 0x01) { // CONNECT only
                out.write(new byte[]{0x05, 0x07, 0x00, 0x01, 0, 0, 0, 0, 0, 0});
                return;
            }

            String targetHost;
            byte atyp = header[3];
            if (atyp == 0x01) { // IPv4
                byte[] ip = new byte[4];
                in.read(ip);
                targetHost = InetAddress.getByAddress(ip).getHostAddress();
            } else if (atyp == 0x03) { // Domain
                int len = in.read();
                byte[] domain = new byte[len];
                in.read(domain);
                targetHost = new String(domain, StandardCharsets.UTF_8);
            } else {
                out.write(new byte[]{0x05, 0x08, 0x00, 0x01, 0, 0, 0, 0, 0, 0});
                return;
            }

            int p1 = in.read();
            int p2 = in.read();
            int targetPort = ((p1 & 0xFF) << 8) | (p2 & 0xFF);

            Socket targetSocket = new Socket(targetHost, targetPort);
            out.write(new byte[]{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0});
            out.flush();

            spliceSockets(mClientSocket, targetSocket);
        }

        private void spliceSockets(final Socket a, final Socket b) {
            Thread t1 = new Thread(new Runnable() {
                @Override
                public void run() {
                    try {
                        InputStream in = a.getInputStream();
                        OutputStream out = b.getOutputStream();
                        byte[] buf = new byte[16384];
                        int n;
                        while ((n = in.read(buf)) != -1) {
                            out.write(buf, 0, n);
                        }
                    } catch (Exception ignored) {}
                    try { b.close(); } catch (Exception ignored) {}
                }
            });

            Thread t2 = new Thread(new Runnable() {
                @Override
                public void run() {
                    try {
                        InputStream in = b.getInputStream();
                        OutputStream out = a.getOutputStream();
                        byte[] buf = new byte[16384];
                        int n;
                        while ((n = in.read(buf)) != -1) {
                            out.write(buf, 0, n);
                        }
                    } catch (Exception ignored) {}
                    try { a.close(); } catch (Exception ignored) {}
                }
            });

            t1.start();
            t2.start();
            try {
                t1.join();
                t2.join();
            } catch (InterruptedException ignored) {}
        }
    }
}
