import Crypto, { CryptoError, CryptoFailure } from './crypto.js';
import sanitizeHtml from './sanitize.js';

// Every label reaches this file already translated, through the data-* of
// #messages: a Content-Security-Policy without 'unsafe-inline' rules out an
// inline script carrying a catalogue.
function messages() {
    const element = document.getElementById('messages');

    return {
        get(name) {
            if (!element) {
                return '';
            }

            return element.getAttribute(`data-${name}`) || '';
        },
        number(name, fallback) {
            const value = Number(element && element.getAttribute(`data-${name}`));

            return Number.isFinite(value) && value > 0 ? value : fallback;
        },
    };
}

// highlight.js renamed highlightBlock to highlightElement in 10.7, and the
// bundle shipped here is still a 9.x.
function highlight(element) {
    // The bundle is loaded with defer and only for the paste types that need
    // it: a network hiccup on it must not take the whole paste down with it.
    if ('undefined' === typeof hljs) {
        return;
    }

    if ('function' === typeof hljs.highlightElement) {
        hljs.highlightElement(element);
        return;
    }

    hljs.highlightBlock(element);
}

// show and hide drive the [hidden] attribute rather than a class: an element
// carrying a live role is only announced when it enters the accessibility tree,
// and the class it used to toggle left it there, off screen, all along.
function show(element) {
    if (!element) {
        return;
    }

    element.hidden = false;
    // The class lands on the next frame so the reveal has something to
    // transition from.
    requestAnimationFrame(() => element.classList.add('show'));
}

function hide(element) {
    if (!element) {
        return;
    }

    element.classList.remove('show');
    element.hidden = true;
}

function showError(message) {
    const errorElement = document.getElementById('paste-error');
    if (!errorElement || '' === message) {
        return;
    }

    errorElement.textContent = message;
    show(errorElement);
}

function hideError() {
    const errorElement = document.getElementById('paste-error');
    if (errorElement) {
        errorElement.textContent = '';
        hide(errorElement);
    }
}

// execCommand is deprecated, it only remains as a fallback for browsers without
// the clipboard API, and for the cases where the API rejects the write.
function copyWithExecCommand(text) {
    const textarea = document.createElement('textarea');
    textarea.value = text;
    document.body.appendChild(textarea);
    textarea.select();

    try {
        if (!document.execCommand('copy')) {
            throw new Error('Fail to copy the link to the clipboard');
        }
    } finally {
        document.body.removeChild(textarea);
    }
}

async function copyToClipboard(text) {
    if (!navigator.clipboard) {
        copyWithExecCommand(text);
        return;
    }

    try {
        await navigator.clipboard.writeText(text);
    } catch (error) {
        // The clipboard API needs the permission and a focused document, neither
        // of which is guaranteed here.
        copyWithExecCommand(text);
    }
}

function boot() {
    const labels = messages();

    // Nothing on this page works without Web Crypto, and it is absent outside a
    // secure context: a service published over plain HTTP reaches exactly here.
    // The form stays locked and says why, rather than looking ready and failing
    // on submit — or worse, on a paste nobody can read afterwards.
    if (!Crypto.support()) {
        showError(labels.get('insecure-context'));
        return;
    }

    const form = document.querySelector('form[name="paste"]');
    if (form) {
        initForm(form, labels);
    }

    const pasteContent = document.getElementById('paste-content');
    if (!pasteContent) {
        return;
    }

    const paste = readPasteData(pasteContent);

    displayPaste(pasteContent, paste)
        .then(() => displayPasteInformation(paste))
        .catch(error => {
            // The reader gets nothing out of this page: say which accident it
            // was, and leave out the expiry notice, which would only promise
            // reads that cannot happen.
            showError(decryptionMessage(error, labels));
            console.error(error);
        });
}

// decryptionMessage turns a failure into the sentence that fits it. A link cut
// short by a mail client, a key that does not match, and a browser that cannot
// decrypt are three different accidents, and only one of them is worth asking
// the sender about.
function decryptionMessage(error, labels) {
    if (!(error instanceof CryptoError)) {
        return labels.get('decryption-failed');
    }

    switch (error.failure) {
        case CryptoFailure.MissingKey:
        case CryptoFailure.MalformedKey:
            return labels.get('link-incomplete');
        case CryptoFailure.Unsupported:
            return labels.get('insecure-context');
        default:
            return labels.get('decryption-failed');
    }
}

