package com.chessvision.app;

import android.content.Context;
import android.graphics.Bitmap;
import android.graphics.Canvas;
import android.graphics.Paint;
import android.graphics.RectF;
import android.view.View;
import android.view.animation.DecelerateInterpolator;

/** Full-screen, touch-through view that draws the arrow picture made by the Go core. */
final class ArrowView extends View {
    private Bitmap bitmap;
    private final RectF dst = new RectF();
    private final RectF draw = new RectF();
    private final Paint paint = new Paint(Paint.FILTER_BITMAP_FLAG);
    private final int[] loc = new int[2];
    private float fade = 1f;
    private long shownKey;

    ArrowView(Context context) {
        super(context);
    }

    /** Shows the picture; a new picture (different key) fades in, the same one is left alone. */
    void setArrow(Bitmap bmp, float left, float top, float right, float bottom, long key) {
        boolean same = bitmap != null && key == shownKey;
        bitmap = bmp;
        shownKey = key;
        dst.set(left, top, right, bottom);
        if (!same) {
            fade = 0f;
            animate().cancel();
            android.animation.ValueAnimator a = android.animation.ValueAnimator.ofFloat(0f, 1f);
            a.setDuration(240);
            a.setInterpolator(new DecelerateInterpolator());
            a.addUpdateListener(new android.animation.ValueAnimator.AnimatorUpdateListener() {
                @Override
                public void onAnimationUpdate(android.animation.ValueAnimator va) {
                    fade = (Float) va.getAnimatedValue();
                    invalidate();
                }
            });
            a.start();
        }
        invalidate();
    }

    void clearArrow() {
        bitmap = null;
        shownKey = 0;
        invalidate();
    }

    @Override
    protected void onDraw(Canvas canvas) {
        Bitmap b = bitmap;
        if (b == null) {
            return;
        }
        // the window may be offset from the screen origin (status bar, cutout)
        getLocationOnScreen(loc);
        draw.set(dst.left - loc[0], dst.top - loc[1], dst.right - loc[0], dst.bottom - loc[1]);
        paint.setAlpha(Math.round(255 * fade));
        canvas.drawBitmap(b, null, draw, paint);
    }
}
