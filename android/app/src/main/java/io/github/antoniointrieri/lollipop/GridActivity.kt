package io.github.antoniointrieri.lollipop

import android.app.Activity
import android.app.NotificationManager
import android.graphics.Color
import android.graphics.Typeface
import android.graphics.drawable.GradientDrawable
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.util.TypedValue
import android.view.Gravity
import android.view.View
import android.view.WindowManager
import android.widget.LinearLayout
import android.widget.TextView

// Full-screen grid shown over the lock screen: one rounded tile per agent, Stream Deck style. Woken by
// AgentService when an agent needs attention. Dims the backlight, auto-dismisses, and a tap on the
// background closes it. A tap on a tile asks the desktop to bring that agent's window to the front.
class GridActivity : Activity() {
    private val handler = Handler(Looper.getMainLooper())
    private val dismiss = Runnable { finish() }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        // Appear over the lock screen and turn the screen on, without unlocking the phone.
        setShowWhenLocked(true)
        setTurnScreenOn(true)

        // Keep the screen on while visible, but dimmed, to limit battery drain on an otherwise black screen.
        window.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
        window.attributes = window.attributes.apply { screenBrightness = 0.12f }

        // Clear the one-shot full-screen alert that woke us, if still posted.
        getSystemService(NotificationManager::class.java).cancel(Probe.ALERT_ID)

        setContentView(buildView(RemoteClient.agents))

        // Auto-dismiss after the timeout, then the screen can turn back off.
        handler.postDelayed(dismiss, TIMEOUT_MS)
    }

    private fun buildView(agents: List<RemoteClient.Agent>): View {
        val root = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            gravity = Gravity.CENTER
            setBackgroundColor(Color.BLACK)
            setPadding(dp(16), dp(24), dp(16), dp(24))
            setOnClickListener { finish() }
        }

        if (agents.isEmpty()) {
            root.addView(TextView(this).apply {
                text = "nessun agente"
                setTextColor(0xFF9E9E9E.toInt())
                setTextSizeSp(16f)
                gravity = Gravity.CENTER
            })
            return root
        }

        val gap = dp(10)
        // Adaptive columns: ceil(sqrt(n)) gives a balanced square-ish grid (3 cols for 5-9 agents).
        val columns = Math.ceil(Math.sqrt(agents.size.toDouble())).toInt().coerceAtLeast(1)
        val available = resources.displayMetrics.widthPixels - dp(32) - gap * (columns - 1)
        val cell = (available / columns).coerceAtMost(dp(160))

        var i = 0
        while (i < agents.size) {
            val row = LinearLayout(this).apply {
                orientation = LinearLayout.HORIZONTAL
                gravity = Gravity.CENTER
            }
            var c = 0
            while (c < columns && i < agents.size) {
                row.addView(tile(agents[i], cell, gap))
                i++
                c++
            }
            root.addView(row)
        }
        return root
    }

    private fun tile(agent: RemoteClient.Agent, size: Int, gap: Int): View {
        val state = Probe.State.of(agent.state)
        val waiting = state == Probe.State.WAITING
        val onColor = if (state == Probe.State.WORKING) Color.BLACK else Color.WHITE

        val bg = GradientDrawable().apply {
            shape = GradientDrawable.RECTANGLE
            cornerRadius = dp(18).toFloat()
            setColor(state.color)
            // Waiting tiles get a bright, thick ring so the eye jumps to them; the rest get a subtle
            // darker edge for depth. A finished-but-unseen agent (blink) is ringed in its own color.
            when {
                waiting -> setStroke(dp(4), Color.WHITE)
                agent.blink -> setStroke(dp(3), lighten(state.color))
                else -> setStroke(dp(1), darken(state.color, 0.6f))
            }
        }

        val name = TextView(this).apply {
            text = agent.label
            setTextColor(onColor)
            setTextSizeSp(17f)
            setTypeface(Typeface.DEFAULT_BOLD)
            gravity = Gravity.CENTER
            maxLines = 2
            ellipsize = android.text.TextUtils.TruncateAt.END
        }

        val statusLine = TextView(this).apply {
            // A filled dot marks the agents that are waiting for you.
            text = if (waiting) "● ${state.label}" else state.label
            setTextColor(withAlpha(onColor, if (waiting) 0xFF else 0xB3))
            setTextSizeSp(12f)
            setTypeface(Typeface.DEFAULT_BOLD)
            gravity = Gravity.CENTER
            maxLines = 1
            ellipsize = android.text.TextUtils.TruncateAt.END
            setPadding(0, dp(4), 0, 0)
        }

        return LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            gravity = Gravity.CENTER
            background = bg
            setPadding(dp(10), dp(10), dp(10), dp(10))
            layoutParams = LinearLayout.LayoutParams(size, size).apply {
                setMargins(gap / 2, gap / 2, gap / 2, gap / 2)
            }
            addView(name)
            addView(statusLine)
            setOnClickListener {
                RemoteClient.focus(this@GridActivity, agent.key)
                finish()
            }
        }
    }

    override fun onStart() {
        super.onStart()
        visible = true
    }

    override fun onStop() {
        visible = false
        super.onStop()
    }

    override fun onDestroy() {
        handler.removeCallbacks(dismiss)
        super.onDestroy()
    }

    private fun dp(v: Int): Int = (v * resources.displayMetrics.density).toInt()

    private fun TextView.setTextSizeSp(sp: Float) =
        setTextSize(TypedValue.COMPLEX_UNIT_SP, sp)

    companion object {
        const val TIMEOUT_MS = 10_000L

        // Read by AgentService to avoid relaunching while the grid is already on screen.
        @Volatile
        var visible = false

        private fun darken(c: Int, f: Float): Int =
            Color.rgb((Color.red(c) * f).toInt(), (Color.green(c) * f).toInt(), (Color.blue(c) * f).toInt())

        private fun lighten(c: Int): Int {
            fun up(ch: Int) = ch + ((255 - ch) * 0.5f).toInt()
            return Color.rgb(up(Color.red(c)), up(Color.green(c)), up(Color.blue(c)))
        }

        private fun withAlpha(c: Int, a: Int): Int = (c and 0x00FFFFFF) or (a shl 24)
    }
}
