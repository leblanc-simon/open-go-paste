import assert from 'node:assert/strict';
import test from 'node:test';

// crypto.js only needs a window carrying the WebCrypto API and the fragment of
// the current url.
function setLocationHash(hash) {
    globalThis.window = {
        crypto: globalThis.crypto,
        location: { hash: hash },
    };
}

setLocationHash('');

const { default: Crypto } = await import('../server/assets/js/crypto.js');

async function encryptedFragmentOf(text) {
    setLocationHash('');
    const crypto = new Crypto();
    const encrypted = await crypto.encrypt(text);

    return { data: encrypted.join(','), fragment: crypto.getFragment() };
}

test('a paste encrypted with the default algorithm is read back', async () => {
    const clearText = 'héllo, wörld — 42\n\tmultiline';
    const { data, fragment } = await encryptedFragmentOf(clearText);

    // The marker announces AES-GCM, which is authenticated where AES-CBC was
    // only confidential.
    assert.equal(fragment.split('|')[0], 'g');

    setLocationHash('#' + fragment);
    const reader = new Crypto();

    assert.equal(await reader.decrypt(data.split(',')), clearText);
});

test('a tampered ciphertext is rejected', async () => {
    const { data, fragment } = await encryptedFragmentOf('the original message');

    const bytes = data.split(',').map(Number);
    bytes[0] = (bytes[0] + 1) % 256;

    setLocationHash('#' + fragment);
    const reader = new Crypto();

    await assert.rejects(() => reader.decrypt(bytes.map(String)));
});

test('a percent encoded fragment is still read', async () => {
    const clearText = 'encoded fragment';
    const { data, fragment } = await encryptedFragmentOf(clearText);

    setLocationHash('#' + fragment.replace(/\|/g, '%7C'));
    const reader = new Crypto();

    assert.equal(await reader.decrypt(data.split(',')), clearText);
});

// Regression test for the key shared between two pastes: the link of the first
// used to decrypt the second.
test('two pastes never share a key', async () => {
    const first = await encryptedFragmentOf('first paste');
    const second = await encryptedFragmentOf('second paste');

    assert.notEqual(first.fragment, second.fragment);

    setLocationHash('#' + first.fragment);
    const reader = new Crypto();

    await assert.rejects(() => reader.decrypt(second.data.split(',')));
});

test('reset drops the key of the instance', async () => {
    setLocationHash('');
    const crypto = new Crypto();

    await crypto.encrypt('first paste');
    const firstFragment = crypto.getFragment();

    crypto.reset();

    await crypto.encrypt('second paste');

    assert.notEqual(crypto.getFragment(), firstFragment);
});

// Links handed out before the switch to AES-GCM carry no algorithm marker and
// encode their text as UTF-16: they must stay readable.
test('a legacy AES-CBC link is still readable', async () => {
    const clearText = 'a legacy paste';

    const key = await globalThis.crypto.subtle.generateKey(
        { name: 'AES-CBC', length: 256 },
        true,
        ['encrypt', 'decrypt']
    );
    const iv = globalThis.crypto.getRandomValues(new Uint8Array(16));

    const utf16 = new Uint16Array(clearText.length);
    for (let i = 0; i < clearText.length; i++) {
        utf16[i] = clearText.charCodeAt(i);
    }

    const encrypted = new Uint8Array(await globalThis.crypto.subtle.encrypt(
        { name: 'AES-CBC', iv: iv },
        key,
        utf16.buffer
    ));

    const jwk = await globalThis.crypto.subtle.exportKey('jwk', key);
    const legacyFragment = Array.from(iv).join(',') + '|' + btoa(JSON.stringify(jwk));

    setLocationHash('#' + legacyFragment);
    const reader = new Crypto();

    assert.equal(await reader.decrypt(Array.from(encrypted).map(String)), clearText);
});
