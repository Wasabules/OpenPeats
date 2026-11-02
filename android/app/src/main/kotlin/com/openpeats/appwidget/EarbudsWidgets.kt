package com.openpeats.appwidget

import android.content.Context
import android.os.Build
import android.text.format.DateFormat
import androidx.annotation.DrawableRes
import androidx.annotation.StringRes
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.DpSize
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.glance.ColorFilter
import androidx.glance.GlanceId
import androidx.glance.GlanceModifier
import androidx.glance.GlanceTheme
import androidx.glance.Image
import androidx.glance.ImageProvider
import androidx.glance.LocalContext
import androidx.glance.LocalSize
import androidx.glance.action.Action
import androidx.glance.action.clickable
import androidx.glance.appwidget.GlanceAppWidget
import androidx.glance.appwidget.GlanceAppWidgetReceiver
import androidx.glance.appwidget.LinearProgressIndicator
import androidx.glance.appwidget.SizeMode
import androidx.glance.action.actionStartActivity
import androidx.glance.appwidget.action.actionStartService
import androidx.glance.appwidget.appWidgetBackground
import androidx.glance.appwidget.cornerRadius
import androidx.glance.appwidget.provideContent
import androidx.glance.appwidget.updateAll
import androidx.glance.background
import androidx.glance.color.ColorProvider
import androidx.glance.layout.Alignment
import androidx.glance.layout.Box
import androidx.glance.layout.Column
import androidx.glance.layout.Row
import androidx.glance.layout.Spacer
import androidx.glance.layout.fillMaxHeight
import androidx.glance.layout.fillMaxSize
import androidx.glance.layout.fillMaxWidth
import androidx.glance.layout.height
import androidx.glance.layout.padding
import androidx.glance.layout.size
import androidx.glance.layout.width
import androidx.glance.text.FontWeight
import androidx.glance.text.Text
import androidx.glance.text.TextAlign
import androidx.glance.text.TextStyle
import androidx.glance.unit.ColorProvider
import com.openpeats.MainActivity
import com.openpeats.R
import com.openpeats.earbuds
import com.openpeats.hasBluetoothPermission
import com.openpeats.protocol.AncMode
import com.openpeats.service.EarbudsService
import com.openpeats.session.EarbudsCommand
import com.openpeats.session.EarbudsView
import com.openpeats.session.KnownEarbuds
import com.openpeats.session.LinkStatus
import com.openpeats.ui.ancLevelLabel
import com.openpeats.ui.transparencyLabel

// The home screen widgets. All draw from the session's view, which holds the
// last state read when the link is down, and act through the service, which a
// widget tap is allowed to start from a cold process.

/** Base of every widget: the same data, drawn differently. */
abstract class EarbudsWidget : GlanceAppWidget() {
    override suspend fun provideGlance(context: Context, id: GlanceId) {
        provideContent {
            val view by context.earbuds.view.collectAsState()
            GlanceTheme { Draw(view) }
        }
    }

    @Composable
    protected abstract fun Draw(view: EarbudsView)
}

private val WIDGETS: List<GlanceAppWidget> by lazy {
    listOf(
        ModesWidget(),
        QuickSwitchWidget(),
        BatteryWidget(),
        BatteryMiniWidget(),
        DashboardWidget(),
        LevelsWidget(),
        TogglesWidget(),
    )
}

suspend fun updateAllWidgets(context: Context) = WIDGETS.forEach { it.updateAll(context) }

// ------------------------------------------------------------ building blocks

@Composable
private fun openApp(): Action = actionStartActivity<MainActivity>()

/**
 * What tapping a control does. Without known earbuds or the permission, the
 * app is the only place that can do anything about it.
 */
@Composable
private fun act(view: EarbudsView, command: EarbudsCommand): Action {
    val context = LocalContext.current
    return if (view.known != null && context.hasBluetoothPermission()) {
        actionStartService(EarbudsService.intentFor(context, command), isForegroundService = true)
    } else {
        openApp()
    }
}

