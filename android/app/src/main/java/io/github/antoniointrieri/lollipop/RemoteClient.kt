package io.github.antoniointrieri.lollipop

import android.content.Context
import android.util.Log
import org.json.JSONObject
import java.io.BufferedReader
import java.io.InputStreamReader
import java.net.HttpURLConnection
import java.net.URL
import java.net.URLEncoder
import java.util.concurrent.CopyOnWriteArraySet

// Talks to the lollipop desktop over the LAN. It keeps a Server-Sent Events connection to /events on a
// background thread, parses each state snapshot, and exposes the latest list of agents so the grid and
// the Live Update read the same data. Reconnects with exponential backoff; survives the 20s heartbeat.
// Platform APIs only (HttpURLConnection + org.json), no AndroidX, no external libraries.
object RemoteClient {
    private const val TAG = "RemoteClient"
    private const val PREFS = "pairing"
    private const val KEY_ENDPOINT = "endpoint"
    private const val KEY_TOKEN = "token"
    const val DEFAULT_PORT = 47600

    data class Agent(val key: String, val label: String, val state: String, val blink: Boolean)

    enum class Status { DISCONNECTED, CONNECTING, CONNECTED, ERROR }

    interface Listener {
        fun onAgents(agents: List<Agent>) {}
        fun onStatus(status: Status, error: String?) {}
    }

    @Volatile
    var agents: List<Agent> = emptyList()
        private set

    @Volatile
    var status: Status = Status.DISCONNECTED
        private set

    @Volatile
    var lastError: String? = null
        private set

    private val listeners = CopyOnWriteArraySet<Listener>()

    @Volatile private var host: String = ""
    @Volatile private var port: Int = DEFAULT_PORT
    @Volatile private var token: String = ""

    @Volatile private var running = false
    private var thread: Thread? = null
    @Volatile private var connection: HttpURLConnection? = null

    fun addListener(l: Listener) {
        listeners.add(l)
        // Hand the newcomer the current snapshot right away, so the UI never starts blank.
        l.onStatus(status, lastError)
        l.onAgents(agents)
    }

    fun removeListener(l: Listener) = listeners.remove(l)

    // --- configuration (SharedPreferences) ---

    fun save(ctx: Context, endpoint: String, tokenValue: String) {
        ctx.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit()
            .putString(KEY_ENDPOINT, endpoint.trim())
            .putString(KEY_TOKEN, tokenValue.trim())
            .apply()
    }

    fun endpoint(ctx: Context): String =
        ctx.getSharedPreferences(PREFS, Context.MODE_PRIVATE).getString(KEY_ENDPOINT, "").orEmpty()

    fun token(ctx: Context): String =
        ctx.getSharedPreferences(PREFS, Context.MODE_PRIVATE).getString(KEY_TOKEN, "").orEmpty()

    fun isConfigured(ctx: Context): Boolean = endpoint(ctx).isNotBlank() && token(ctx).isNotBlank()

    private fun loadConfig(ctx: Context) {
        val ep = endpoint(ctx)
        val colon = ep.lastIndexOf(':')
        if (colon in 1 until ep.length - 1) {
            host = ep.substring(0, colon).trim()
            port = ep.substring(colon + 1).trim().toIntOrNull() ?: DEFAULT_PORT
        } else {
            host = ep.trim()
            port = DEFAULT_PORT
        }
        token = token(ctx)
    }

    // --- lifecycle ---

    @Synchronized
    fun start(ctx: Context) {
        loadConfig(ctx)
        if (host.isBlank()) {
            lastError = "indirizzo non configurato"
            setStatus(Status.ERROR)
            return
        }
        if (running) return
        running = true
        thread = Thread({ loop() }, "lollipop-sse").apply { isDaemon = true; start() }
    }

    @Synchronized
    fun stop() {
        running = false
        connection?.disconnect()
        connection = null
        thread?.interrupt()
        thread = null
        agents = emptyList()
        setStatus(Status.DISCONNECTED)
        listeners.forEach { it.onAgents(agents) }
    }

