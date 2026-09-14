import assert from 'node:assert/strict';
import test from 'node:test';
import { JSDOM } from 'jsdom';

// These exercise app.js the way a browser runs it: build the page the template
// renders, hand it a url, import the module, and look at what the reader ends up
// seeing. The failures used to reach the console only, leaving a blank page
// behind — after the read had already been spent on the server.

const MESSAGES = {
    'link-incomplete': 'This link is incomplete.',
    'decryption-failed': 'This content could not be unlocked.',
    'insecure-context': 'This address is not a secure one.',
};

function pasteMarkup(encryptedValue) {
    const attributes = Object.entries(MESSAGES)
        .map(([name, value]) => `data-${name}="${value}"`)
        .join(' ');

    return `<!doctype html><html><body>
        <div id="messages" hidden ${attributes}></div>
        <div class="paste-information" role="status" hidden
            data-information="seen §nbAvailableRead§ more times, until §dateAvailable§ at §hourAvailable§"
            data-information-no-nb-read="until §dateAvailable§ at §hourAvailable§"
            data-last-read="last read"></div>
        <p id="paste-error" class="notice notice--error" role="alert" hidden></p>
        <div id="paste-content"
            data-encrypted-value="${encryptedValue}"
            data-nb-of-read-allowed="3"
            data-nb-of-read="1"
            data-expire-time="2099-01-01T00:00:00Z"
            data-type="text"></div>
    </body></html>`;
}

// Each scenario needs its own evaluation of app.js, and a module is only
// evaluated once per specifier: the counter is what gets a fresh one.
let run = 0;

async function renderPastePage(encryptedValue, hash) {
    const dom = new JSDOM(pasteMarkup(encryptedValue), {
        url: `http://localhost/p/a-paste${hash}`,
    });

    // crypto.js reaches for window.crypto, which jsdom does not implement.
    Object.defineProperty(dom.window, 'crypto', { value: globalThis.crypto });
    dom.window.requestAnimationFrame = callback => callback();

    globalThis.window = dom.window;
    globalThis.document = dom.window.document;
    globalThis.requestAnimationFrame = dom.window.requestAnimationFrame;
    globalThis.DOMParser = dom.window.DOMParser;

    run += 1;
    await import(`../server/assets/js/app.js?run=${run}`);

    await settled(dom.window.document);

    return dom.window.document;
}

// Waits for the decryption to have produced something rather than for a fixed
// delay: a timeout tuned on an idle machine turns flaky as soon as the other
// test files run alongside this one.
async function settled(document) {
    const content = document.getElementById('paste-content');
    const error = document.getElementById('paste-error');

    for (let attempt = 0; attempt < 200; attempt++) {
        if ('' !== content.textContent || !error.hidden) {
            return;
        }

        await new Promise(resolve => setTimeout(resolve, 5));
    }

    throw new Error('the page neither displayed a paste nor an error');
}

// crypto.js is imported through the same fresh specifier as app.js, so the two
// share one module instance and one view of window.
async function encryptedPaste(text) {
    const dom = new JSDOM('<!doctype html><html><body></body></html>', {
        url: 'http://localhost/',
    });
    Object.defineProperty(dom.window, 'crypto', { value: globalThis.crypto });
    globalThis.window = dom.window;

    const { default: Crypto } = await import('../server/assets/js/crypto.js?paste');
    const crypto = new Crypto();
    const encrypted = await crypto.encrypt(text);

    return { data: encrypted.join(','), fragment: crypto.getFragment() };
}

function visibleError(document) {
    const element = document.getElementById('paste-error');

    return element.hidden ? '' : element.textContent;
}

test('a paste opened with its whole link is displayed', async () => {
    const clearText = 'the text only the link can open';
    const { data, fragment } = await encryptedPaste(clearText);

    const document = await renderPastePage(data, '#' + fragment);

    assert.equal(document.getElementById('paste-content').textContent, clearText);
    assert.equal(visibleError(document), '');

    // And the reader is told how long it stays available.
    const information = document.querySelector('.paste-information');
    assert.equal(information.hidden, false);
    assert.match(information.textContent, /seen 2 more times/);
});

test('a link cut before its key explains itself instead of showing nothing', async () => {
    const { data } = await encryptedPaste('a text nobody will read');

    const document = await renderPastePage(data, '');

    assert.equal(visibleError(document), MESSAGES['link-incomplete']);
    assert.equal(document.getElementById('paste-content').textContent, '');

    // The expiry notice would promise reads that cannot happen.
    assert.equal(document.querySelector('.paste-information').hidden, true);
});

test('a link whose key does not match says so', async () => {
    const first = await encryptedPaste('the first paste');
    const second = await encryptedPaste('the second paste');

    const document = await renderPastePage(second.data, '#' + first.fragment);

    assert.equal(visibleError(document), MESSAGES['decryption-failed']);
});

test('a truncated fragment is reported as an incomplete link', async () => {
    const { data, fragment } = await encryptedPaste('a text nobody will read');

    const document = await renderPastePage(data, '#' + fragment.slice(0, -40));

    assert.equal(visibleError(document), MESSAGES['link-incomplete']);
});
