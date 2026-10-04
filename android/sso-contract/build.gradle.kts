// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip

// The binder interface between the Super App and the apps of the same family. Both sides compile
// against this one module, so the interface, the parcelable and the constants cannot drift apart.
plugins {
    alias(libs.plugins.android.library)
    alias(libs.plugins.kotlin.android)
}

android {
    namespace = "org.badmosip.nativesso.contract"
    compileSdk = 34
    defaultConfig {
        minSdk = 28
        consumerProguardFiles("consumer-rules.pro")
    }
    buildFeatures {
        aidl = true
        buildConfig = false
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    kotlinOptions { jvmTarget = "17" }
}