/** The widget's own background, rounded like the launcher wants it. */
@Composable
private fun Frame(
    modifier: GlanceModifier = GlanceModifier,
    padding: Int = 8,
    content: @Composable () -> Unit,
) {
    val base = GlanceModifier.fillMaxSize().appWidgetBackground()
    val background = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
        base.background(GlanceTheme.colors.widgetBackground)
            .cornerRadius(android.R.dimen.system_app_widget_background_radius)
    } else {
        // cornerRadius does nothing below Android 12.
        base.background(
            ImageProvider(R.drawable.widget_frame),
            colorFilter = ColorFilter.tint(GlanceTheme.colors.widgetBackground),
        )
    }
    Box(background.padding(padding.dp).then(modifier), contentAlignment = Alignment.Center) {
        content()
    }
}

/** A tappable rounded surface, lit when [active]. */
@Composable
private fun Chip(
    action: Action,
    active: Boolean,
    modifier: GlanceModifier = GlanceModifier,
    content: @Composable () -> Unit,
) {
    val color = if (active) GlanceTheme.colors.primary else GlanceTheme.colors.secondaryContainer
    Box(
        modifier
            .background(ImageProvider(R.drawable.widget_chip), colorFilter = ColorFilter.tint(color))
            .clickable(action),
        contentAlignment = Alignment.Center,
    ) {
        content()
    }
}

@Composable
private fun chipContentColor(active: Boolean): ColorProvider =
    if (active) GlanceTheme.colors.onPrimary else GlanceTheme.colors.onSecondaryContainer

@Composable
private fun Label(
    text: String,
    color: ColorProvider = GlanceTheme.colors.onSurface,
    size: Int = 12,
    bold: Boolean = false,
    modifier: GlanceModifier = GlanceModifier,
    center: Boolean = false,
) {
    Text(
        text,
        modifier = modifier,
        maxLines = 1,
        style = TextStyle(
            color = color,
            fontSize = size.sp,
            fontWeight = if (bold) FontWeight.Bold else FontWeight.Normal,
            textAlign = if (center) TextAlign.Center else TextAlign.Start,
        ),
    )
}

@Composable
private fun Icon(@DrawableRes id: Int, color: ColorProvider, size: Int) {
    Image(
        ImageProvider(id),
        contentDescription = null,
        modifier = GlanceModifier.size(size.dp),
        colorFilter = ColorFilter.tint(color),
    )
}

@Composable
private fun str(@StringRes id: Int, vararg args: Any): String =
    LocalContext.current.getString(id, *args)

/** The link's state in a few words; the time of the last read when it is down. */
@Composable
private fun status(view: EarbudsView): String {
    val context = LocalContext.current
    val known = view.known ?: return str(R.string.status_no_earbuds)
    return when (view.link) {
        LinkStatus.CONNECTED -> str(R.string.status_connected)
        LinkStatus.CONNECTING -> str(R.string.status_connecting)
        else -> if (known.readAtMs > 0) {
            str(R.string.status_last_read, DateFormat.getTimeFormat(context).format(known.readAtMs))
        } else {
            str(R.string.status_disconnected)
        }
    }
}

@DrawableRes
private fun modeIcon(mode: Int?): Int = when (mode?.toByte()) {
    AncMode.ON -> R.drawable.ic_mode_anc
    AncMode.TRANSPARENCY -> R.drawable.ic_mode_transparency
    else -> R.drawable.ic_mode_off
}

private val MODES = listOf(
    AncMode.OFF.toInt() to R.string.anc_off,
    AncMode.ON.toInt() to R.string.anc_on,
    AncMode.TRANSPARENCY.toInt() to R.string.anc_transparency,
)

