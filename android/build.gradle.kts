// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip

import com.android.build.api.dsl.ApplicationExtension
import java.security.SecureRandom
import java.util.Base64

plugins {
    alias(libs.plugins.android.application) apply false
    alias(libs.plugins.android.library) apply false
    alias(libs.plugins.kotlin.android) apply false
    alias(libs.plugins.kotlin.jvm) apply false
    alias(libs.plugins.kotlin.compose) apply false
}

// ---------------------------------------------------------------------------------------------
// One signing certificate for the whole app family.
//
// Android grants a permission declared with protectionLevel="signature" only to apps signed with
// the same certificate as the app that declared it. The Super App's single sign-on service is
// protected that way, so the Super App and the Child App must carry the same certificate, in
// debug builds too. The test keystore is created with keytool on the first build, outside git,
// in $PS03_RUN_DIR/keys/ps03-family-test.jks, together with a random password file next to it.
// Both are test-only material: never commit them, never use them for a store release.
//
// Overrides (Gradle property, then environment variable):
//   ps03.familyKeystore / PS03_FAMILY_KEYSTORE            keystore path
//   ps03.familyKeystorePassword / PS03_FAMILY_KEYSTORE_PASSWORD
//   PS03_RUN_DIR                                          run folder (default: <repo>/run, as scripts/lib.sh)
// ---------------------------------------------------------------------------------------------
val repoDir: File = rootDir.parentFile
val runDir: File = System.getenv("PS03_RUN_DIR")?.takeIf { it.isNotBlank() }?.let(::file)
    ?: repoDir.resolve("run")

val familyKeystore: File = providers.gradleProperty("ps03.familyKeystore")
    .orElse(providers.environmentVariable("PS03_FAMILY_KEYSTORE"))
    .map { file(it) }
    .getOrElse(runDir.resolve("keys/ps03-family-test.jks"))
val familyPasswordFile = File(familyKeystore.path + ".password")
val familyKeyAlias = "family"

/** Returns the keystore password, creating the keystore (and its password file) when missing. */
fun familyKeystorePassword(): String {
    val explicit = providers.gradleProperty("ps03.familyKeystorePassword")
        .orElse(providers.environmentVariable("PS03_FAMILY_KEYSTORE_PASSWORD"))
        .orNull
    if (familyKeystore.isFile) {
        return explicit ?: familyPasswordFile.takeIf { it.isFile }?.readText()?.trim()
            ?: throw GradleException(
                "Family keystore ${familyKeystore.path} exists but its password is unknown: " +
                    "create ${familyPasswordFile.name} next to it or set ps03.familyKeystorePassword.",
            )
    }
    val password = explicit ?: ByteArray(18).also { SecureRandom().nextBytes(it) }
        .let { Base64.getUrlEncoder().withoutPadding().encodeToString(it) }
    familyKeystore.parentFile.mkdirs()
    val keytool = File(System.getProperty("java.home"), "bin/keytool").path
    val process = ProcessBuilder(
        keytool, "-genkeypair", "-noprompt",
        "-keystore", familyKeystore.path, "-storetype", "PKCS12",
        "-storepass", password, "-keypass", password, "-alias", familyKeyAlias,
        "-keyalg", "EC", "-groupname", "secp256r1", "-sigalg", "SHA256withECDSA",
        "-validity", "3650", "-dname", "CN=PS03 App Family (test only)",
    ).redirectErrorStream(true).start()
    val output = process.inputStream.bufferedReader().readText()
    if (process.waitFor() != 0) throw GradleException("keytool failed: $output")
    if (explicit == null) familyPasswordFile.writeText(password + "\n")
    logger.lifecycle("Created the test-only family keystore ${familyKeystore.path}")
    return password
}

// Every application module gets the "family" signing config for debug and release builds.
subprojects {
    plugins.withId("com.android.application") {
        extensions.configure<ApplicationExtension> {
            val password = familyKeystorePassword()
            val family = signingConfigs.create("family") {
                storeFile = familyKeystore
                storePassword = password
                keyAlias = familyKeyAlias
                keyPassword = password
            }
            buildTypes.getByName("debug").signingConfig = family
            buildTypes.getByName("release").signingConfig = family
        }
    }
}
