let _key = null;

const keyAlgorythm = 'AES-CTR';
const keyImportAlgorythm = 'A256CTR';
const keyLength = 256;
const cryptLength = 128;

export default class Crypto {
    constructor() {
        this.password = null;
        this.counter = null;
        if (window.location.hash) {
            const splitHash = window.location.hash.substring(1).split('|');
            this.counter = new ArrayBuffer(16);
            const counterDatas = splitHash.shift().split(',');
            let bufferView = new Uint8Array(this.counter);
            counterDatas.forEach((counterData, index) => {
                bufferView[index] = counterData;
            })
            this.password = splitHash.join('|');
        }
    }

    async encrypt(data) {
        const counter = window.crypto.getRandomValues(new Uint8Array(16));
        this.counter = counter;
        const key = await this.key;
        return new Promise(function (resolve, reject) {
            window.crypto.subtle.encrypt({
                    name: keyAlgorythm,
                    counter: counter,
                    length: cryptLength,
                },
                key,
                Crypto.str2ab(data)
            ).then(function(encrypted){
                //returns an ArrayBuffer containing the encrypted data
                resolve(new Uint8Array(encrypted));
            }).catch(reject);
        });
    }

    async decrypt(data) {
        const key = await this.key;
        const counter = this.counter;
        return new Promise(function (resolve, reject) {
            window.crypto.subtle.decrypt({
                    name: keyAlgorythm,
                    counter: counter,
                    length: cryptLength,
                },
                key,
                Crypto.Uint8ArrayToBuffer(data)
            ).then(function(decrypted){
                //returns an ArrayBuffer containing the encrypted data
                resolve(Crypto.ab2str(decrypted));
            }).catch(reject);
        });
    }

    get key() {
        let that = this;
        return new Promise(function (resolve, reject) {
            if (null !== _key) {
                resolve(_key);
                return;
            }

            if (that.password) {
                that.generateKeyFromPassword().then(key => {
                    resolve(key);
                });
            } else {
                that.generateNewKey().then(key => {
                    resolve(key);
                });
            }
        });
    }

    set key(key) {
        _key = key;
    }

    getPasswordAndCounter() {
        return this.counter.join(',') + '|' + this.password;
    }

    generateKeyFromPassword() {
        return window.crypto.subtle.importKey(
            'jwk',
            {
                alg: 'A256CTR',
                ext: true,
                k: this.password,
                key_opts: ['encrypt', 'decrypt'],
                kty: 'oct',
            },
            {
                name: keyAlgorythm,
            },
            true,
            ['encrypt', 'decrypt']
        );
    }

    generateNewKey() {
        let that = this;
        return new Promise(function (resolve, reject) {
            window.crypto.subtle.generateKey(
                {
                    name: keyAlgorythm,
                    length: keyLength,
                },
                true,
                ['encrypt', 'decrypt']
            ).then(function (key) {
                _key = key;
                window.crypto.subtle
                    .exportKey('jwk', key)
                    .then(function (keydata) {
                        that.password = keydata.k;
                        resolve(key);
                    })
                    .catch(reject)
            }).catch(reject);
        });
    }

    static support() {
        return (window.crypto && window.crypto.subtle && window.crypto.subtle.encrypt);
    }

    static ab2str(buffer) {
        return String.fromCharCode.apply(null, new Uint16Array(buffer));
    }

    static str2ab(string) {
        const buffer = new ArrayBuffer(string.length * 2); // 2 bytes for each char
        let bufferView = new Uint16Array(buffer);
        for (var i=0, strLen = string.length; i < strLen; i++) {
            bufferView[i] = string.charCodeAt(i);
        }
        return buffer;
    }

    static Uint8ArrayToBuffer(a) {
        const buffer = new ArrayBuffer(a.length);
        let bufferView = new Uint8Array(buffer);
        for (var i=0, strLen = a.length; i < strLen; i++) {
            bufferView[i] = a[i];
        }
        return buffer;
    }
}
