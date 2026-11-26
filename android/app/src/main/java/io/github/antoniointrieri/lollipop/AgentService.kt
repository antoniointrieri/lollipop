package io.github.antoniointrieri.lollipop

import android.app.NotificationManager
import android.app.Service
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.IBinder

// Foreground service that keeps the SSE connection to the lollipop desktop alive. On every snapshot it
// refreshes the Live Update, and when an agent transitions into WAITING (relative to the previous
// snapshot) it posts the full-screen alert that wakes the lock screen, unless the grid is already up.
class AgentService : Service(), RemoteClient.Listener {

    private var previous: Map<String, Probe.State> = emptyMap()
    private var seeded = false

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (!running) {
            Probe.ensureChannel(this)
            startForeground(
                Probe.NOTIFICATION_ID,
                Probe.build(this, RemoteClient.agents, "in connessione..."),
                ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE,
            )
            running = true
            RemoteClient.addListener(this)
            RemoteClient.start(this)
        }
        return START_STICKY
    }

    override fun onDestroy() {
        running = false
        RemoteClient.removeListener(this)
        RemoteClient.stop()
        stopForeground(STOP_FOREGROUND_REMOVE)
        super.onDestroy()
    }

    override fun onBind(intent: Intent?): IBinder? = null

    // --- RemoteClient.Listener ---

    override fun onAgents(agents: List<RemoteClient.Agent>) {
        val nm = getSystemService(NotificationManager::class.java)
        nm.notify(Probe.NOTIFICATION_ID, Probe.build(this, agents, note()))

        val now = agents.associate { it.key to Probe.State.of(it.state) }
        if (!seeded) {
            // First snapshot sets the baseline: don't wake the screen for agents that were already
            // waiting when we connected.
            previous = now
            seeded = true
            return
        }

        // Fire only on a genuine transition into WAITING, and not while the grid is already on screen.
        val newlyWaiting = agents.filter {
            Probe.State.of(it.state) == Probe.State.WAITING &&
                previous[it.key] != null && previous[it.key] != Probe.State.WAITING
        }
        if (newlyWaiting.isNotEmpty() && !GridActivity.visible) {
            nm.notify(Probe.ALERT_ID, Probe.buildAlert(this, newlyWaiting.map { it.label }))
        }
        previous = now
    }

    override fun onStatus(status: RemoteClient.Status, error: String?) {
        // Reflect connection trouble in the Live Update when there are no agents to show.
        if (RemoteClient.agents.isEmpty()) {
            val nm = getSystemService(NotificationManager::class.java)
            nm.notify(Probe.NOTIFICATION_ID, Probe.build(this, emptyList(), note()))
        }
    }

    private fun note(): String = when (RemoteClient.status) {
        RemoteClient.Status.CONNECTED -> "nessun agente"
        RemoteClient.Status.CONNECTING -> "in connessione..."
        RemoteClient.Status.ERROR -> "errore: ${RemoteClient.lastError ?: "rete"}"
        RemoteClient.Status.DISCONNECTED -> "disconnesso"
    }

    companion object {
        @Volatile
        var running = false
    }
}
