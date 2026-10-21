// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip

// Child App ("Health Card"): signs in silently with the Super App's device secret. Signed with the family certificate configured in the root build file.
plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.android)
    alias(libs.plugins.kotlin.compose)
}

val issuer: String = providers.gradleProperty("ps03.issuer").get()

// Test builds of an app that is NOT part of the family (android/scripts/build-apks.sh):
//   -Pps03.intruder=true      Health Card's package name, signed with the Android debug certificate,
//                             without declaring the family permission: Android must refuse its bind.
//   -Pps03.intruder=takeover  the same, but declaring the permission, for the takeover step of
//                             android/scripts/run-family-tests.sh: Hub's per-call check must refuse it.
val intruder: String = providers.gradleProperty("ps03.intruder").getOrElse("false")
require(intruder in setOf("false", "true", "takeover")) { "ps03.intruder must be false, true or takeover" }

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
        // client_id = this prefix + 12 hex characters of the client key's thumbprint (as in Hub):
        // every install registers its own key (android/scripts/register-client.sh childapp).
        buildConfigField("String", "CLIENT_ID_PREFIX", "\"ps03-card-\"")
        buildConfigField("String", "REDIRECT_URI", "\"org.badmosip.nativesso.childapp://oauth/callback\"")
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
    }

    if (intruder != "false") {
        // Replaces the family signing config the root build file set.
        buildTypes.getByName("debug").signingConfig = signingConfigs.getByName("debug")
    }
    if (intruder == "true") {
        sourceSets.getByName("debug").manifest.srcFile("src/intruder/AndroidManifest.xml")
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
    testOptions {
        unitTests.isIncludeAndroidResources = true
        unitTests.all {
            it.testLogging {
                events("failed")
                exceptionFormat = org.gradle.api.tasks.testing.logging.TestExceptionFormat.FULL
            }
        }
    }
    // AccessibilityScan, the accessibility check both apps' UI tests run on every screen they show.
    sourceSets.getByName("androidTest").java.srcDir("../ui-test-support")
    packaging {
        resources.excludes += setOf("META-INF/versions/9/OSGI-INF/MANIFEST.MF")
    }
}

dependencies {
    implementation(project(":sso-contract"))
    implementation(project(":oidc-client"))
    implementation(project(":secure-store"))
    implementation(libs.androidx.browser)
    implementation(libs.androidx.core.ktx)
    implementation(libs.androidx.activity.compose)
    implementation(platform(libs.androidx.compose.bom))
    implementation(libs.androidx.compose.material3)
    implementation(libs.androidx.compose.ui)

    testImplementation(libs.junit)
    testImplementation(libs.robolectric)
    testImplementation(libs.androidx.test.core)
    testImplementation(libs.okhttp.mockwebserver)
    testImplementation(platform(libs.androidx.compose.bom))
    testImplementation(libs.androidx.compose.ui.test.junit4)
    debugImplementation(platform(libs.androidx.compose.bom))
    debugImplementation(libs.androidx.compose.ui.test.manifest)

    androidTestImplementation(libs.androidx.test.runner)
    androidTestImplementation(libs.androidx.test.ext.junit)
    androidTestImplementation(libs.androidx.test.uiautomator)
    androidTestImplementation(libs.accessibility.test.framework)
    androidTestImplementation(libs.guava)
}