function showLink(form, pasteId, fragment) {
    const linkDiv = document.getElementById('link');
    const sendLink = document.getElementById('send-link');
    const pasteUrl = `${form.action}p/${pasteId}#${fragment}`;

    const anchor = document.createElement('a');
    anchor.setAttribute('href', `/p/${pasteId}#${fragment}`);
    anchor.textContent = pasteUrl;

    const linkContainer = linkDiv.querySelector('.link');
    linkContainer.textContent = '';
    linkContainer.appendChild(anchor);

    sendLink.setAttribute('href', `mailto:?body=${encodeURIComponent(`<${pasteUrl}>`)}`);
    show(linkDiv);

    return pasteUrl;
}

// submitError maps what came back to something the visitor can act on. The
// generic "please try again" is the last resort, not the first answer: retrying
// is useless against a quota or an oversized paste.
function submitError(response, labels) {
    switch (response.status) {
        case 429:
            return labels.get('rate-limited');
        case 413:
            return labels.get('too-large');
        default:
            return labels.get('save-error');
    }
}

function initForm(form, labels) {
    const fields = document.getElementById('paste-fields');
    const submitButton = form.querySelector('button[type="submit"]');
    const maxContentBytes = labels.number('max-content-bytes', 0);
    let submitting = false;

    // The browser can encrypt, so the form is usable. It ships locked, which is
    // what keeps a browser without JavaScript from posting the text in clear.
    if (fields) {
        fields.disabled = false;
    }

    form.addEventListener('submit', event => {
        event.preventDefault();

        // Clicking twice used to create two pastes, the first one orphaned with
        // nobody holding its link.
        if (submitting) {
            return;
        }

        submitting = true;
        setSubmitting(submitButton, labels.get('saving'));

        submitPaste(form, labels, maxContentBytes).finally(() => {
            submitting = false;
            setSubmitting(submitButton, '');
        });
    }, false);

    const copyLink = document.getElementById('copy-link');
    if (copyLink) {
        copyLink.addEventListener('click', () => copyPasteLink(copyLink, labels));
    }
}

// An empty label puts the button back the way the template rendered it.
function setSubmitting(button, label) {
    if (!button) {
        return;
    }

    button.disabled = '' !== label;
    button.textContent = '' !== label ? label : button.getAttribute('data-label');
}

function copyPasteLink(button, labels) {
    const feedback = document.querySelector('.copy-feedback');
    const link = document.querySelector('#link .link a');
    if (!link) {
        return;
    }

    const announce = message => {
        if (!feedback) {
            return;
        }

        feedback.textContent = message;
        window.setTimeout(() => {
            feedback.textContent = '';
        }, 5000);
    };

    copyToClipboard(link.textContent.trim())
        .then(() => announce(labels.get('copied')))
        .catch(error => {
            // A refused clipboard used to leave the button silent: the visitor
            // walked away believing the link was theirs.
            console.error(error);
            announce(labels.get('copy-failed'));
        })
    ;
}