/** The modes in a row: icons alone when narrow, then with short then full names. */
@Composable
private fun ModeRow(view: EarbudsView, modifier: GlanceModifier = GlanceModifier) {
    val size = LocalSize.current
    val stacked = size.height >= 72.dp
    Row(modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        MODES.forEachIndexed { i, (mode, label) ->
            if (i > 0) Spacer(GlanceModifier.width(6.dp))
            val active = view.shownMode == mode && view.known != null
            val color = chipContentColor(active)
            val name = when {
                size.width < 200.dp -> null
                // Beside its icon a name needs more room than under it.
                mode == AncMode.TRANSPARENCY.toInt() && size.width < if (stacked) 300.dp else 380.dp ->
                    str(R.string.anc_transparency_short)
                else -> str(label)
            }
            Chip(
                act(view, EarbudsCommand.AncModeTo(mode)),
                active,
                GlanceModifier.defaultWeight().fillMaxHeight(),
            ) {
                if (stacked) {
                    Column(horizontalAlignment = Alignment.CenterHorizontally) {
                        Icon(modeIcon(mode), color, 22)
                        if (name != null) Label(name, color)
                    }
                } else {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Icon(modeIcon(mode), color, 18)
                        if (name != null) {
                            Spacer(GlanceModifier.width(4.dp))
                            Label(name, color)
                        }
                    }
                }
            }
        }
    }
}

private fun levelColor(level: Int): Color = when {
    level <= 20 -> Color(0xFFE05E5E)
    level <= 35 -> Color(0xFFE0A33E)
    else -> Color(0xFF3FBF7F)
}

private data class Source(val key: String, @StringRes val label: Int, val level: Int)

private fun sources(k: KnownEarbuds): List<Source> = listOf(
    Source("batteryLeft", R.string.battery_left, k.batteryLeft),
    Source("batteryRight", R.string.battery_right, k.batteryRight),
    Source("batteryCase", R.string.battery_case, k.batteryCase),
).filter { it.key !in k.unsupported }

/** A message standing in for controls that have nothing to show yet. */
@Composable
private fun Hint(text: String) {
    Label(text, GlanceTheme.colors.onSurfaceVariant, center = true)
}

// ------------------------------------------------------------- the widgets

/** Off, noise cancelling and transparency, one tap each. */
class ModesWidget : EarbudsWidget() {
    override val sizeMode = SizeMode.Responsive(RESPONSIVE)

    @Composable
    override fun Draw(view: EarbudsView) {
        Frame(padding = 6) {
            if (view.known != null && "ancMode" in view.known.unsupported) {
                Hint(str(R.string.widget_unsupported))
            } else {
                ModeRow(view, GlanceModifier.fillMaxHeight())
            }
        }
    }
}

/** One button going through the modes, showing the current one. */
class QuickSwitchWidget : EarbudsWidget() {
    @Composable
    override fun Draw(view: EarbudsView) {
        val mode = view.shownMode
        val active = view.link == LinkStatus.CONNECTED && mode != null && mode != AncMode.OFF.toInt()
        val color = chipContentColor(active)
        Frame(padding = 4) {
            Chip(act(view, EarbudsCommand.CycleAncMode), active, GlanceModifier.fillMaxSize()) {
                Column(horizontalAlignment = Alignment.CenterHorizontally) {
                    Icon(modeIcon(mode), color, 26)
                    Label(
                        if (mode == AncMode.TRANSPARENCY.toInt()) str(R.string.anc_transparency_short)
                        else str(EarbudsService.modeLabel(mode ?: AncMode.OFF.toInt())),
                        color,
                        size = 11,
                    )
                }
            }
        }
    }
}

/** Left, right and case, with gauges when there is room. */
class BatteryWidget : EarbudsWidget() {
    override val sizeMode = SizeMode.Responsive(RESPONSIVE)

    @Composable
    override fun Draw(view: EarbudsView) {
        val size = LocalSize.current
        val known = view.known
        Frame(GlanceModifier.clickable(openApp())) {
            if (known == null) {
                Hint(status(view))
                return@Frame
            }
            Column(GlanceModifier.fillMaxWidth(), horizontalAlignment = Alignment.CenterHorizontally) {
                Row(GlanceModifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                    sources(known).forEachIndexed { i, s ->
                        if (i > 0) Spacer(GlanceModifier.width(8.dp))
                        Column(
                            GlanceModifier.defaultWeight(),
                            horizontalAlignment = Alignment.CenterHorizontally,
                        ) {
                            Label(str(s.label), GlanceTheme.colors.onSurfaceVariant, size = 11)
                            Label(str(R.string.battery_percent, s.level), bold = true, size = 16)
                            if (size.height >= 72.dp) {
                                Spacer(GlanceModifier.height(3.dp))
                                LinearProgressIndicator(
                                    s.level / 100f,
                                    GlanceModifier.fillMaxWidth(),
                                    color = ColorProvider(levelColor(s.level), levelColor(s.level)),
                                    backgroundColor = GlanceTheme.colors.surfaceVariant,
                                )
                            }
                        }
                    }
                }
                if (size.height >= 100.dp) {
                    Spacer(GlanceModifier.height(4.dp))
                    Label(status(view), GlanceTheme.colors.onSurfaceVariant, size = 11)
                }
            }
        }
    }
}

