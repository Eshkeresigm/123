package com.chessvision.app;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.app.Service;
import android.content.Context;
import android.content.Intent;
import android.content.pm.ServiceInfo;
import android.content.res.Configuration;
import android.graphics.Bitmap;
import android.graphics.Color;
import android.graphics.PixelFormat;
import android.graphics.Typeface;
import android.graphics.drawable.GradientDrawable;
import android.hardware.display.DisplayManager;
import android.hardware.display.VirtualDisplay;
import android.media.Image;
import android.media.ImageReader;
import android.media.projection.MediaProjection;
import android.media.projection.MediaProjectionManager;
import android.os.Build;
import android.os.Handler;
import android.os.HandlerThread;
import android.os.IBinder;
import android.os.Looper;
import android.util.DisplayMetrics;
import android.view.Gravity;
import android.view.MotionEvent;
import android.view.View;
import android.view.WindowManager;
import android.widget.LinearLayout;
import android.widget.TextView;

import org.json.JSONArray;
import org.json.JSONObject;

import java.nio.ByteBuffer;
import java.util.Locale;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;

import cvmobile.Cvmobile;
import cvmobile.Host;
import cvmobile.Session;

/**
 * Foreground service: captures the screen with MediaProjection, feeds frames to the Go core
 * and shows the floating panel and the arrow window.
 */
public final class CaptureService extends Service {
    static final String EXTRA_CODE = "code";
    static final String EXTRA_DATA = "data";
    private static final String ACTION_STOP = "com.chessvision.app.STOP";
    private static final String CHANNEL = "chessvision";

    private static final int INK = 0xFF1B2340;
    private static final int MUTED = 0xFF5B6585;
    private static final int ACCENT = 0xFF5B5BF0;
    private static final long MIN_FRAME_GAP_MS = 110;
    private static final int MAX_SHORT_SIDE = 1080;

    private final Handler main = new Handler(Looper.getMainLooper());
    private HandlerThread bgThread;
    private Handler bg;

    private WindowManager wm;
    private MediaProjection projection;
    private VirtualDisplay display;
    private ImageReader reader;
    private Session session;

    private int screenW, screenH, densityDpi;
    private int frameW, frameH;
    private float scale = 1f; // frame pixels per screen pixel

    private ArrowView arrow;
    private LinearLayout panel;
    private WindowManager.LayoutParams panelLp;
    private TextView bestView, evalView, statusView;
    private TextView runBtn;
    private LinearLayout details;
    private TextView collapseBtn;
    private Bitmap overlayBmp;

    private volatile boolean stopped;
    private boolean scheduled;
    private long lastPush;
    private final int[] loc = new int[2];

    @Override
    public IBinder onBind(Intent intent) {
        return null;
    }

    @Override
    public int onStartCommand(Intent intent, int flags, int startId) {
        if (intent != null && ACTION_STOP.equals(intent.getAction())) {
            stopSelf();
            return START_NOT_STICKY;
        }
        if (session != null) {
            return START_NOT_STICKY;
        }
        if (intent == null) {
            stopSelf();
            return START_NOT_STICKY;
        }
        startAsForeground();
        int code = intent.getIntExtra(EXTRA_CODE, 0);
        Intent data = intent.getParcelableExtra(EXTRA_DATA);
        if (data == null) {
            stopSelf();
            return START_NOT_STICKY;
        }
        try {
            begin(code, data);
        } catch (RuntimeException e) {
            stopSelf();
        }
        return START_NOT_STICKY;
    }

    // ---- foreground notification ----------------------------------------------------------

    private void startAsForeground() {
        NotificationManager nm = (NotificationManager) getSystemService(Context.NOTIFICATION_SERVICE);
        if (Build.VERSION.SDK_INT >= 26) {
            nm.createNotificationChannel(new NotificationChannel(CHANNEL, "ChessVision",
                    NotificationManager.IMPORTANCE_LOW));
        }
        Intent stop = new Intent(this, CaptureService.class).setAction(ACTION_STOP);
        PendingIntent pi = PendingIntent.getService(this, 0, stop, PendingIntent.FLAG_IMMUTABLE);
        Notification.Builder nb = Build.VERSION.SDK_INT >= 26
                ? new Notification.Builder(this, CHANNEL) : new Notification.Builder(this);
        nb.setSmallIcon(R.drawable.ic_stat)
                .setContentTitle("ChessVision работает")
                .setContentText("Анализ шахматной доски на экране")
                .setOngoing(true)
                .setContentIntent(pi);
        Notification n = nb.build();
        if (Build.VERSION.SDK_INT >= 29) {
            startForeground(1, n, ServiceInfo.FOREGROUND_SERVICE_TYPE_MEDIA_PROJECTION);
        } else {
            startForeground(1, n);
        }
    }

