// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip

// Super App ("Hub"): the app the user signs in to with eSignet; shares that sign-in with the family. Signed with the family certificate configured in the root build file.
plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.android)
    alias(libs.plugins.kotlin.compose)
}

val issuer: String = providers.gradleProperty("ps03.issuer").get()

// A release build blocks cleartext, so an http issuer would compile and then fail at runtime.
gradle.taskGraph.whenReady {
    if (!issuer.startsWith("https://") && allTasks.any { it.project == project && it.name.matches(Regex("(assemble|bundle|package)Release")) }) {
        throw GradleException("Release builds need an https issuer: pass -Pps03.issuer=https://... (got $issuer)")
    }
}

android {
    namespace = "org.badmosip.nativesso.superapp"
    compileSdk = 34

    defaultConfig {
        applicationId = "org.badmosip.nativesso.superapp"
        minSdk = 28
        targetSdk = 34
        versionCode = 1
        versionName = "0.1.0"
        buildConfigField("String", "ISSUER", "\"$issuer\"")
        // client_id = this prefix + 12 hex characters of the client key's thumbprint: every install
        // has its own Keystore key and so its own registration (android/scripts/register-client.sh).
        buildConfigField("String", "CLIENT_ID_PREFIX", "\"ps03-hub-\"")
        buildConfigField("String", "REDIRECT_URI", "\"org.badmosip.nativesso.superapp://oauth/callback\"")
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
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
        // Robolectric reads the merged manifest (the service and its permission) from the resources.
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
    testImplementation(libs.okhttp.mockwebserver)
    testImplementation(libs.robolectric)
    testImplementation(libs.androidx.test.core)
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
