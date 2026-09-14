import assert from 'node:assert/strict';
import test from 'node:test';

// Same harness as crypto.test.js: crypto.js only needs a window carrying the
// WebCrypto API and the fragment of the current url.
function setLocationHash(hash) {
    globalThis.window = {
        crypto: globalThis.crypto,
        location: { hash: hash },
    };
}

setLocationHash('');

const { default: Crypto, CryptoError, CryptoFailure } = await import('../server/assets/js/crypto.js');

async function encryptedFragmentOf(text) {
    setLocationHash('');
    const crypto = new Crypto();
    const encrypted = await crypto.encrypt(text);

    return { data: encrypted.join(','), fragment: crypto.getFragment() };
}

async function failureOf(hash, data) {
    setLocationHash(hash);
    const reader = new Crypto();

    try {
        await reader.decrypt(data.split(','));
    } catch (error) {
        assert.ok(error instanceof CryptoError, `expected a CryptoError, got ${error}`);

        return error.failure;
    }

    assert.fail('decrypt was expected to reject');
}

// The link reached the reader without its fragment: mail clients and chat
// gateways cut long urls, and everything after the "#" goes with them. The page
// used to stay blank while the read had already been spent.
test('a link without a fragment reports a missing key', async () => {
    const { data } = await encryptedFragmentOf('a paste nobody will read');

    assert.equal(await failureOf('', data), CryptoFailure.MissingKey);
});

test('a fragment truncated mid key reports a malformed key', async () => {
    const { data, fragment } = await encryptedFragmentOf('a paste nobody will read');

    // Half the base64 of the key survived the trip.
    const truncated = fragment.slice(0, fragment.length - 40);

    assert.equal(await failureOf('#' + truncated, data), CryptoFailure.MalformedKey);
});

test('a fragment holding only its algorithm marker reports a malformed key', async () => {
    const { data } = await encryptedFragmentOf('a paste nobody will read');

    assert.equal(await failureOf('#g', data), CryptoFailure.MalformedKey);
});

test('a fragment carrying an initialisation vector of the wrong size is rejected', async () => {
    const { data, fragment } = await encryptedFragmentOf('a paste nobody will read');

    const parts = fragment.split('|');
    // AES-GCM is specified for twelve bytes; eleven means a mangled fragment,
    // not a shorter vector.
    const shortened = [parts[0], parts[1].split(',').slice(0, 11).join(','), parts[2]].join('|');

    assert.equal(await failureOf('#' + shortened, data), CryptoFailure.MalformedKey);
});

// A whole, well formed link that simply does not open this paste — the fragment
// of another one, or a ciphertext altered on the way. Worth a different sentence
// than a truncated link: there is nothing to ask the sender to resend.
test('a well formed key that does not match reports a wrong key', async () => {
    const first = await encryptedFragmentOf('the first paste');
    const second = await encryptedFragmentOf('the second paste');

    assert.equal(await failureOf('#' + first.fragment, second.data), CryptoFailure.WrongKey);
});

test('a tampered ciphertext reports a wrong key', async () => {
    const { data, fragment } = await encryptedFragmentOf('the original message');

    const bytes = data.split(',').map(Number);
    bytes[0] = (bytes[0] + 1) % 256;

    assert.equal(await failureOf('#' + fragment, bytes.join(',')), CryptoFailure.WrongKey);
});

// The creation page builds a Crypto too. A stray fragment in its url must not
// stop it from encrypting, so the constructor keeps the failure aside instead of
// throwing it.
test('an unusable fragment does not stop the page from encrypting', async () => {
    setLocationHash('#not-a-fragment');
    const crypto = new Crypto();

    const encrypted = await crypto.encrypt('a new paste');
    assert.ok(encrypted.length > 0);

    // And the key it generated is its own, readable back.
    const fragment = crypto.getFragment();
    setLocationHash('#' + fragment);
    const reader = new Crypto();

    assert.equal(await reader.decrypt(encrypted.join(',').split(',')), 'a new paste');
});