    // ---- start-up -------------------------------------------------------------------------

    private void begin(int code, Intent data) {
        wm = (WindowManager) getSystemService(Context.WINDOW_SERVICE);
        bgThread = new HandlerThread("cv-bg");
        bgThread.start();
        bg = new Handler(bgThread.getLooper());

        readScreen();
        session = Cvmobile.newSession(host);

        MediaProjectionManager mpm = (MediaProjectionManager) getSystemService(Context.MEDIA_PROJECTION_SERVICE);
        projection = mpm.getMediaProjection(code, data);
        projection.registerCallback(new MediaProjection.Callback() {
            @Override
            public void onStop() {
                main.post(new Runnable() {
                    @Override
                    public void run() {
                        stopSelf();
                    }
                });
            }
        }, main);
        startCapture();

        arrow = new ArrowView(this);
        wm.addView(arrow, arrowParams());
        buildPanel();
        wm.addView(panel, panelLp);
        bg.post(poll);
    }

    private void readScreen() {
        DisplayMetrics dm = new DisplayMetrics();
        wm.getDefaultDisplay().getRealMetrics(dm);
        screenW = dm.widthPixels;
        screenH = dm.heightPixels;
        densityDpi = dm.densityDpi;
        float s = Math.min(1f, (float) MAX_SHORT_SIDE / Math.min(screenW, screenH));
        frameW = Math.max(16, Math.round(screenW * s) & ~1);
        frameH = Math.max(16, Math.round(screenH * s) & ~1);
        scale = (float) frameW / screenW;
    }

    private void startCapture() {
        reader = newReader();
        display = projection.createVirtualDisplay("chessvision", frameW, frameH, densityDpi,
                DisplayManager.VIRTUAL_DISPLAY_FLAG_AUTO_MIRROR, reader.getSurface(), null, bg);
    }

    private ImageReader newReader() {
        ImageReader r = ImageReader.newInstance(frameW, frameH, PixelFormat.RGBA_8888, 2);
        r.setOnImageAvailableListener(new ImageReader.OnImageAvailableListener() {
            @Override
            public void onImageAvailable(ImageReader rd) {
                requestPump(rd);
            }
        }, bg);
        return r;
    }

    // ---- frame pump (runs on the background thread) -----------------------------------------

    private void requestPump(final ImageReader rd) {
        if (scheduled || stopped) {
            return;
        }
        scheduled = true;
        long wait = Math.max(0, MIN_FRAME_GAP_MS - (System.currentTimeMillis() - lastPush));
        bg.postDelayed(new Runnable() {
            @Override
            public void run() {
                scheduled = false;
                pump(rd);
            }
        }, wait);
    }

    private void pump(ImageReader rd) {
        if (stopped || rd != reader) {
            return;
        }
        Image img = null;
        try {
            img = rd.acquireLatestImage();
            if (img == null) {
                return;
            }
            Image.Plane p = img.getPlanes()[0];
            ByteBuffer buf = p.getBuffer();
            byte[] arr = new byte[buf.remaining()];
            buf.get(arr);
            lastPush = System.currentTimeMillis();
            session.pushFrame(img.getWidth(), img.getHeight(), p.getRowStride(), arr);
        } catch (RuntimeException ignored) {
            // a closed reader during rotation or shutdown
        } finally {
            if (img != null) {
                img.close();
            }
        }
    }

    // ---- polling the Go core ---------------------------------------------------------------

    private final Runnable poll = new Runnable() {
        @Override
        public void run() {
            if (stopped) {
                return;
            }
            try {
                final String js = session.state();
                main.post(new Runnable() {
                    @Override
                    public void run() {
                        applyState(js);
                    }
                });
            } catch (RuntimeException ignored) {
                // session closed
            }
            bg.postDelayed(this, 250);
        }
    };

