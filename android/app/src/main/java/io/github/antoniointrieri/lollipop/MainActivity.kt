package io.github.antoniointrieri.lollipop

import android.Manifest
import android.app.Activity
import android.content.Intent
import android.os.Bundle
import android.provider.Settings
import android.text.InputType
import android.view.Gravity
import android.view.WindowInsets
import android.widget.Button
import android.widget.EditText
import android.widget.LinearLayout
import android.widget.ScrollView
import android.widget.TextView

// Pairing + control screen: enter the desktop address and token, connect (which starts the foreground
// service and its SSE connection) or disconnect, and watch the live connection status.
class MainActivity : Activity(), RemoteClient.Listener {
    private lateinit var endpoint: EditText
    private lateinit var token: EditText
    private lateinit var connectButton: Button
    private lateinit var connStatus: TextView

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val pad = dp(20)

        endpoint = EditText(this).apply {
            inputType = InputType.TYPE_TEXT_VARIATION_URI
            hint = "192.168.1.x:${RemoteClient.DEFAULT_PORT}"
            setText(RemoteClient.endpoint(this@MainActivity)
                .ifBlank { ":${RemoteClient.DEFAULT_PORT}" })
        }
        token = EditText(this).apply {
            inputType = InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS or InputType.TYPE_CLASS_TEXT
            hint = "token"
            setText(RemoteClient.token(this@MainActivity))
        }
        connStatus = TextView(this).apply { setTextSizeSp(14f); setPadding(0, dp(4), 0, dp(8)) }
        connectButton = button("Connetti") { toggleConnection() }

        val column = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            addView(TextView(this@MainActivity).apply {
                text = "lollipop"
                setTextSizeSp(24f)
                setPadding(0, 0, 0, dp(12))
            })

            addView(sectionLabel("Accoppiamento"))
            addView(fieldLabel("Indirizzo (host:porta)"))
            addView(endpoint)
            addView(fieldLabel("Token"))
            addView(token)
            addView(connStatus)
            addView(connectButton)

            addView(spacer())
            addView(sectionLabel("Altro"))
            addView(button("Permesso notifiche") {
                requestPermissions(arrayOf(Manifest.permission.POST_NOTIFICATIONS), 1)
            })
            addView(button("Apri griglia") {
                startActivity(Intent(this@MainActivity, GridActivity::class.java))
            })
            addView(button("Impostazioni Live Update") {
                startActivity(
                    Intent(Settings.ACTION_APP_NOTIFICATION_PROMOTION_SETTINGS)
                        .putExtra(Settings.EXTRA_APP_PACKAGE, packageName),
                )
            })
        }

        val scroll = ScrollView(this).apply { addView(column) }
        // Edge-to-edge is mandatory from Android 15: keep the content clear of the system bars.
        scroll.setOnApplyWindowInsetsListener { v, insets ->
            val bars = insets.getInsets(WindowInsets.Type.systemBars())
            v.setPadding(pad + bars.left, pad + bars.top, pad + bars.right, pad + bars.bottom)
            insets
        }
        setContentView(scroll)

        // Lets a test drive pairing without touching the keyboard:
        // adb shell am start -n <pkg>/.MainActivity --es endpoint host:port --es token T --ez connect true
        intent.getStringExtra("endpoint")?.let { endpoint.setText(it) }
        intent.getStringExtra("token")?.let { token.setText(it) }
        if (intent.getBooleanExtra("connect", false) && !AgentService.running) toggleConnection()
    }

    override fun onResume() {
        super.onResume()
        RemoteClient.addListener(this)
        render(RemoteClient.status, RemoteClient.lastError)
    }

    override fun onPause() {
        RemoteClient.removeListener(this)
        super.onPause()
    }

    private fun toggleConnection() {
        if (AgentService.running) {
            stopService(Intent(this, AgentService::class.java))
        } else {
            RemoteClient.save(this, endpoint.text.toString(), token.text.toString())
            startForegroundService(Intent(this, AgentService::class.java))
        }
        // Reflect the new intent immediately; the real status follows via the listener.
        connectButton.postDelayed({ render(RemoteClient.status, RemoteClient.lastError) }, 300)
    }

    // --- RemoteClient.Listener ---

    override fun onStatus(status: RemoteClient.Status, error: String?) {
        runOnUiThread { render(status, error) }
    }

    private fun render(status: RemoteClient.Status, error: String?) {
        val running = AgentService.running
        connectButton.text = if (running) "Disconnetti" else "Connetti"
        val label = when {
            !running -> "non connesso"
            status == RemoteClient.Status.CONNECTED -> "connesso"
            status == RemoteClient.Status.CONNECTING -> "in connessione..."
            status == RemoteClient.Status.ERROR -> "errore: ${error ?: "rete"}"
            else -> "non connesso"
        }
        val count = RemoteClient.agents.size
        connStatus.text = if (running && status == RemoteClient.Status.CONNECTED)
            "Stato: $label ($count agenti)" else "Stato: $label"
    }

    private fun sectionLabel(t: String) = TextView(this).apply {
        text = t
        setTextSizeSp(13f)
        setTypeface(typeface, android.graphics.Typeface.BOLD)
        setTextColor(0xFF888888.toInt())
        setPadding(0, dp(4), 0, dp(4))
    }

    private fun fieldLabel(t: String) = TextView(this).apply {
        text = t
        setTextSizeSp(13f)
        setPadding(0, dp(8), 0, dp(2))
    }

    private fun spacer() = TextView(this).apply { setPadding(0, dp(16), 0, 0) }

    private fun button(label: String, onClick: () -> Unit) = Button(this).apply {
        text = label
        gravity = Gravity.CENTER
        setOnClickListener { onClick() }
    }

    private fun dp(v: Int): Int = (v * resources.displayMetrics.density).toInt()

    private fun TextView.setTextSizeSp(sp: Float) =
        setTextSize(android.util.TypedValue.COMPLEX_UNIT_SP, sp)
}
