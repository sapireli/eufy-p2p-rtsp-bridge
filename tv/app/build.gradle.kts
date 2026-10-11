plugins { id("com.android.application"); id("org.jetbrains.kotlin.android") }

android {
    namespace = "com.eufywall.tv"
    compileSdk = 36
    defaultConfig {
        applicationId = "com.eufywall.viewer"
        minSdk = 24
        targetSdk = 35
        versionCode = 7
        versionName = "0.5.2"
    }
    compileOptions { sourceCompatibility = JavaVersion.VERSION_17; targetCompatibility = JavaVersion.VERSION_17 }
    kotlinOptions { jvmTarget = "17" }
}

dependencies {
    testImplementation("junit:junit:4.13.2")
    implementation("androidx.media3:media3-exoplayer:1.11.1")
    implementation("com.github.alexeyvasilyev:rtsp-client-android:5.6.3")
    implementation("com.squareup.okhttp3:okhttp:4.12.0")
}
