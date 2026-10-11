plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
    id("org.jetbrains.kotlin.plugin.compose")
}

// Version numbers come from the release workflow, which derives them from the
// git tag (-Popenpeats.versionName=1.2.3 -Popenpeats.versionCode=10203).
// Nothing here has to be edited by hand at tag time; the defaults are what a
// local debug build gets.
val appVersionName = (findProperty("openpeats.versionName") as String?) ?: "0.1.0"
val appVersionCode = ((findProperty("openpeats.versionCode") as String?) ?: "1").toInt()

// Release signing is optional on purpose. Without a keystore the build still
// produces an unsigned APK, so a fork with no secrets configured can run the
// exact same workflow and get an artefact out of it.
val keystorePath = findProperty("openpeats.keystore") as String?

android {
    namespace = "com.openpeats"
    compileSdk = 35

    defaultConfig {
        applicationId = "com.openpeats"
        minSdk = 26
        targetSdk = 35
        versionCode = appVersionCode
        versionName = appVersionName
    }

    signingConfigs {
        if (keystorePath != null) {
            create("release") {
                storeFile = file(keystorePath)
                storePassword = findProperty("openpeats.keystorePassword") as String?
                keyAlias = findProperty("openpeats.keyAlias") as String?
                keyPassword = findProperty("openpeats.keyPassword") as String?
            }
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = false
            signingConfig = if (keystorePath != null) {
                signingConfigs.getByName("release")
            } else {
                signingConfigs.getByName("debug")
            }
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    kotlinOptions {
        jvmTarget = "17"
    }

    buildFeatures {
        compose = true
    }

    sourceSets["main"].java.srcDirs("src/main/kotlin")

    packaging {
        resources.excludes += "/META-INF/{AL2.0,LGPL2.1}"
    }

    lint {
        // A string resource that exists in English but not in French is caught
        // by .github/scripts/check-android-strings.py, which reports every
        // missing name at once instead of failing on the first.
        disable += "MissingTranslation"
    }
}

dependencies {
    val composeBom = platform("androidx.compose:compose-bom:2024.12.01")
    implementation(composeBom)

    implementation("androidx.core:core-ktx:1.15.0")
    implementation("androidx.activity:activity-compose:1.9.3")
    implementation("androidx.lifecycle:lifecycle-runtime-ktx:2.8.7")
    implementation("androidx.lifecycle:lifecycle-viewmodel-compose:2.8.7")

    implementation("androidx.compose.ui:ui")
    implementation("androidx.compose.ui:ui-graphics")
    implementation("androidx.compose.material3:material3")
    implementation("androidx.compose.material:material-icons-extended")

    implementation("org.jetbrains.kotlinx:kotlinx-coroutines-android:1.9.0")

    testImplementation("junit:junit:4.13.2")
}