    private void applyState(String js) {
        if (stopped || panel == null) {
            return;
        }
        try {
            JSONObject o = new JSONObject(js);
            boolean running = o.optBoolean("running");
            runBtn.setText(running ? "Пауза" : "Старт");
            String best = o.optString("best", "");
            String result = o.optString("result", "");
            if (!result.isEmpty()) {
                bestView.setText(result);
            } else if (running && !best.isEmpty()) {
                bestView.setText(o.optString("bestSan", best) + "  " + best.substring(0, 2) + "→" + best.substring(2, 4));
            } else {
                bestView.setText("—");
            }
            if (o.optBoolean("isMate")) {
                evalView.setText("мат в " + Math.abs(o.optInt("mate")));
            } else if (running) {
                evalView.setText(String.format(Locale.US, "%+.1f", o.optDouble("eval")));
            } else {
                evalView.setText("");
            }
            statusView.setText(o.optString("status", ""));
            int kind = o.optInt("kind");
            statusView.setTextColor(kind == 1 ? 0xFF1F8F55 : kind == 2 ? 0xFFB26A00 : kind == 3 ? 0xFFC62828 : MUTED);
            reportPanelRect();
        } catch (Exception ignored) {
            // malformed state is never expected; keep the previous picture
        }
    }

    private void reportPanelRect() {
        if (panel.getVisibility() != View.VISIBLE || panel.getWidth() == 0) {
            return;
        }
        panel.getLocationOnScreen(loc);
        final int x = Math.round(loc[0] * scale);
        final int y = Math.round(loc[1] * scale);
        final int w = Math.round(panel.getWidth() * scale);
        final int h = Math.round(panel.getHeight() * scale);
        bg.post(new Runnable() {
            @Override
            public void run() {
                if (!stopped) {
                    session.setPanelRect(x, y, w, h);
                }
            }
        });
    }

    // ---- host callbacks (called by Go on its own threads) -----------------------------------

    private final Host host = new Host() {
        @Override
        public void setSelfVisible(final boolean visible) {
            final CountDownLatch done = new CountDownLatch(1);
            main.post(new Runnable() {
                @Override
                public void run() {
                    int v = visible ? View.VISIBLE : View.INVISIBLE;
                    if (arrow != null) {
                        arrow.setVisibility(v);
                    }
                    if (panel != null) {
                        panel.setVisibility(v);
                    }
                    done.countDown();
                }
            });
            try {
                done.await(500, TimeUnit.MILLISECONDS);
            } catch (InterruptedException ignored) {
                Thread.currentThread().interrupt();
            }
        }

        @Override
        public void showOverlay(final long x, final long y, final long w, final long h, final byte[] rgba) {
            main.post(new Runnable() {
                @Override
                public void run() {
                    if (stopped || arrow == null || w <= 0 || h <= 0 || rgba.length < w * h * 4) {
                        return;
                    }
                    if (overlayBmp == null || overlayBmp.getWidth() != w || overlayBmp.getHeight() != h) {
                        overlayBmp = Bitmap.createBitmap((int) w, (int) h, Bitmap.Config.ARGB_8888);
                    }
                    overlayBmp.copyPixelsFromBuffer(ByteBuffer.wrap(rgba));
                    arrow.setArrow(overlayBmp, x / scale, y / scale, (x + w) / scale, (y + h) / scale);
                }
            });
        }

        @Override
        public void hideOverlay() {
            main.post(new Runnable() {
                @Override
                public void run() {
                    if (arrow != null) {
                        arrow.clearArrow();
                    }
                }
            });
        }
    };

    // ---- windows ---------------------------------------------------------------------------

    private int dp(int v) {
        return Math.round(v * getResources().getDisplayMetrics().density);
    }

    private int overlayType() {
        return Build.VERSION.SDK_INT >= 26 ? WindowManager.LayoutParams.TYPE_APPLICATION_OVERLAY
                : WindowManager.LayoutParams.TYPE_PHONE;
    }

    private WindowManager.LayoutParams arrowParams() {
        WindowManager.LayoutParams lp = new WindowManager.LayoutParams(screenW, screenH, overlayType(),
                WindowManager.LayoutParams.FLAG_NOT_FOCUSABLE
                        | WindowManager.LayoutParams.FLAG_NOT_TOUCHABLE
                        | WindowManager.LayoutParams.FLAG_LAYOUT_IN_SCREEN
                        | WindowManager.LayoutParams.FLAG_LAYOUT_NO_LIMITS,
                PixelFormat.TRANSLUCENT);
        lp.gravity = Gravity.TOP | Gravity.START;
        lp.x = 0;
        lp.y = 0;
        if (Build.VERSION.SDK_INT >= 28) {
            lp.layoutInDisplayCutoutMode = WindowManager.LayoutParams.LAYOUT_IN_DISPLAY_CUTOUT_MODE_SHORT_EDGES;
        }
        return lp;
    }

