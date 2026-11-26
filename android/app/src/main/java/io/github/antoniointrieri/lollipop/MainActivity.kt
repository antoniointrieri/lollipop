package io.github.antoniointrieri.lollipop

import android.Manifest
import android.app.Activity
import android.content.Intent
import android.content.pm.PackageManager
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
import android.widget.Toast
import com.google.mlkit.vision.barcode.common.Barcode
import com.google.mlkit.vision.codescanner.GmsBarcodeScannerOptions
import com.google.mlkit.vision.codescanner.GmsBarcodeScanning

// Pairing + control screen: scan the QR code shown by lollipop (or enter the desktop address and token),
// connect (which starts the foreground service and its SSE connection) or disconnect, and watch the live
// connection status.
class MainActivity : Activity(), RemoteClient.Listener {
    private lateinit var endpoint: EditText
    private lateinit var token: EditText
    private lateinit var connectButton: Button
    private lateinit var connStatus: TextView
    private var afterLocalNetwork: (() -> Unit)? = null // waits for the local network permission

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
            addView(button("Scansiona QR") { scanPairing() })
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
        // or, as if the QR code had been scanned: --es pair 'lollipop://pair?addr=host:port&token=T'
        intent.getStringExtra("endpoint")?.let { endpoint.setText(it) }
        intent.getStringExtra("token")?.let { token.setText(it) }
        intent.getStringExtra("pair")?.let { pair(it) }
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
            refreshSoon()
        } else {
            connect()
        }
    }

    // Saves the pairing and starts the service; a running one restarts so it connects with the new pairing.
    private fun connect() = withLocalNetwork {
        RemoteClient.save(this, endpoint.text.toString(), token.text.toString())
        val service = Intent(this, AgentService::class.java)
        if (AgentService.running) stopService(service)
        startForegroundService(service)
        refreshSoon()
    }

    // Reflect the new intent immediately; the real status follows via the listener.
    private fun refreshSoon() =
        connectButton.postDelayed({ render(RemoteClient.status, RemoteClient.lastError) }, 300)

    // Reads the QR code in lollipop's Settings → Phone with the Google code scanner: Play services shows the
    // camera and returns the decoded text, so the app needs no camera permission.
    private fun scanPairing() {
        val options = GmsBarcodeScannerOptions.Builder()
            .setBarcodeFormats(Barcode.FORMAT_QR_CODE)
            .build()
        GmsBarcodeScanning.getClient(this, options).startScan()
            .addOnSuccessListener { pair(it.rawValue) }
            .addOnFailureListener { toast("Scansione non riuscita: ${it.message}") }
    }

    // Fills in address and token from a pairing QR code and connects. The code lists every private address
    // of the PC (Wi-Fi, but also WSL or VPN adapters): the first one that answers wins, else the PC's best guess.
    private fun pair(raw: String?) {
        val (addrs, tok) = RemoteClient.parsePairing(raw) ?: run {
            toast("QR non valido: usa quello in Impostazioni → Telefono di lollipop")
            return
        }
        withLocalNetwork {
            connStatus.text = "Stato: cerco il PC..."
            Thread {
                val addr = addrs.firstOrNull { RemoteClient.reachable(it, tok) } ?: addrs.first()
                runOnUiThread {
                    endpoint.setText(addr)
                    token.setText(tok)
                    connect()
                }
            }.apply { isDaemon = true }.start()
        }
    }

    // From Android 17 an app reaches devices on the LAN, the PC included, only with the local network
    // permission: without it the connection times out. Runs action once it is granted.
    private fun withLocalNetwork(action: () -> Unit) {
        if (checkSelfPermission(Manifest.permission.ACCESS_LOCAL_NETWORK) == PackageManager.PERMISSION_GRANTED) {
            action()
            return
        }
        afterLocalNetwork = action
        requestPermissions(arrayOf(Manifest.permission.ACCESS_LOCAL_NETWORK), REQUEST_LOCAL_NETWORK)
    }

    override fun onRequestPermissionsResult(requestCode: Int, permissions: Array<String>, grantResults: IntArray) {
        if (requestCode != REQUEST_LOCAL_NETWORK) return
        val action = afterLocalNetwork
        afterLocalNetwork = null
        if (grantResults.firstOrNull() == PackageManager.PERMISSION_GRANTED) {
            action?.invoke()
        } else {
            toast("Senza il permesso per la rete locale l'app non raggiunge il PC")
        }
    }

    private fun toast(msg: String) = Toast.makeText(this, msg, Toast.LENGTH_LONG).show()

    private companion object {
        const val REQUEST_LOCAL_NETWORK = 2
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
