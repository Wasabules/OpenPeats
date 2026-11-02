package com.openpeats.ui

import android.annotation.SuppressLint
import android.app.LocaleManager
import android.content.Context
import android.os.Build
import android.os.LocaleList
import androidx.annotation.RequiresApi
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import com.openpeats.R
import org.xmlpull.v1.XmlPullParser
import java.util.Locale

/**
 * The app's language: the system's unless picked here. Android keeps the choice
 * and applies it to the whole app, widgets and notifications included.
 *
 * Android 13 is where it can do that. Earlier versions just follow the system.
 */
@Composable
fun LanguageCard() {
    if (Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU) return
    val context = LocalContext.current
    val languages = remember { context.shippedLanguages() }
    val manager = context.getSystemService(LocaleManager::class.java)
    val current = manager.applicationLocales.takeIf { !it.isEmpty }?.get(0)?.language

    // The index into the list stands for the tag; -1 for the system's language.
    val options = buildList {
        add(-1 to stringResource(R.string.language_system))
        languages.forEachIndexed { i, tag -> add(i to endonym(tag)) }
    }
    Section(stringResource(R.string.section_language)) {
        SegmentedRow(
            options = options,
            selected = languages.indexOf(current),
            enabled = true,
            onSelect = { i ->
                manager.applicationLocales =
                    if (i < 0) LocaleList.getEmptyLocaleList() else LocaleList.forLanguageTags(languages[i])
            },
        )
    }
}

/** A language's name in itself: it has to be readable to whoever needs to switch to it. */
private fun endonym(tag: String): String {
    val locale = Locale.forLanguageTag(tag)
    return locale.getDisplayLanguage(locale).replaceFirstChar { it.titlecase(locale) }
}

/**
 * The languages this build ships, from the list the build generates out of the
 * values-* directories: a new translation shows up here with no code to edit.
 */
@RequiresApi(Build.VERSION_CODES.TIRAMISU)
@SuppressLint("DiscouragedApi")
private fun Context.shippedLanguages(): List<String> = buildList {
    resources.getXml(R.xml._generated_res_locale_config).use { xml ->
        while (xml.next() != XmlPullParser.END_DOCUMENT) {
            if (xml.eventType == XmlPullParser.START_TAG && xml.name == "locale") {
                xml.getAttributeValue(ANDROID_NS, "name")?.let { add(it) }
            }
        }
    }
}

private const val ANDROID_NS = "http://schemas.android.com/apk/res/android"
