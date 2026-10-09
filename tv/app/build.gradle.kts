plugins { id("com.android.application"); id("org.jetbrains.kotlin.android") }

android {
    namespace = "com.eufywall.tv"
    compileSdk = 36
    defaultConfig {
        applicationId = "com.eufywall.tv"
        minSdk = 24
        targetSdk = 35
        versionCode = 2
        versionName = "0.2"
    }
    compileOptions { sourceCompatibility = JavaVersion.VERSION_17; targetCompatibility = JavaVersion.VERSION_17 }
    kotlinOptions { jvmTarget = "17" }
}

dependencies {
    implementation("androidx.media3:media3-exoplayer:1.11.1")
    implementation("com.github.alexeyvasilyev:rtsp-client-android:5.6.3")
    implementation("com.squareup.okhttp3:okhttp:4.12.0")
}
