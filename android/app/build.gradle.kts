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

// Release signing is optional on purpose. Without a keystore the release build
// falls back to the debug key, so a fork with no secrets configured can run the
// exact same workflow and still get an APK that installs. That key is generated
// per machine — on CI, per run — so two such builds cannot update one another:
// published releases need the real keystore.
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
    sourceSets["test"].java.srcDirs("src/test/kotlin")

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

// The protocol vectors are shared with the Go implementation and live at the
// repository root, outside this Gradle build. Declared as a test input so that
// editing them reruns the tests instead of replaying a cached result.
val protocolVectors = rootProject.file("../testvectors/protocol.json")

tasks.withType<Test>().configureEach {
    inputs.file(protocolVectors)
    systemProperty("openpeats.vectors", protocolVectors.absolutePath)
}

dependencies {
    val composeBom = platform("androidx.compose:compose-bom:2024.12.01")
    implementation(composeBom)

    implementation("androidx.core:core-ktx:1.15.0")
    implementation("androidx.activity:activity-compose:1.9.3")
    implementation("androidx.lifecycle:lifecycle-runtime-ktx:2.8.7")
    implementation("androidx.lifecycle:lifecycle-viewmodel-compose:2.8.7")
    implementation("androidx.lifecycle:lifecycle-runtime-compose:2.8.7")

    implementation("androidx.compose.ui:ui")
    implementation("androidx.compose.ui:ui-graphics")
    implementation("androidx.compose.material3:material3")
    implementation("androidx.compose.material:material-icons-extended")

    implementation("org.jetbrains.kotlinx:kotlinx-coroutines-android:1.9.0")

    testImplementation("junit:junit:4.13.2")
    // Android's own org.json is a stub in local unit tests; this is the real one.
    testImplementation("org.json:json:20240303")
}
