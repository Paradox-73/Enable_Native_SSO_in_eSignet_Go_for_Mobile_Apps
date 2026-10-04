// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip

// Small encrypted file store for tokens: AES-256-GCM with a key held in Android Keystore,
// ciphertext in noBackupFilesDir. Written for this project instead of Jetpack security-crypto,
// whose APIs are deprecated since 1.1.0-beta01. The file format and the cipher sit behind plain
// Java interfaces, so every rule is tested on the JVM with a software AES key.
plugins {
    alias(libs.plugins.android.library)
    alias(libs.plugins.kotlin.android)
}

android {
    namespace = "org.badmosip.nativesso.securestore"
    compileSdk = 34
    defaultConfig {
        minSdk = 28
    }
    buildFeatures {
        buildConfig = false
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    kotlinOptions { jvmTarget = "17" }
    testOptions {
        unitTests.all {
            it.testLogging {
                events("failed")
                exceptionFormat = org.gradle.api.tasks.testing.logging.TestExceptionFormat.FULL
            }
        }
    }
}

dependencies {
    testImplementation(libs.junit)
}