    private TextView button(String text, boolean primary) {
        TextView t = new TextView(this);
        t.setText(text);
        t.setTextSize(14);
        t.setTypeface(Typeface.DEFAULT_BOLD);
        t.setGravity(Gravity.CENTER);
        t.setPadding(dp(12), dp(9), dp(12), dp(9));
        t.setTextColor(primary ? Color.WHITE : INK);
        GradientDrawable bgd = new GradientDrawable();
        bgd.setCornerRadius(dp(12));
        bgd.setColor(primary ? ACCENT : 0xFFEEF1F9);
        t.setBackground(bgd);
        return t;
    }

    private void buildPanel() {
        panel = new LinearLayout(this);
        panel.setOrientation(LinearLayout.VERTICAL);
        panel.setPadding(dp(14), dp(10), dp(14), dp(12));
        GradientDrawable card = new GradientDrawable();
        card.setColor(0xF5FFFFFF);
        card.setCornerRadius(dp(20));
        card.setStroke(dp(1), 0xFFE0E5F2);
        panel.setBackground(card);

        // title row (drag handle)
        LinearLayout head = new LinearLayout(this);
        head.setOrientation(LinearLayout.HORIZONTAL);
        head.setGravity(Gravity.CENTER_VERTICAL);
        TextView title = new TextView(this);
        title.setText("ChessVision");
        title.setTextSize(13);
        title.setTypeface(Typeface.DEFAULT_BOLD);
        title.setTextColor(MUTED);
        head.addView(title, new LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f));
        collapseBtn = new TextView(this);
        collapseBtn.setText("▾");
        collapseBtn.setTextSize(20);
        collapseBtn.setTextColor(MUTED);
        collapseBtn.setPadding(dp(12), 0, dp(12), 0);
        head.addView(collapseBtn);
        TextView close = new TextView(this);
        close.setText("✕");
        close.setTextSize(17);
        close.setTextColor(MUTED);
        close.setPadding(dp(10), 0, dp(2), 0);
        head.addView(close);
        panel.addView(head);

