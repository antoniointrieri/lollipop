package io.github.antoniointrieri.lollipop

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.os.Build
import android.text.SpannableStringBuilder
import android.text.Spanned

// State model and notification builders for the real agents coming from the lollipop desktop.
// Mapping (fixed by the desktop contract): working -> yellow, done -> green, monitoring -> blue,
// anything else (waiting, blocked, ...) -> red, meaning "the agent is waiting for you".
object Probe {
    const val CHANNEL = "agents"
    const val ALERT_CHANNEL = "alerts"
    const val NOTIFICATION_ID = 1
    const val ALERT_ID = 2

    enum class State(val label: String, val semantic: Int, val color: Int) {
        WAITING("in attesa", Notification.SEMANTIC_STYLE_DANGER, 0xFFE53935.toInt()),
        DONE("finito", Notification.SEMANTIC_STYLE_SAFE, 0xFF43A047.toInt()),
        WORKING("al lavoro", Notification.SEMANTIC_STYLE_CAUTION, 0xFFF9C534.toInt()),
        MONITORING("in background", Notification.SEMANTIC_STYLE_INFO, 0xFF1E88E5.toInt());

        companion object {
            // Everything not explicitly working/done/monitoring is treated as "waiting for you" (red).
            fun of(raw: String): State = when (raw.trim().lowercase()) {
                "working" -> WORKING
                "done" -> DONE
                "monitoring" -> MONITORING
                else -> WAITING
            }
        }
    }

    fun ensureChannel(ctx: Context) {
        val nm = ctx.getSystemService(NotificationManager::class.java)
        val channel = NotificationChannel(CHANNEL, "Agenti", NotificationManager.IMPORTANCE_DEFAULT).apply {
            setSound(null, null)
            enableVibration(false)
            lockscreenVisibility = Notification.VISIBILITY_PUBLIC
        }
        // High-importance channel for the full-screen alert that wakes the screen on a WAITING agent.
        val alerts = NotificationChannel(ALERT_CHANNEL, "Avvisi", NotificationManager.IMPORTANCE_HIGH).apply {
            setSound(null, null)
            enableVibration(false)
            lockscreenVisibility = Notification.VISIBILITY_PUBLIC
        }
        nm.createNotificationChannel(channel)
        nm.createNotificationChannel(alerts)
    }

    // The ongoing Live Update: one colored segment per agent, a title summarizing the fleet, and the
    // agent names colored with semantic styles. `note` carries the connection state when there are no
    // agents to show (e.g. "in connessione...").
    fun build(ctx: Context, agents: List<RemoteClient.Agent>, note: String): Notification {
        val states = agents.map { State.of(it.state) }
        val waiting = states.count { it == State.WAITING }
        val working = states.count { it == State.WORKING }
        val title = when {
            agents.isEmpty() -> note.ifBlank { "nessun agente" }
            waiting > 0 -> "$waiting in attesa"
            working > 0 -> "$working al lavoro"
            else -> "${agents.size} agenti"
        }

        val text = SpannableStringBuilder()
        agents.forEachIndexed { i, agent ->
            if (i > 0) text.append("  ")
            val start = text.length
            text.append(agent.label)
            if (Build.VERSION.SDK_INT >= 37) {
                text.setSpan(
                    Notification.createSemanticStyleAnnotation(states[i].semantic),
                    start, text.length, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE,
                )
            }
        }

        val builder = Notification.Builder(ctx, CHANNEL)
            .setSmallIcon(R.drawable.ic_stat_lollipop)
            .setContentTitle(title)
            .setContentText(if (agents.isEmpty()) note else text)
            .setShortCriticalText(if (waiting > 0) "$waiting in attesa" else "${agents.size} agenti")
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .setRequestPromotedOngoing(true)
            .setVisibility(Notification.VISIBILITY_PUBLIC)
            .setContentIntent(openGrid(ctx))

        // ProgressStyle needs at least one segment; skip it when the fleet is empty.
        if (agents.isNotEmpty()) {
            val segments = states.map { state ->
                Notification.ProgressStyle.Segment(1).setColor(state.color).also {
                    if (Build.VERSION.SDK_INT >= 37) it.setSemanticStyle(state.semantic)
                }
            }
            builder.setStyle(
                Notification.ProgressStyle()
                    .setStyledByProgress(false)
                    .setProgressSegments(segments)
                    .setProgress(segments.size)
            )
        }
        return builder.build()
    }

    // One-shot full-screen alert: its full-screen intent turns the screen on and shows GridActivity over
    // the lock screen when an agent transitions to WAITING. Posted by AgentService, not ongoing.
    fun buildAlert(ctx: Context, waiting: List<String>): Notification {
        val title = if (waiting.size == 1) "${waiting[0]} in attesa" else "${waiting.size} agenti in attesa"
        return Notification.Builder(ctx, ALERT_CHANNEL)
            .setSmallIcon(R.drawable.ic_stat_lollipop)
            .setContentTitle(title)
            .setContentText(waiting.joinToString(", "))
            .setCategory(Notification.CATEGORY_CALL)
            .setFullScreenIntent(openGrid(ctx), true)
            .setContentIntent(openGrid(ctx))
            .setAutoCancel(true)
            .setVisibility(Notification.VISIBILITY_PUBLIC)
            .build()
    }

    // Opens the grid over the lock screen without unlocking the phone.
    private fun openGrid(ctx: Context): PendingIntent = PendingIntent.getActivity(
        ctx, 0,
        Intent(ctx, GridActivity::class.java)
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP),
        PendingIntent.FLAG_IMMUTABLE,
    )
}
