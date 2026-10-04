plugins { id("com.android.application"); id("org.jetbrains.kotlin.android") }

android {
    namespace = "com.eufywall.tv"
    compileSdk = 36
    defaultConfig {
        applicationId = "com.eufywall.tv"
        minSdk = 23
        targetSdk = 35
        versionCode = 1
        versionName = "0.1"
    }
    compileOptions { sourceCompatibility = JavaVersion.VERSION_17; targetCompatibility = JavaVersion.VERSION_17 }
    kotlinOptions { jvmTarget = "17" }
}

dependencies {
    implementation("androidx.media3:media3-exoplayer:1.11.1")
    implementation("androidx.media3:media3-exoplayer-rtsp:1.11.1")
    implementation("androidx.media3:media3-ui:1.11.1")
    implementation("com.squareup.okhttp3:okhttp:4.12.0")
}
