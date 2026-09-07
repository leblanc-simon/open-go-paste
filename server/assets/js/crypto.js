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

export default class Crypto {
    constructor() {
        // The key belongs to the instance, never to the module: a key shared
        // between two pastes would make the link of the first decrypt the
        // second.
        this._key = null;
        this.jwk = null;
        this.iv = null;
        this.algorithm = defaultAlgorithm;

        if (window.location.hash) {
            this.readFragment(window.location.hash.substring(1));
        }
    }

    readFragment(fragment) {
        // Some clients percent encode the fragment before handing it over.
        if (/%7C/i.test(fragment)) {
            fragment = decodeURIComponent(fragment);
        }

        const parts = fragment.split('|');

        if (algorithmMarkers[parts[0]]) {
            this.algorithm = algorithmMarkers[parts.shift()];
        } else {
            this.algorithm = legacyAlgorithm;
        }

        const ivBytes = parts.shift().split(',');
        this.iv = new Uint8Array(ivBytes.length);
        ivBytes.forEach((ivByte, index) => {
            this.iv[index] = Number(ivByte);
        });

        this.jwk = JSON.parse(atob(parts.join('|')));
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
        const key = await this.key();
        const decrypted = await window.crypto.subtle.decrypt(
            Crypto.getEncryptDecryptOptions(this.algorithm, this.iv),
            key,
            new Uint8Array(data.map(Number)).buffer
        );

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

    static support() {
        return (window.crypto && window.crypto.subtle && window.crypto.subtle.encrypt);
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
