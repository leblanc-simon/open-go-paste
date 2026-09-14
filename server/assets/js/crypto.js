// AES-GCM is authenticated: whoever holds the ciphertext, the operator of the
// service included, cannot alter the content without the recipient noticing.
// AES-CBC, used until then, is malleable and stays supported for reading only.
const defaultAlgorithm = 'AES-GCM';
const legacyAlgorithm = 'AES-CBC';
const keyLength = 256;

// Initialisation vector sizes, in bytes: 12 is the size AES-GCM is specified
// for, AES-CBC needs a full block.
const ivSizes = {
    'AES-GCM': 12,
    'AES-CBC': 16,
};

// The fragment holds "<marker>|<iv bytes>|<base64 jwk>". Links handed out before
// the switch to AES-GCM carry no marker and start with their iv, so they are
// still readable; anything else is a current link.
const algorithmMarkers = {
    'g': 'AES-GCM',
    'c': 'AES-CBC',
};
const markersByAlgorithm = {
    'AES-GCM': 'g',
    'AES-CBC': 'c',
};

// The reasons a paste can stay unreadable. They exist so that app.js can tell
// the reader which one happened: a link truncated on its way is not the same
// accident as a key that does not match, and neither is a browser that cannot
// decrypt at all. Without them every failure looked alike — an empty page.
export const CryptoFailure = {
    // No fragment at all: the link was cut after the "#", which is what mail
    // clients and chat gateways do to long URLs.
    MissingKey: 'missing-key',
    // A fragment is there but does not parse: truncated mid-key, or mangled.
    MalformedKey: 'malformed-key',
    // The ciphertext did not authenticate against the key of the link.
    WrongKey: 'wrong-key',
    // No Web Crypto: the page is served over plain HTTP, or the browser is too
    // old. Nothing can be encrypted nor decrypted here.
    Unsupported: 'unsupported',
};

export class CryptoError extends Error {
    constructor(failure, cause) {
        super(`crypto failure: ${failure}`);
        this.name = 'CryptoError';
        this.failure = failure;
        this.cause = cause;
    }
}

export default class Crypto {
    constructor() {
        // The key belongs to the instance, never to the module: a key shared
        // between two pastes would make the link of the first decrypt the
        // second.
        this._key = null;
        this.jwk = null;
        this.iv = null;
        this.algorithm = defaultAlgorithm;
        // A fragment that does not parse is kept aside rather than thrown from
        // the constructor: the creation page builds an instance too, and a
        // stray hash in its url must not stop it from encrypting. decrypt is
        // where the failure matters, and where it is raised.
        this.fragmentError = null;

        const fragment = window.location.hash.substring(1);
        if ('' === fragment) {
            return;
        }

        try {
            this.readFragment(fragment);
        } catch (error) {
            this.reset();
            this.fragmentError = error;
        }
    }

    // readFragment throws rather than leaving a half built key behind: a paste
    // that cannot be read has to say so, and every malformed fragment used to
    // end up as the same silent rejection further down.
    readFragment(fragment) {
        // Some clients percent encode the fragment before handing it over.
        if (/%7C/i.test(fragment)) {
            try {
                fragment = decodeURIComponent(fragment);
            } catch (error) {
                throw new CryptoError(CryptoFailure.MalformedKey, error);
            }
        }

        const parts = fragment.split('|');

        if (algorithmMarkers[parts[0]]) {
            this.algorithm = algorithmMarkers[parts.shift()];
        } else {
            this.algorithm = legacyAlgorithm;
        }

        // A marker on its own, or a fragment cut right after it.
        if (parts.length < 2) {
            throw new CryptoError(CryptoFailure.MalformedKey);
        }

        const ivBytes = parts.shift().split(',');
        if (ivBytes.length !== ivSizes[this.algorithm]) {
            throw new CryptoError(CryptoFailure.MalformedKey);
        }

        this.iv = new Uint8Array(ivBytes.length);
        for (let index = 0; index < ivBytes.length; index++) {
            const value = Number(ivBytes[index]);
            if (!Number.isInteger(value) || value < 0 || value > 255) {
                throw new CryptoError(CryptoFailure.MalformedKey);
            }

            this.iv[index] = value;
        }

        try {
            this.jwk = JSON.parse(atob(parts.join('|')));
        } catch (error) {
            // atob rejects anything that is not base64, JSON.parse anything
            // that is not an object: both mean a fragment that lost characters.
            throw new CryptoError(CryptoFailure.MalformedKey, error);
        }

        if (null === this.jwk || 'object' !== typeof this.jwk) {
            throw new CryptoError(CryptoFailure.MalformedKey);
        }
    }

