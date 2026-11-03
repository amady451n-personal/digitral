/*
 * axis_crypto_tap.js — axisnet (com.axis.net) crypto tap, class-name-agnostic.
 *
 * Konteks: R8 me-rename okhttp3/retrofit2 di axisnet (hanya okio yang tersisa
 * sebagai paket), jadi tap OkHttp by-name tidak menemukan target. Hook
 * javax.crypto.* + java.security.MessageDigest + java.net.URL — semuanya nama
 * platform yang tak berubah. Target data: nama header & resep signature yang
 * memakai saltKey/secretKey/keyTempest (docs/bima-protocol.md §15) + enkripsi
 * body (aes-everywhere).
 *
 * Batas sesi: NO LOGIN, NO OTP. Hanya init app + panggilan publik (banner,
 * config). Anti-tamper §13: watchdog bisa membunuh target ±30 s setelah
 * attach — mitigasi: spawn gating + relaunch tanpa hook bila dibunuh.
 *
 * Jalankan dari host yang sudah bisa adb ke MuMu (§13 plumbing):
 *   frida -U -f com.axis.net -l scripts/frida/axis_crypto_tap.js
 * Output: baris berawalan [TAP] di stdout.
 */

'use strict';

function hex(arr, cap) {
    var n = Math.min(arr.length, cap || 512);
    var s = '';
    for (var i = 0; i < n; i++) {
        var h = (arr[i] & 0xff).toString(16);
        s += (h.length < 2 ? '0' : '') + h;
    }
    return s + (arr.length > n ? '…(' + arr.length + 'B)' : '');
}

function desc(spec) {
    if (spec === null || spec === undefined) return '<no-spec>';
    try {
        var ks = Java.cast(spec, Java.use('javax.crypto.spec.SecretKeySpec'));
        return 'alg=' + ks.getAlgorithm() + ' key=' + hex(ks.getEncoded(), 64);
    } catch (e) {
        try {
            var iv = Java.cast(spec, Java.use('javax.crypto.spec.IvParameterSpec'));
            return 'iv=' + hex(iv.getIV(), 32);
        } catch (e2) {
            return '<spec?>' + e;
        }
    }
}

Java.perform(function () {
    console.log('[TAP] attached; hooking javax.crypto / java.security / java.net.URL');

    // javax.crypto.Mac — HMAC: inti signature scheme
    try {
        var Mac = Java.use('javax.crypto.Mac');
        Mac.doFinal.overload('[B').implementation = function (msg) {
            var out = this.doFinal(msg);
            console.log('[TAP] HMAC alg=' + this.getAlgorithm() + ' msg=' + hex(msg) + ' -> ' + hex(out, 96));
            return out;
        };
        Mac.doFinal.overload('()').implementation = function () {
            var out = this.doFinal();
            console.log('[TAP] HMAC(fed) alg=' + this.getAlgorithm() + ' -> ' + hex(out, 96));
            return out;
        };
    } catch (e) { console.log('[TAP] Mac hook limited: ' + e); }

    // javax.crypto.Cipher — body AES / helper
    try {
        var Cipher = Java.use('javax.crypto.Cipher');
        var tapKey = {};                       // per-instance holder via closure-map
        Cipher.init.overload('int', 'java.security.Key').implementation = function (op, k) {
            tapKey[this.hashCode()] = desc(k);
            console.log('[TAP] Cipher.init ' + this.getAlgorithm() + ' op=' + op + ' ' + desc(k));
            return this.init(op, k);
        };
        Cipher.init.overload('int', 'java.security.Key', 'java.security.spec.AlgorithmParameterSpec').implementation = function (op, k, p) {
            tapKey[this.hashCode()] = desc(k) + ' ' + desc(p);
            console.log('[TAP] Cipher.init ' + this.getAlgorithm() + ' op=' + op + ' ' + desc(k) + ' ' + desc(p));
            return this.init(op, k, p);
        };
        Cipher.doFinal.overload('[B').implementation = function (msg) {
            var out = this.doFinal(msg);
            console.log('[TAP] CIPHER ' + this.getAlgorithm() + ' ' +
                (tapKey[this.hashCode()] || '<no-init>') + ' in=' + hex(msg) + ' out=' + hex(out, 128));
            delete tapKey[this.hashCode()];
            return out;
        };
    } catch (e) { console.log('[TAP] Cipher hook limited: ' + e); }

    // java.security.MessageDigest — sha/md5 (saltKey concat)
    try {
        var MD = Java.use('java.security.MessageDigest');
        MD.digest.overload('[B').implementation = function (msg) {
            var out = this.digest(msg);
            console.log('[TAP] DIGEST ' + this.getAlgorithm() + ' in=' + hex(msg) + ' -> ' + hex(out, 64));
            return out;
        };
        MD.digest.overload('()').implementation = function () {
            var out = this.digest();
            console.log('[TAP] DIGEST(fed) ' + this.getAlgorithm() + ' -> ' + hex(out, 64));
            return out;
        };
    } catch (e) { console.log('[TAP] MessageDigest hook limited: ' + e); }

    // javax.crypto.spec.SecretKeySpec + IvParameterSpec — log constructor
    try {
        var SKS = Java.use('javax.crypto.spec.SecretKeySpec');
        SKS.$init.overload('[B', 'java.lang.String').implementation = function (kb, alg) {
            console.log('[TAP] SecretKeySpec alg=' + alg + ' key=' + hex(kb, 64));
            return this.$init(kb, alg);
        };
        SKS.$init.overload('[B', 'int', 'int', 'java.lang.String').implementation = function (kb, off, len, alg) {
            console.log('[TAP] SecretKeySpec alg=' + alg + ' key[' + off + '+ ' + len + ']=' + hex(kb, 64));
            return this.$init(kb, off, len, alg);
        };
        var IPS = Java.use('javax.crypto.spec.IvParameterSpec');
        IPS.$init.overload('[B').implementation = function (iv) {
            console.log('[TAP] IvParameterSpec iv=' + hex(iv, 32));
            return this.$init(iv);
        };
    } catch (e) { console.log('[TAP] Spec hooks limited: ' + e); }

    // java.net.URL — hanya endpoint axis
    try {
        var URLC = Java.use('java.net.URL');
        URLC.$init.overload('java.lang.String').implementation = function (s) {
            if (s && s.indexOf('axis.co.id') >= 0) console.log('[TAP] URL ' + s);
            return this.$init(s);
        };
    } catch (e) { console.log('[TAP] URL hook limited: ' + e); }

    console.log('[TAP] hooks siap — tunggu init/public calls (NO login / NO OTP)');
});