        // best move + evaluation
        LinearLayout row = new LinearLayout(this);
        row.setOrientation(LinearLayout.HORIZONTAL);
        row.setGravity(Gravity.CENTER_VERTICAL);
        bestView = new TextView(this);
        bestView.setText("—");
        bestView.setTextSize(24);
        bestView.setTypeface(Typeface.DEFAULT_BOLD);
        bestView.setTextColor(INK);
        row.addView(bestView, new LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f));
        evalView = new TextView(this);
        evalView.setTextSize(15);
        evalView.setTextColor(MUTED);
        row.addView(evalView);
        panel.addView(row);

        // details (status and buttons)
        details = new LinearLayout(this);
        details.setOrientation(LinearLayout.VERTICAL);
        statusView = new TextView(this);
        statusView.setTextSize(12);
        statusView.setTextColor(MUTED);
        statusView.setMaxLines(3);
        statusView.setPadding(0, dp(2), 0, dp(8));
        details.addView(statusView);

        LinearLayout btns = new LinearLayout(this);
        btns.setOrientation(LinearLayout.HORIZONTAL);
        runBtn = button("Старт", true);
        TextView reset = button("Сброс", false);
        TextView find = button("Найти доску", false);
        LinearLayout.LayoutParams b1 = new LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f);
        LinearLayout.LayoutParams b2 = new LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f);
        LinearLayout.LayoutParams b3 = new LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 1.3f);
        b2.leftMargin = dp(6);
        b3.leftMargin = dp(6);
        btns.addView(runBtn, b1);
        btns.addView(reset, b2);
        btns.addView(find, b3);
        details.addView(btns);
        panel.addView(details);

        runBtn.setOnClickListener(new View.OnClickListener() {
            @Override
            public void onClick(View v) {
                bg.post(new Runnable() {
                    @Override
                    public void run() {
                        session.toggle();
                    }
                });
            }
        });
        reset.setOnClickListener(new View.OnClickListener() {
            @Override
            public void onClick(View v) {
                bg.post(new Runnable() {
                    @Override
                    public void run() {
                        session.reset();
                    }
                });
            }
        });
        find.setOnClickListener(new View.OnClickListener() {
            @Override
            public void onClick(View v) {
                bg.post(new Runnable() {
                    @Override
                    public void run() {
                        session.recalibrate();
                    }
                });
            }
        });
        collapseBtn.setOnClickListener(new View.OnClickListener() {
            @Override
            public void onClick(View v) {
                boolean open = details.getVisibility() == View.VISIBLE;
                details.setVisibility(open ? View.GONE : View.VISIBLE);
                collapseBtn.setText(open ? "▸" : "▾");
            }
        });
        close.setOnClickListener(new View.OnClickListener() {
            @Override
            public void onClick(View v) {
                stopSelf();
            }
        });

        panelLp = new WindowManager.LayoutParams(Math.min(Math.round(screenW * 0.94f), dp(340)),
                WindowManager.LayoutParams.WRAP_CONTENT, overlayType(),
                WindowManager.LayoutParams.FLAG_NOT_FOCUSABLE | WindowManager.LayoutParams.FLAG_LAYOUT_NO_LIMITS,
                PixelFormat.TRANSLUCENT);
        panelLp.gravity = Gravity.TOP | Gravity.START;
        panelLp.x = (screenW - panelLp.width) / 2;
        panelLp.y = dp(40);

        head.setOnTouchListener(new View.OnTouchListener() {
            private float downX, downY;
            private int startX, startY;

            @Override
            public boolean onTouch(View v, MotionEvent e) {
                switch (e.getActionMasked()) {
                    case MotionEvent.ACTION_DOWN:
                        downX = e.getRawX();
                        downY = e.getRawY();
                        startX = panelLp.x;
                        startY = panelLp.y;
                        return true;
                    case MotionEvent.ACTION_MOVE:
                        panelLp.x = startX + Math.round(e.getRawX() - downX);
                        panelLp.y = startY + Math.round(e.getRawY() - downY);
                        try {
                            wm.updateViewLayout(panel, panelLp);
                        } catch (RuntimeException ignored) {
                            // the window was removed
                        }
                        return true;
                    default:
                        return true;
                }
            }
        });
    }

    // ---- rotation --------------------------------------------------------------------------

    @Override
    public void onConfigurationChanged(Configuration newConfig) {
        super.onConfigurationChanged(newConfig);
        if (session == null || stopped) {
            return;
        }
        main.postDelayed(new Runnable() {
            @Override
            public void run() {
                if (stopped) {
                    return;
                }
                readScreen();
                try {
                    wm.updateViewLayout(arrow, arrowParams());
                    panelLp.x = Math.max(0, Math.min(panelLp.x, screenW - panelLp.width));
                    panelLp.y = Math.max(0, Math.min(panelLp.y, screenH - dp(80)));
                    wm.updateViewLayout(panel, panelLp);
                } catch (RuntimeException ignored) {
                    // views are being torn down
                }
                bg.post(new Runnable() {
                    @Override
                    public void run() {
                        if (stopped) {
                            return;
                        }
                        ImageReader old = reader;
                        reader = newReader();
                        display.resize(frameW, frameH, densityDpi);
                        display.setSurface(reader.getSurface());
                        old.close();
                        session.recalibrate();
                    }
                });
            }
        }, 300);
    }

    // ---- shutdown --------------------------------------------------------------------------

    @Override
    public void onDestroy() {
        stopped = true;
        try {
            if (arrow != null) {
                wm.removeView(arrow);
            }
            if (panel != null) {
                wm.removeView(panel);
            }
        } catch (RuntimeException ignored) {
            // already removed
        }
        arrow = null;
        panel = null;
        final Session s = session;
        final HandlerThread t = bgThread;
        final VirtualDisplay vd = display;
        final ImageReader rd = reader;
        final MediaProjection mp = projection;
        session = null;
        new Thread(new Runnable() {
            @Override
            public void run() {
                try {
                    if (s != null) {
                        s.close();
                    }
                    if (vd != null) {
                        vd.release();
                    }
                    if (rd != null) {
                        rd.close();
                    }
                    if (mp != null) {
                        mp.stop();
                    }
                } catch (RuntimeException ignored) {
                    // best effort
                }
                if (t != null) {
                    t.quitSafely();
                }
            }
        }).start();
        super.onDestroy();
    }
}
