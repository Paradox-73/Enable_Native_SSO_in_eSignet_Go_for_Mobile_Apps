// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip

pluginManagement {
    repositories {
        google()
        mavenCentral()
        gradlePluginPortal()
    }
}

dependencyResolutionManagement {
    repositoriesMode.set(RepositoriesMode.FAIL_ON_PROJECT_REPOS)
    repositories {
        google()
        mavenCentral()
    }
}

rootProject.name = "esignet-native-sso-android"

include(":sso-contract")
include(":oidc-client")
include(":superapp")
include(":childapp")