/** The earbud closest to empty, which is the one that decides. */
class BatteryMiniWidget : EarbudsWidget() {
    @Composable
    override fun Draw(view: EarbudsView) {
        val known = view.known
        val lowest = known?.let { k ->
            sources(k).filter { it.key != "batteryCase" }.minOfOrNull { it.level }
        }
        Frame(GlanceModifier.clickable(openApp()), padding = 4) {
            Column(horizontalAlignment = Alignment.CenterHorizontally) {
                Icon(
                    R.drawable.ic_battery,
                    if (lowest != null) ColorProvider(levelColor(lowest), levelColor(lowest))
                    else GlanceTheme.colors.onSurfaceVariant,
                    22,
                )
                Label(
                    if (lowest != null) str(R.string.battery_percent, lowest) else "—",
                    bold = true,
                    size = 15,
                )
            }
        }
    }
}

/** The name, the link, the batteries and the modes; sub-modes when there is room. */
class DashboardWidget : EarbudsWidget() {
    override val sizeMode = SizeMode.Responsive(RESPONSIVE_TALL)

    @Composable
    override fun Draw(view: EarbudsView) {
        val size = LocalSize.current
        val known = view.known
        Frame {
            Column(GlanceModifier.fillMaxSize()) {
                Row(
                    GlanceModifier.fillMaxWidth().clickable(openApp()),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Label(
                        known?.name ?: str(R.string.app_name),
                        bold = true,
                        size = 14,
                        modifier = GlanceModifier.defaultWeight(),
                    )
                    Label(status(view), GlanceTheme.colors.onSurfaceVariant, size = 11)
                }
                if (known != null) {
                    Spacer(GlanceModifier.height(2.dp))
                    Row(GlanceModifier.fillMaxWidth().clickable(openApp())) {
                        sources(known).forEachIndexed { i, s ->
                            if (i > 0) Spacer(GlanceModifier.width(10.dp))
                            Label(str(s.label) + " ", GlanceTheme.colors.onSurfaceVariant)
                            Label(str(R.string.battery_percent, s.level), bold = true)
                        }
                    }
                }
                Spacer(GlanceModifier.height(6.dp))
                val tall = size.height >= 170.dp && known != null
                if (known == null || "ancMode" !in known.unsupported) {
                    ModeRow(view, GlanceModifier.defaultWeight())
                }
                if (tall) {
                    Spacer(GlanceModifier.height(6.dp))
                    LevelRows(view, GlanceModifier.defaultWeight())
                }
            }
        }
    }
}

/** The sub-modes of whichever mode is on, which only ever applies one at a time. */
class LevelsWidget : EarbudsWidget() {
    override val sizeMode = SizeMode.Responsive(RESPONSIVE)

    @Composable
    override fun Draw(view: EarbudsView) {
        Frame(padding = 6) { LevelRows(view, GlanceModifier.fillMaxSize()) }
    }
}

