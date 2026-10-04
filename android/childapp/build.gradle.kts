// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip

// Child App ("Health Card"): signs in silently with the Super App's device secret. Signed with the family certificate configured in the root build file.
plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.android)
    alias(libs.plugins.kotlin.compose)
}

val issuer: String = providers.gradleProperty("ps03.issuer").get()

android {
    namespace = "org.badmosip.nativesso.childapp"
    compileSdk = 34

    defaultConfig {
        applicationId = "org.badmosip.nativesso.childapp"
        minSdk = 28
        targetSdk = 34
        versionCode = 1
        versionName = "0.1.0"
        buildConfigField("String", "ISSUER", "\"$issuer\"")
    }

    buildTypes {
        release {
            isMinifyEnabled = false
        }
    }
    buildFeatures {
        compose = true
        buildConfig = true
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    kotlinOptions { jvmTarget = "17" }
    packaging {
        resources.excludes += setOf("META-INF/versions/9/OSGI-INF/MANIFEST.MF")
    }
}

dependencies {
    implementation(project(":sso-contract"))
    implementation(project(":oidc-client"))
    implementation(libs.androidx.core.ktx)
    implementation(libs.androidx.activity.compose)
    implementation(platform(libs.androidx.compose.bom))
    implementation(libs.androidx.compose.material3)
}
