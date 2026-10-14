// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.superapp.sso

import android.app.Application
import android.content.ComponentName
import android.content.Intent
import android.content.pm.ApplicationInfo
import android.content.pm.PackageInfo
import android.content.pm.PackageManager
import android.content.pm.PermissionInfo
import android.content.pm.Signature
import android.content.pm.SigningInfo
import androidx.test.core.app.ApplicationProvider
import org.badmosip.nativesso.contract.FamilySsoContract
import org.badmosip.nativesso.contract.IFamilySso
import org.badmosip.nativesso.contract.SigningCertificates
import org.badmosip.nativesso.oidc.DpopProofFactory
import org.badmosip.nativesso.oidc.JwkThumbprint
import org.badmosip.nativesso.oidc.EcKeyJwsSigner
import org.badmosip.nativesso.superapp.auth.MemoryBlobStore
import org.badmosip.nativesso.superapp.auth.TokenBundle
import org.badmosip.nativesso.superapp.auth.verifiedJws
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Robolectric
import org.robolectric.RobolectricTestRunner
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config
import org.robolectric.shadow.api.Shadow
import org.robolectric.shadows.ShadowBinder
import org.robolectric.shadows.ShadowSigningInfo
import java.time.Instant

/**
 * The bound service as Android sees it: the merged manifest (exported, guarded by the signature
 * permission), the AIDL stub, `Binder.getCallingUid()` and the caller's real signing certificates
 * read through `PackageManager`. Callers are installed in Robolectric's package manager with their
 * own uid and certificate; a caller signed with another certificate is refused with
 * a `SecurityException` carrying `ERROR_CALLER_NOT_ALLOWED`.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], application = FamilySsoServiceTest.TestApp::class)
class FamilySsoServiceTest {
    /** Stands in for HubApp: same gate, but an in-memory store and a software DPoP key (no Keystore in Robolectric). */
    class TestApp : Application(), FamilySsoHost {
        var session: TokenBundle? = null
        val dpopSigner = EcKeyJwsSigner.generate()
        internal val store = MemoryBlobStore()
        override val familySsoGate: FamilySsoGate by lazy {
            FamilySsoGate(
                callers = AndroidCallerLookup(packageManager),
                familyCertificates = { SigningCertificates.sha256Of(packageManager, packageName) },
                session = { session },
                proofs = DpopProofFactory(dpopSigner),
                accessLog = FamilyAccessLog(store),
            )
        }
    }

    private val app: TestApp = ApplicationProvider.getApplicationContext()
    private val pm: PackageManager = app.packageManager
    private val familyCert = "family certificate (test)".toByteArray()
    private val debugCert = "android debug certificate (test)".toByteArray()
    private val issuer = "http://localhost:38088"
    private val tokenEndpoint = "$issuer/v1/esignet/oauth/v2/token"

    private val childUid = 10_101
    private val intruderUid = 10_202
    private val ownerlessUid = 10_303

    private lateinit var service: IFamilySso

    @Before
    fun setUp() {
        // Hub itself keeps the package parsed from its manifest (service, permission); only its certificate is set.
        shadowOf(pm).getInternalMutablePackageInfo(app.packageName).signingInfo = signingInfoOf(familyCert)
        install(FamilySsoContract.CHILD_APP_PACKAGE, childUid, familyCert)
        app.session = TokenBundle(
            issuer = issuer,
            clientId = "ps03-hub-0123456789ab",
            subject = "psut-1",
            sessionId = "sid-1",
            accessToken = "hub-access-token-value",
            tokenType = "DPoP",
            accessTokenExpiresAt = null,
            idToken = "hub-id-token-value",
            idTokenExpiresAt = Instant.now().plusSeconds(120),
            refreshToken = null,
            deviceSecret = "device-secret-value",
            scope = "openid profile device_sso",
            dpopKeyThumbprint = JwkThumbprint.compute(app.dpopSigner.publicJwk),
            displayName = null,
            signedInAt = Instant.now(),
            tokenEndpoint = tokenEndpoint,
            userinfoEndpoint = "$issuer/v1/esignet/oidc/userinfo",
        )
        val bind = Intent(FamilySsoContract.ACTION_BIND)
        val created = Robolectric.buildService(FamilySsoService::class.java, bind).create().get()
        service = IFamilySso.Stub.asInterface(assertNotNullAndGet(created.onBind(bind)))
    }

    @After
    fun tearDown() {
        ShadowBinder.reset()
    }

    private fun signingInfoOf(certificate: ByteArray): SigningInfo = SigningInfo().also {
        Shadow.extract<ShadowSigningInfo>(it).setSignatures(arrayOf(Signature(certificate)))
    }

    private fun install(packageName: String, uid: Int, certificate: ByteArray) {
        val signingInfo = signingInfoOf(certificate)
        val info = PackageInfo().apply {
            this.packageName = packageName
            applicationInfo = ApplicationInfo().apply {
                this.packageName = packageName
                this.uid = uid
            }
            this.signingInfo = signingInfo
        }
        shadowOf(pm).installPackage(info)
        shadowOf(pm).setPackagesForUid(uid, packageName)
    }

    private fun <T> assertNotNullAndGet(value: T?): T {
        assertNotNull(value)
        return value!!
    }

    private fun refusedWith(code: Int, call: () -> Unit) {
        val e = assertThrows(RuntimeException::class.java) { call() }
        assertEquals(e.toString(), code, FamilySsoContract.errorCodeOf(e))
        if (code == FamilySsoContract.ERROR_CALLER_NOT_ALLOWED) assertTrue(e is SecurityException)
    }

    @Test
    fun `manifest exports the service behind the signature permission`() {
        val info = pm.getServiceInfo(ComponentName(app, FamilySsoService::class.java), 0)
        assertEquals(FamilySsoContract.SERVICE_CLASS, info.name)
        assertEquals(FamilySsoContract.PERMISSION, info.permission)
        assertTrue(info.exported)
        val permission = pm.getPermissionInfo(FamilySsoContract.PERMISSION, 0)
        assertEquals(PermissionInfo.PROTECTION_SIGNATURE, permission.protection)
    }

    @Test
    fun `bind with another action gets no binder`() {
        val other = Robolectric.buildService(FamilySsoService::class.java).create().get()
        assertNull(other.onBind(Intent("org.example.OTHER")))
        assertNull(other.onBind(null))
    }

    @Test
    fun `family app signed with Hub's certificate gets the bundle and a proof`() {
        ShadowBinder.setCallingUid(childUid)
        assertEquals(FamilySsoContract.VERSION, service.version)
        val bundle = assertNotNullAndGet(service.ssoBundle)
        assertEquals(issuer, bundle.issuer)
        assertEquals("hub-id-token-value", bundle.idToken)
        assertEquals("device-secret-value", bundle.deviceSecret)
        assertEquals("openid profile device_sso", bundle.scope)
        val proof = verifiedJws(service.signDpopProof("POST", tokenEndpoint, null, null), app.dpopSigner.publicJwk)
        assertEquals(tokenEndpoint, proof.payload.toJSONObject()["htu"])
        assertEquals(listOf(FamilySsoContract.CHILD_APP_PACKAGE), FamilyAccessLog(app.store).list().map { it.packageName })
    }

    @Test
    fun `caller with a different signature is rejected on every call`() {
        // Health Card's package name, signed with the Android debug certificate (the intruder build).
        install(FamilySsoContract.CHILD_APP_PACKAGE, intruderUid, debugCert)
        ShadowBinder.setCallingUid(intruderUid)
        refusedWith(FamilySsoContract.ERROR_CALLER_NOT_ALLOWED) { service.version }
        refusedWith(FamilySsoContract.ERROR_CALLER_NOT_ALLOWED) { service.ssoBundle }
        refusedWith(FamilySsoContract.ERROR_CALLER_NOT_ALLOWED) { service.signDpopProof("POST", tokenEndpoint, null, null) }
        assertTrue(FamilyAccessLog(app.store).list().isEmpty())
    }

    @Test
    fun `uid with no package is rejected`() {
        ShadowBinder.setCallingUid(ownerlessUid)
        refusedWith(FamilySsoContract.ERROR_CALLER_NOT_ALLOWED) { service.ssoBundle }
    }

    @Test
    fun `not signed in - null bundle and ERROR_NOT_SIGNED_IN for proofs`() {
        app.session = null
        ShadowBinder.setCallingUid(childUid)
        assertNull(service.ssoBundle)
        refusedWith(FamilySsoContract.ERROR_NOT_SIGNED_IN) { service.signDpopProof("POST", tokenEndpoint, null, null) }
    }

    @Test
    fun `foreign htu is refused with ERROR_HTU_NOT_ALLOWED`() {
        ShadowBinder.setCallingUid(childUid)
        refusedWith(FamilySsoContract.ERROR_HTU_NOT_ALLOWED) { service.signDpopProof("POST", "https://attacker.example/token", null, null) }
    }
}
