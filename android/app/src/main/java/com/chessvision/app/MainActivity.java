package com.chessvision.app;

import android.Manifest;
import android.app.Activity;
import android.content.Intent;
import android.graphics.Color;
import android.graphics.Typeface;
import android.graphics.drawable.GradientDrawable;
import android.media.projection.MediaProjectionManager;
import android.net.Uri;
import android.os.Build;
import android.os.Bundle;
import android.provider.Settings;
import android.view.Gravity;
import android.view.View;
import android.widget.Button;
import android.widget.LinearLayout;
import android.widget.ScrollView;
import android.widget.TextView;
import android.widget.Toast;

public final class MainActivity extends Activity {
    private static final int REQ_OVERLAY = 1;
    private static final int REQ_CAPTURE = 2;
    private static final int REQ_NOTIFY = 3;

    private static final int INK = 0xFF1B2340;
    private static final int MUTED = 0xFF5B6585;
    private static final int ACCENT = 0xFF5B5BF0;

    private TextView hint;

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        buildUi();
    }

    @Override
    protected void onResume() {
        super.onResume();
        refreshHint();
    }

    private int dp(int v) {
        return Math.round(v * getResources().getDisplayMetrics().density);
    }

    private void buildUi() {
        LinearLayout col = new LinearLayout(this);
        col.setOrientation(LinearLayout.VERTICAL);
        col.setPadding(dp(24), dp(48), dp(24), dp(32));
        col.setGravity(Gravity.CENTER_HORIZONTAL);

        TextView title = new TextView(this);
        title.setText("ChessVision");
        title.setTextSize(32);
        title.setTypeface(Typeface.DEFAULT_BOLD);
        title.setTextColor(INK);
        col.addView(title);

        TextView sub = new TextView(this);
        sub.setText("Подсказки лучшего хода прямо поверх шахматной доски");
        sub.setTextSize(15);
        sub.setTextColor(MUTED);
        sub.setGravity(Gravity.CENTER);
        sub.setPadding(0, dp(8), 0, dp(28));
        col.addView(sub);

        TextView steps = new TextView(this);
        steps.setText("1. Разрешите показ поверх других приложений.\n"
                + "2. Разрешите запись экрана (на Android 14 выберите «Весь экран» или окно шахматного приложения).\n"
                + "3. Откройте шахматы и нажмите «Старт» в плавающей панели. Доска, фигуры и ваша сторона определяются сами.");
        steps.setTextSize(15);
        steps.setTextColor(INK);
        steps.setLineSpacing(0, 1.25f);
        GradientDrawable card = new GradientDrawable();
        card.setColor(Color.WHITE);
        card.setCornerRadius(dp(18));
        card.setStroke(dp(1), 0xFFE3E7F2);
        steps.setBackground(card);
        steps.setPadding(dp(18), dp(16), dp(18), dp(16));
        col.addView(steps, new LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT, LinearLayout.LayoutParams.WRAP_CONTENT));

        hint = new TextView(this);
        hint.setTextSize(13);
        hint.setTextColor(MUTED);
        hint.setPadding(0, dp(16), 0, dp(16));
        hint.setGravity(Gravity.CENTER);
        col.addView(hint);

        Button go = new Button(this);
        go.setText("Запустить помощника");
        go.setAllCaps(false);
        go.setTextSize(17);
        go.setTextColor(Color.WHITE);
        GradientDrawable bg = new GradientDrawable();
        bg.setColor(ACCENT);
        bg.setCornerRadius(dp(16));
        go.setBackground(bg);
        go.setOnClickListener(new View.OnClickListener() {
            @Override
            public void onClick(View v) {
                start();
            }
        });
        LinearLayout.LayoutParams lp = new LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT, dp(56));
        col.addView(go, lp);

        TextView fair = new TextView(this);
        fair.setText("Используйте для анализа партий и тренировки. В онлайн-играх с живыми соперниками "
                + "помощь движка запрещена правилами платформ.");
        fair.setTextSize(12);
        fair.setTextColor(MUTED);
        fair.setGravity(Gravity.CENTER);
        fair.setPadding(0, dp(20), 0, 0);
        col.addView(fair);

        ScrollView sv = new ScrollView(this);
        sv.setBackgroundColor(0xFFF4F6FB);
        sv.setFillViewport(true);
        sv.addView(col);
        setContentView(sv);
    }

    private void refreshHint() {
        if (hint == null) {
            return;
        }
        if (canOverlay()) {
            hint.setText("Разрешение «Поверх других приложений» выдано");
        } else {
            hint.setText("Нужно разрешение «Поверх других приложений» — запрос появится при запуске");
        }
    }

    private boolean canOverlay() {
        return Build.VERSION.SDK_INT < 23 || Settings.canDrawOverlays(this);
    }

    private void start() {
        if (!canOverlay()) {
            Intent i = new Intent(Settings.ACTION_MANAGE_OVERLAY_PERMISSION,
                    Uri.parse("package:" + getPackageName()));
            startActivityForResult(i, REQ_OVERLAY);
            return;
        }
        if (Build.VERSION.SDK_INT >= 33
                && checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS)
                != android.content.pm.PackageManager.PERMISSION_GRANTED) {
            requestPermissions(new String[]{Manifest.permission.POST_NOTIFICATIONS}, REQ_NOTIFY);
            return;
        }
        requestCapture();
    }

    private void requestCapture() {
        MediaProjectionManager mpm = (MediaProjectionManager) getSystemService(MEDIA_PROJECTION_SERVICE);
        startActivityForResult(mpm.createScreenCaptureIntent(), REQ_CAPTURE);
    }

    @Override
    public void onRequestPermissionsResult(int requestCode, String[] permissions, int[] grantResults) {
        super.onRequestPermissionsResult(requestCode, permissions, grantResults);
        if (requestCode == REQ_NOTIFY) {
            // the notification is optional for the service to run, so continue either way
            requestCapture();
        }
    }

    @Override
    protected void onActivityResult(int requestCode, int resultCode, Intent data) {
        super.onActivityResult(requestCode, resultCode, data);
        if (requestCode == REQ_OVERLAY) {
            refreshHint();
            if (canOverlay()) {
                start();
            } else {
                Toast.makeText(this, "Без этого разрешения стрелку не показать", Toast.LENGTH_LONG).show();
            }
        } else if (requestCode == REQ_CAPTURE) {
            if (resultCode != RESULT_OK || data == null) {
                Toast.makeText(this, "Запись экрана не разрешена", Toast.LENGTH_LONG).show();
                return;
            }
            Intent svc = new Intent(this, CaptureService.class);
            svc.putExtra(CaptureService.EXTRA_CODE, resultCode);
            svc.putExtra(CaptureService.EXTRA_DATA, data);
            if (Build.VERSION.SDK_INT >= 26) {
                startForegroundService(svc);
            } else {
                startService(svc);
            }
            moveTaskToBack(true);
        }
    }
}
