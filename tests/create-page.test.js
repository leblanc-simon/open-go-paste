import assert from 'node:assert/strict';
import test from 'node:test';
import { JSDOM } from 'jsdom';

// The creation page, run the way a browser runs it. What is checked here is the
// behaviour around the happy path: the lock that keeps clear text from leaving
// without encryption, the second click, and the four ways a submission can come
// back wrong — each of which used to read "this paste could not be saved".

const MESSAGES = {
    'save-error': 'This paste could not be saved.',
    'network-error': 'The service could not be reached.',
    'rate-limited': 'Too many requests from your address.',
    'too-large': 'This text is too long for the service.',
    'encryption-failed': 'Your browser could not encrypt this text.',
    'insecure-context': 'This address is not a secure one.',
    'copied': 'Link has been copied!',
    'copy-failed': 'The link could not be copied.',
    'saving': 'Encrypting and sending…',
};

function createMarkup() {
    const attributes = Object.entries(MESSAGES)
        .map(([name, value]) => `data-${name}="${value}"`)
        .join(' ');

    return `<!doctype html><html><body>
        <div id="messages" hidden ${attributes} data-max-content-bytes="4186112"></div>
        <div id="link" role="status" hidden>
            <div class="title">Copy link and send it.</div>
            <div class="link"></div>
            <div class="link-actions">
                <button type="button" id="copy-link" aria-label="Copy the link"></button>
                <a id="send-link" aria-label="Send the link by email"></a>
                <span class="copy-feedback" role="status"></span>
            </div>
        </div>
        <p id="paste-error" class="notice notice--error" role="alert" hidden></p>
        <form method="post" action="http://localhost/" name="paste">
            <fieldset id="paste-fields" disabled>
                <select name="max_time"><option value="1h" selected>1 hour</option></select>
                <select name="type"><option value="text" selected>text</option></select>
                <input type="number" value="1" name="max_read">
                <textarea name="paste" id="paste"></textarea>
                <button type="submit" data-label="Save">Save</button>
            </fieldset>
        </form>
    </body></html>`;
}

let run = 0;

// respond drives the fake fetch: it records the calls and answers what the
// scenario asks for.
async function renderCreatePage({ respond, withCrypto = true } = {}) {
    const dom = new JSDOM(createMarkup(), { url: 'http://localhost/' });

    if (withCrypto) {
        Object.defineProperty(dom.window, 'crypto', { value: globalThis.crypto });
    } else {
        // What a page served over plain HTTP looks like: no subtle at all.
        Object.defineProperty(dom.window, 'crypto', { value: {} });
    }

    dom.window.requestAnimationFrame = callback => callback();

    globalThis.window = dom.window;
    globalThis.document = dom.window.document;
    globalThis.requestAnimationFrame = dom.window.requestAnimationFrame;
    globalThis.FormData = dom.window.FormData;
    globalThis.DOMParser = dom.window.DOMParser;
    // navigator is a getter only property on globalThis in Node.
    Object.defineProperty(globalThis, 'navigator', {
        value: { clipboard: { writeText: async () => {} } },
        configurable: true,
        writable: true,
    });

    const calls = [];
    globalThis.fetch = async (url, options) => {
        calls.push({ url, options });

        return respond ? respond(calls.length) : new Response('a-paste-id', { status: 200 });
    };

    run += 1;
    await import(`../server/assets/js/app.js?create=${run}`);

    const document = dom.window.document;

    return {
        document,
        calls,
        form: document.querySelector('form[name="paste"]'),
        fields: document.getElementById('paste-fields'),
        error: document.getElementById('paste-error'),
        submitButton: document.querySelector('button[type="submit"]'),
        submit() {
            this.form.dispatchEvent(new dom.window.Event('submit', { cancelable: true, bubbles: true }));
        },
        async settle() {
            await new Promise(resolve => setTimeout(resolve, 10));
        },
        visibleError() {
            return this.error.hidden ? '' : this.error.textContent;
        },
    };
}

test('the form is unlocked once the browser is known to encrypt', async () => {
    const page = await renderCreatePage();

    assert.equal(page.fields.disabled, false);
    assert.equal(page.visibleError(), '');
});

// Web Crypto is absent outside a secure context, so this is a service published
// over plain HTTP. The form has to stay shut: submitting would store clear text.
test('without usable encryption the form stays locked and says why', async () => {
    const page = await renderCreatePage({ withCrypto: false });

    assert.equal(page.fields.disabled, true);
    assert.equal(page.visibleError(), MESSAGES['insecure-context']);
});