@Composable
private fun LevelRows(view: EarbudsView, modifier: GlanceModifier) {
    val known = view.known
    val options: List<Triple<Int, String, EarbudsCommand>> = when {
        known == null -> emptyList()
        view.shownMode == AncMode.ON.toInt() && "ancLevel" !in known.unsupported ->
            known.ancLevels.map { Triple(it, str(ancLevelLabel(it)), EarbudsCommand.AncLevelTo(it)) }
        view.shownMode == AncMode.TRANSPARENCY.toInt() && "transparencyLevel" !in known.unsupported ->
            known.transparencyLevels.map {
                Triple(it, str(transparencyLabel(it)), EarbudsCommand.TransparencyLevelTo(it))
            }
        else -> emptyList()
    }
    if (known == null || options.isEmpty()) {
        Box(modifier, contentAlignment = Alignment.Center) {
            Hint(if (known == null) status(view) else str(R.string.widget_levels_inactive))
        }
        return
    }
    val selected =
        if (view.shownMode == AncMode.ON.toInt()) known.ancLevel else known.transparencyLevel
    val perRow = if (LocalSize.current.width < 250.dp) 2 else options.size
    Column(modifier) {
        options.chunked(perRow).forEachIndexed { r, row ->
            if (r > 0) Spacer(GlanceModifier.height(6.dp))
            Row(GlanceModifier.fillMaxWidth().defaultWeight()) {
                row.forEachIndexed { i, (code, label, command) ->
                    if (i > 0) Spacer(GlanceModifier.width(6.dp))
                    val active = code == selected
                    Chip(
                        act(view, command),
                        active,
                        GlanceModifier.defaultWeight().fillMaxHeight(),
                    ) {
                        Label(label, chipContentColor(active), center = true)
                    }
                }
            }
        }
    }
}

/** Game mode and the touch lock, for the settings worth flipping often. */
class TogglesWidget : EarbudsWidget() {
    override val sizeMode = SizeMode.Responsive(RESPONSIVE)

    @Composable
    override fun Draw(view: EarbudsView) {
        val known = view.known
        val toggles = buildList {
            if (known == null || "gameMode" !in known.unsupported) {
                add(Triple(R.string.toggle_game_mode, known?.gameMode == true, EarbudsCommand.ToggleGameMode))
            }
            if (known == null || "touchDisabled" !in known.unsupported) {
                add(Triple(R.string.widget_touch_lock, known?.touchDisabled == true, EarbudsCommand.ToggleTouchLock))
            }
        }
        Frame(padding = 6) {
            if (toggles.isEmpty()) {
                Hint(str(R.string.widget_unsupported))
                return@Frame
            }
            Row(GlanceModifier.fillMaxSize()) {
                toggles.forEachIndexed { i, (label, on, command) ->
                    if (i > 0) Spacer(GlanceModifier.width(6.dp))
                    Chip(act(view, command), on, GlanceModifier.defaultWeight().fillMaxHeight()) {
                        Label(str(label), chipContentColor(on), center = true)
                    }
                }
            }
        }
    }
}

private val RESPONSIVE = setOf(
    DpSize(110.dp, 40.dp), DpSize(200.dp, 40.dp), DpSize(300.dp, 40.dp), DpSize(380.dp, 40.dp),
    DpSize(110.dp, 72.dp), DpSize(200.dp, 72.dp), DpSize(300.dp, 72.dp),
    DpSize(110.dp, 100.dp), DpSize(200.dp, 100.dp), DpSize(300.dp, 100.dp),
)

private val RESPONSIVE_TALL = setOf(
    DpSize(200.dp, 100.dp), DpSize(300.dp, 100.dp),
    DpSize(200.dp, 170.dp), DpSize(300.dp, 170.dp),
)

class ModesWidgetReceiver : GlanceAppWidgetReceiver() {
    override val glanceAppWidget: GlanceAppWidget = ModesWidget()
}

class QuickSwitchWidgetReceiver : GlanceAppWidgetReceiver() {
    override val glanceAppWidget: GlanceAppWidget = QuickSwitchWidget()
}

class BatteryWidgetReceiver : GlanceAppWidgetReceiver() {
    override val glanceAppWidget: GlanceAppWidget = BatteryWidget()
}

class BatteryMiniWidgetReceiver : GlanceAppWidgetReceiver() {
    override val glanceAppWidget: GlanceAppWidget = BatteryMiniWidget()
}

class DashboardWidgetReceiver : GlanceAppWidgetReceiver() {
    override val glanceAppWidget: GlanceAppWidget = DashboardWidget()
}

class LevelsWidgetReceiver : GlanceAppWidgetReceiver() {
    override val glanceAppWidget: GlanceAppWidget = LevelsWidget()
}

class TogglesWidgetReceiver : GlanceAppWidgetReceiver() {
    override val glanceAppWidget: GlanceAppWidget = TogglesWidget()
}
