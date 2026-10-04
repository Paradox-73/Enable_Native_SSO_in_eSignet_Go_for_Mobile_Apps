// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip

// OpenID Connect client for the two apps: discovery, PAR, PKCE, DPoP, private_key_jwt, the
// authorization-code and Native SSO token-exchange grants, userinfo and id_token checks.
// Plain Kotlin on the JVM with no Android dependency, so every rule runs in fast JVM tests
// against MockWebServer. Key storage stays in the apps, behind the JwsSigner interface.
import org.jetbrains.kotlin.gradle.dsl.JvmTarget

plugins {
    alias(libs.plugins.kotlin.jvm)
    `java-library`
}

java {
    sourceCompatibility = JavaVersion.VERSION_17
    targetCompatibility = JavaVersion.VERSION_17
}

kotlin {
    explicitApi()
    compilerOptions { jvmTarget.set(JvmTarget.JVM_17) }
}

dependencies {
    api(libs.okhttp)
    api(libs.nimbus.jose.jwt)

    testImplementation(libs.junit)
    testImplementation(libs.okhttp.mockwebserver)
}

tasks.test {
    testLogging {
        events("failed")
        exceptionFormat = org.gradle.api.tasks.testing.logging.TestExceptionFormat.FULL
    }
}