    private fun loop() {
        var backoff = 1000L
        while (running) {
            try {
                setStatus(Status.CONNECTING)
                val conn = open("/events")
                connection = conn
                val code = conn.responseCode
                when (code) {
                    HttpURLConnection.HTTP_OK -> {
                        setStatus(Status.CONNECTED)
                        backoff = 1000L
                        readStream(conn)
                    }
                    HttpURLConnection.HTTP_UNAUTHORIZED -> {
                        lastError = "token non valido (401)"
                        setStatus(Status.ERROR)
                    }
                    else -> {
                        lastError = "HTTP $code"
                        setStatus(Status.ERROR)
                    }
                }
                conn.disconnect()
            } catch (e: Exception) {
                if (running) {
                    lastError = e.message ?: e.javaClass.simpleName
                    setStatus(Status.ERROR)
                    Log.w(TAG, "connection error: ${e.message}")
                }
            } finally {
                connection = null
            }
            if (!running) break
            setStatus(Status.CONNECTING)
            try {
                Thread.sleep(backoff)
            } catch (e: InterruptedException) {
                break
            }
            backoff = (backoff * 2).coerceAtMost(30_000L)
        }
        setStatus(Status.DISCONNECTED)
    }

    // Parse the SSE stream: `event: state` + `data: <json>`, dispatched on a blank line. Lines starting
    // with ':' are comments (the heartbeat), ignored. A null line or timeout ends the stream and triggers
    // a reconnect from loop().
    private fun readStream(conn: HttpURLConnection) {
        val reader = BufferedReader(InputStreamReader(conn.inputStream, Charsets.UTF_8))
        var event: String? = null
        val data = StringBuilder()
        while (running) {
            val line = reader.readLine() ?: break
            when {
                line.isEmpty() -> {
                    if (event == "state" && data.isNotEmpty()) handleData(data.toString())
                    event = null
                    data.setLength(0)
                }
                line.startsWith(":") -> { /* heartbeat / comment */ }
                line.startsWith("event:") -> event = line.substring(6).trim()
                line.startsWith("data:") -> {
                    if (data.isNotEmpty()) data.append('\n')
                    data.append(line.substring(5).trim())
                }
            }
        }
    }

    private fun handleData(json: String) {
        try {
            val parsed = parse(json)
            agents = parsed
            listeners.forEach { it.onAgents(parsed) }
        } catch (e: Exception) {
            Log.w(TAG, "bad snapshot: ${e.message}")
        }
    }

    private fun parse(json: String): List<Agent> {
        val items = JSONObject(json).getJSONArray("items")
        val out = ArrayList<Agent>(items.length())
        for (i in 0 until items.length()) {
            val it = items.getJSONObject(i)
            val key = it.getString("key")
            out.add(
                Agent(
                    key = key,
                    label = it.optString("label", key),
                    state = it.optString("state", ""),
                    blink = it.optBoolean("blink", false),
                )
            )
        }
        return out
    }

    private fun open(path: String): HttpURLConnection {
        val enc = URLEncoder.encode(token, "UTF-8")
        val url = URL("http://$host:$port$path?token=$enc")
        return (url.openConnection() as HttpURLConnection).apply {
            connectTimeout = 5000
            // Longer than the 20s server heartbeat, so a quiet-but-alive stream is not dropped.
            readTimeout = 35000
            setRequestProperty("Accept", "text/event-stream")
            setRequestProperty("Cache-Control", "no-cache")
        }
    }

    // POST /focus {"key":...} on a throwaway thread: brings that agent's window to the front on the PC.
    fun focus(ctx: Context, key: String) {
        if (host.isBlank()) loadConfig(ctx)
        Thread {
            try {
                val enc = URLEncoder.encode(token, "UTF-8")
                val url = URL("http://$host:$port/focus?token=$enc")
                val c = (url.openConnection() as HttpURLConnection).apply {
                    requestMethod = "POST"
                    connectTimeout = 5000
                    readTimeout = 5000
                    doOutput = true
                    setRequestProperty("Content-Type", "application/json")
                }
                val body = JSONObject().put("key", key).toString().toByteArray(Charsets.UTF_8)
                c.outputStream.use { it.write(body) }
                Log.d(TAG, "focus $key -> ${c.responseCode}")
                c.disconnect()
            } catch (e: Exception) {
                Log.w(TAG, "focus failed: ${e.message}")
            }
        }.apply { isDaemon = true }.start()
    }

    private fun setStatus(s: Status) {
        if (status == s && s != Status.ERROR) return
        status = s
        val err = lastError
        listeners.forEach { it.onStatus(s, err) }
    }
}