    // getFragment returns what has to be appended to the link, and is the only
    // place where the key leaves the browser.
    getFragment() {
        return [
            markersByAlgorithm[this.algorithm],
            Array.from(this.iv).join(','),
            btoa(JSON.stringify(this.jwk)),
        ].join('|');
    }

    async encrypt(data) {
        this.iv = window.crypto.getRandomValues(new Uint8Array(ivSizes[this.algorithm]));

        const key = await this.key();
        const encrypted = await window.crypto.subtle.encrypt(
            Crypto.getEncryptDecryptOptions(this.algorithm, this.iv),
            key,
            Crypto.encodeText(data, this.algorithm)
        );

        return new Uint8Array(encrypted);
    }

    async decrypt(data) {
        if (null !== this.fragmentError) {
            throw this.fragmentError;
        }

        if (null === this.jwk) {
            throw new CryptoError(CryptoFailure.MissingKey);
        }

        const key = await this.key();

        let decrypted;
        try {
            decrypted = await window.crypto.subtle.decrypt(
                Crypto.getEncryptDecryptOptions(this.algorithm, this.iv),
                key,
                new Uint8Array(data.map(Number)).buffer
            );
        } catch (error) {
            // AES-GCM authenticates: a rejection here means the key of the link
            // does not open this ciphertext, or that the ciphertext was
            // altered. Either way the reader cannot recover the content.
            throw new CryptoError(CryptoFailure.WrongKey, error);
        }

        return Crypto.decodeText(decrypted, this.algorithm);
    }

    async key() {
        if (null !== this._key) {
            return this._key;
        }

        if (this.jwk) {
            this._key = await this.importKeyFromFragment();
        } else {
            this._key = await this.generateNewKey();
        }

        return this._key;
    }

    // reset drops the key and its initialisation vector, so that the next paste
    // created without reloading the page gets its own.
    reset() {
        this._key = null;
        this.jwk = null;
        this.iv = null;
        this.algorithm = defaultAlgorithm;
        this.fragmentError = null;
    }

    importKeyFromFragment() {
        return window.crypto.subtle.importKey(
            'jwk',
            this.jwk,
            {
                name: this.algorithm,
            },
            true,
            ['encrypt', 'decrypt']
        );
    }

    async generateNewKey() {
        const key = await window.crypto.subtle.generateKey(
            {
                name: this.algorithm,
                length: keyLength,
            },
            true,
            ['encrypt', 'decrypt']
        );

        this.jwk = await window.crypto.subtle.exportKey('jwk', key);

        return key;
    }

    static getEncryptDecryptOptions(algorithm, iv) {
        switch (algorithm) {
            case 'AES-CTR':
                return {
                    name: 'AES-CTR',
                    counter: iv,
                    length: 128,
                };
            case 'AES-CBC':
                return {
                    name: 'AES-CBC',
                    iv: iv,
                };
            case 'AES-GCM':
                return {
                    name: 'AES-GCM',
                    iv: iv,
                };
        }

        throw new Error(`Unsupported algorithm ${algorithm}`);
    }

    // support answers whether this page can encrypt at all. window.crypto.subtle
    // is undefined outside a secure context, so a service published over plain
    // HTTP lands here — with nothing to warn the visitor until app.js asks.
    static support() {
        return Boolean(window.crypto && window.crypto.subtle && window.crypto.subtle.encrypt);
    }

    static encodeText(text, algorithm) {
        if (legacyAlgorithm === algorithm) {
            return Crypto.legacyEncodeText(text);
        }

        // UTF-8, so a paste no longer weighs twice its size.
        return new TextEncoder().encode(text);
    }

    static decodeText(buffer, algorithm) {
        if (legacyAlgorithm === algorithm) {
            return Crypto.legacyDecodeText(buffer);
        }

        return new TextDecoder().decode(buffer);
    }

    static legacyEncodeText(text) {
        const buffer = new ArrayBuffer(text.length * 2); // 2 bytes for each char
        const bufferView = new Uint16Array(buffer);

        for (let i = 0, textLength = text.length; i < textLength; i++) {
            bufferView[i] = text.charCodeAt(i);
        }

        return buffer;
    }

    static legacyDecodeText(buffer) {
        // String.fromCharCode.apply overflows the stack somewhere around 100k
        // characters, hence the chunks.
        const bufferView = new Uint16Array(buffer);
        const chunkSize = 8192;
        let text = '';

        for (let i = 0; i < bufferView.length; i += chunkSize) {
            text += String.fromCharCode.apply(null, bufferView.subarray(i, i + chunkSize));
        }

        return text;
    }
}