async function submitPaste(form, labels, maxContentBytes) {
    const textarea = form.querySelector('textarea[name="paste"]');
    if (!textarea) {
        return;
    }

    // One instance per paste: reusing the key would let the link of the previous
    // paste decrypt this one.
    const crypto = new Crypto();
    const originalContent = textarea.value;

    hideError();
    hide(document.getElementById('link'));

    try {
        let encryptedValue;
        try {
            encryptedValue = await crypto.encrypt(originalContent);
        } catch (error) {
            // Nothing has left the browser at this point, which is worth saying:
            // the visitor still holds the only copy of their text.
            showError(labels.get('encryption-failed'));
            console.error(error);
            return;
        }

        const payload = encryptedValue.join(',');

        // The server caps the body and would answer 413. Checking here spares
        // the upload of a megabyte that has no chance of being accepted, which
        // matters on a phone.
        if (maxContentBytes > 0 && payload.length > maxContentBytes) {
            showError(labels.get('too-large'));
            return;
        }

        textarea.value = payload;

        const body = new FormData(form);
        // Marks the submission as coming from the encrypting path. The server
        // rejects the ones without it, so that a form somehow submitted without
        // this file cannot store clear text.
        body.set('encrypted', '1');

        let response;
        try {
            response = await fetch(form.action, {
                method: 'POST',
                headers: new Headers(),
                body: body,
            });
        } catch (error) {
            // fetch only rejects on a transport failure: offline, DNS, a
            // connection dropped mid-flight. Retrying is the right advice here,
            // and it is not the same advice as for a rejected paste.
            showError(labels.get('network-error'));
            console.error(error);
            return;
        }

        // Without this the rejected paste used to produce a "/p/undefined" link.
        if (!response.ok) {
            showError(submitError(response, labels));
            return;
        }

        showLink(form, await response.text(), crypto.getFragment());
    } catch (error) {
        console.error(error);
        showError(labels.get('save-error'));
    } finally {
        // The clear text goes back into the field whatever happened: on every
        // failing path it is the only copy left.
        textarea.value = originalContent;
        crypto.reset();
    }
}

async function displayPaste(pasteContent, paste) {
    if ('' === paste.encryptedValue) {
        throw new CryptoError(CryptoFailure.WrongKey);
    }

    const crypto = new Crypto();
    const decryptedValue = await crypto.decrypt(paste.encryptedValue.split(','));

    if (!paste.isMarkdown) {
        // textContent rather than an entity encoded innerHTML: the browser
        // escapes it natively, and a megabyte of text no longer has to be
        // rewritten as six megabytes of numeric entities first.
        pasteContent.textContent = decryptedValue;

        if (paste.isCode) {
            highlight(pasteContent);
        }

        return;
    }

    if ('undefined' === typeof showdown) {
        // The converter failed to load: the text is still readable as is, which
        // beats an empty page.
        pasteContent.textContent = decryptedValue;
        return;
    }

    const converter = new showdown.Converter();
    // Showdown lets the HTML of the paste through: sanitize before it reaches
    // the document.
    pasteContent.innerHTML = sanitizeHtml(converter.makeHtml(decryptedValue));
    pasteContent.classList.add('markdown');
    pasteContent.querySelectorAll('pre code').forEach(highlight);
}

function displayPasteInformation(paste) {
    const pasteInformation = document.querySelector('.paste-information');
    if (!pasteInformation) {
        return;
    }

    const availableRead = paste.nbOfReadAllowed - paste.nbOfRead;
    // The template renders RFC 3339, which Date parses as is.
    const expireDate = new Date(paste.expireTime);
    const currentDate = new Date();

    if (expireDate < currentDate || 0 === availableRead) {
        pasteInformation.textContent = pasteInformation.getAttribute('data-last-read');
        show(pasteInformation);
        return;
    }

    let message = 0 === paste.nbOfReadAllowed
        ? pasteInformation.getAttribute('data-information-no-nb-read')
        : pasteInformation.getAttribute('data-information').replace('§nbAvailableRead§', availableRead);

    // Intl, so the date reads the way the visitor expects rather than the way
    // the server would have written it.
    message = message.replace('§dateAvailable§', expireDate.toLocaleDateString());
    message = message.replace('§hourAvailable§', expireDate.toLocaleTimeString());

    pasteInformation.textContent = message;
    show(pasteInformation);
}

// The paste reaches the page through data-* attributes rather than an inline
// script, which is what allows a Content-Security-Policy without
// 'unsafe-inline'.
function readPasteData(pasteContent) {
    const pasteType = pasteContent.getAttribute('data-type');

    return {
        encryptedValue: pasteContent.getAttribute('data-encrypted-value') || '',
        nbOfReadAllowed: Number(pasteContent.getAttribute('data-nb-of-read-allowed')),
        nbOfRead: Number(pasteContent.getAttribute('data-nb-of-read')),
        expireTime: pasteContent.getAttribute('data-expire-time'),
        isCode: 'code' === pasteType,
        isMarkdown: 'markdown' === pasteType,
    };
}

boot();
