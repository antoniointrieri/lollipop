plugins {
    id("com.android.application")
}

android {
    namespace = "io.github.antoniointrieri.lollipop"
    compileSdk = 37

    defaultConfig {
        applicationId = "io.github.antoniointrieri.lollipop"
        // Live Updates (promoted notifications, ProgressStyle) exist from Android 16.
        minSdk = 36
        targetSdk = 37
        versionCode = 1
        versionName = "0.1.0"
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
}

dependencies {
    // Google code scanner (Play services): reads the pairing QR code with its own camera UI, no CAMERA permission.
    implementation("com.google.android.gms:play-services-code-scanner:16.1.0")
}