test('a paste is encrypted before it is sent, and never sent in clear', async () => {
    const page = await renderCreatePage();
    const clearText = 'the confidential text';

    page.document.getElementById('paste').value = clearText;
    page.submit();
    await page.settle();

    assert.equal(page.calls.length, 1);

    const sent = page.calls[0].options.body.get('paste');
    assert.notEqual(sent, clearText);
    assert.match(sent, /^[0-9,]+$/);
    // The marker the server requires before it stores anything.
    assert.equal(page.calls[0].options.body.get('encrypted'), '1');

    // And the link is offered, with its key in the fragment.
    const link = page.document.querySelector('#link .link a');
    assert.match(link.textContent, /^http:\/\/localhost\/p\/a-paste-id#g\|/);
    assert.equal(page.document.getElementById('link').hidden, false);
    assert.match(page.document.getElementById('send-link').getAttribute('href'), /^mailto:\?body=/);
});

// Clicking twice used to create two pastes, the first one orphaned with nobody
// holding its link — and, on a service that counts reads, unreachable forever.
test('a second submission while the first is in flight is ignored', async () => {
    let release;
    const held = new Promise(resolve => { release = resolve; });

    const page = await renderCreatePage({
        respond: async () => {
            await held;

            return new Response('a-paste-id', { status: 200 });
        },
    });

    page.document.getElementById('paste').value = 'the confidential text';
    page.submit();
    page.submit();
    page.submit();

    // The button shuts as the first submission starts, before the encryption
    // it has to await.
    assert.equal(page.submitButton.disabled, true);
    assert.equal(page.submitButton.textContent, MESSAGES['saving']);

    // Long enough for three encryptions to have reached fetch, had they run.
    await page.settle();
    assert.equal(page.calls.length, 1);

    release();
    await page.settle();

    assert.equal(page.calls.length, 1);
    assert.equal(page.submitButton.disabled, false);
    assert.equal(page.submitButton.textContent, 'Save');
});

test('each way a submission fails gets its own message', async () => {
    const cases = [
        { status: 429, expected: MESSAGES['rate-limited'] },
        { status: 413, expected: MESSAGES['too-large'] },
        { status: 500, expected: MESSAGES['save-error'] },
    ];

    for (const { status, expected } of cases) {
        const page = await renderCreatePage({
            respond: async () => new Response('', { status: status }),
        });

        page.document.getElementById('paste').value = 'the confidential text';
        page.submit();
        await page.settle();

        assert.equal(page.visibleError(), expected, `status ${status}`);
        // No link is offered for a paste that was never stored.
        assert.equal(page.document.getElementById('link').hidden, true);
    }
});

// fetch only rejects on a transport failure: offline, DNS, a dropped
// connection. Retrying is the right advice there, and only there.
test('a connection failure is named as one', async () => {
    const page = await renderCreatePage({
        respond: async () => { throw new TypeError('Failed to fetch'); },
    });

    page.document.getElementById('paste').value = 'the confidential text';
    page.submit();
    await page.settle();

    assert.equal(page.visibleError(), MESSAGES['network-error']);
});

// On every failing path the field holds the only copy of the text left.
test('the text is handed back to the writer whatever went wrong', async () => {
    const clearText = 'the text that must survive the failure';

    const page = await renderCreatePage({
        respond: async () => { throw new TypeError('Failed to fetch'); },
    });

    page.document.getElementById('paste').value = clearText;
    page.submit();
    await page.settle();

    assert.equal(page.document.getElementById('paste').value, clearText);
});

test('a paste beyond the ceiling is refused before it is uploaded', async () => {
    const page = await renderCreatePage();

    // The ceiling counts the encrypted payload, which runs about four
    // characters per byte of clear text.
    page.document.getElementById('paste').value = 'x'.repeat(2 * 1024 * 1024);
    page.submit();
    await page.settle();

    assert.equal(page.visibleError(), MESSAGES['too-large']);
    assert.equal(page.calls.length, 0, 'nothing should have been uploaded');
});

test('a refused clipboard tells the reader to copy by hand', async () => {
    const page = await renderCreatePage();

    page.document.getElementById('paste').value = 'the confidential text';
    page.submit();
    await page.settle();

    // Both the API and the execCommand fallback refuse, which is what a browser
    // without clipboard permission does.
    globalThis.navigator.clipboard.writeText = async () => { throw new Error('denied'); };
    page.document.execCommand = () => false;

    page.document.getElementById('copy-link').dispatchEvent(
        new page.document.defaultView.Event('click', { bubbles: true })
    );
    await page.settle();

    assert.equal(page.document.querySelector('.copy-feedback').textContent, MESSAGES['copy-failed']);
});
