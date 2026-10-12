plugins { id("com.android.library"); id("org.jetbrains.kotlin.android") }
android {
    namespace = "com.alexvas.rtsp"
    compileSdk = 36
    defaultConfig { minSdk = 24 }
    compileOptions { sourceCompatibility = JavaVersion.VERSION_17; targetCompatibility = JavaVersion.VERSION_17 }
    kotlinOptions { jvmTarget = "17" }
}
dependencies {
    implementation("androidx.annotation:annotation:1.9.1")
    implementation("androidx.media3:media3-exoplayer:1.9.0")
    implementation("androidx.camera:camera-core:1.5.2")
    implementation("org.jcodec:jcodec:0.2.5")
    testImplementation("junit:junit:4.13.2")
}
