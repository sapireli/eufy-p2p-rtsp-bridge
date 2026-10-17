plugins { id("com.android.application"); id("org.jetbrains.kotlin.android") }

android {
    namespace = "com.eufywall.tv"
    compileSdk = 36
    buildFeatures { buildConfig = true }
    defaultConfig {
        applicationId = "com.eufywall.viewer"
        minSdk = 24
        targetSdk = 35
        versionCode = 13
        versionName = "0.6.4"
    }
    compileOptions { sourceCompatibility = JavaVersion.VERSION_17; targetCompatibility = JavaVersion.VERSION_17 }
    kotlinOptions { jvmTarget = "17" }
}

dependencies {
    testImplementation("junit:junit:4.13.2")
    testImplementation("org.json:json:20240303")
    implementation("androidx.media3:media3-exoplayer:1.11.1")
    implementation(project(":rtsp-client"))
    implementation("com.squareup.okhttp3:okhttp:4.12.0")
}
