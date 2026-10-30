package com.chessvision.app;

import android.content.Context;
import android.graphics.Bitmap;
import android.graphics.Canvas;
import android.graphics.Paint;
import android.graphics.RectF;
import android.view.View;

/** Full-screen, touch-through view that draws the arrow picture made by the Go core. */
final class ArrowView extends View {
    private Bitmap bitmap;
    private final RectF dst = new RectF();
    private final RectF draw = new RectF();
    private final Paint paint = new Paint(Paint.FILTER_BITMAP_FLAG);
    private final int[] loc = new int[2];

    ArrowView(Context context) {
        super(context);
    }

    void setArrow(Bitmap bmp, float left, float top, float right, float bottom) {
        bitmap = bmp;
        dst.set(left, top, right, bottom);
        invalidate();
    }

    void clearArrow() {
        bitmap = null;
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
        canvas.drawBitmap(b, null, draw, paint);
    }
}
