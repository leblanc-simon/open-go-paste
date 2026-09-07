import Crypto from './crypto.js';
import sanitizeHtml from './sanitize.js';

function encode(e){return e.replace(/[^]/g,function(e){return"&#"+e.charCodeAt(0)+";"})}

// highlight.js renamed highlightBlock to highlightElement in 10.7, and the
// bundle shipped here is still a 9.x.
function highlight(element) {
    if ('function' === typeof hljs.highlightElement) {
        hljs.highlightElement(element);
        return;
    }

    hljs.highlightBlock(element);
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

function showError(form, message) {
    const errorElement = document.getElementById('paste-error');
    if (!errorElement) {
        return;
    }

    errorElement.textContent = message || errorElement.getAttribute('data-message');
    errorElement.classList.add('show');
}

function hideError() {
    const errorElement = document.getElementById('paste-error');
    if (errorElement) {
        errorElement.classList.remove('show');
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
    linkDiv.classList.add('show');
}

async function submitPaste(form) {
    const textarea = form.querySelector('textarea[name="paste"]');
    if (!textarea) {
        return;
    }

    // One instance per paste: reusing the key would let the link of the previous
    // paste decrypt this one.
    const crypto = new Crypto();
    const originalContent = textarea.value;

    hideError();

    try {
        const encryptedValue = await crypto.encrypt(originalContent);
        textarea.value = encryptedValue.join(',');

        const response = await fetch(form.action, {
            method: 'POST',
            headers: new Headers(),
            body: new FormData(form),
        });

        textarea.value = originalContent;

        // Without this the rejected paste used to produce a "/p/undefined" link.
        if (!response.ok) {
            showError(form);
            return;
        }

        showLink(form, await response.text(), crypto.getFragment());
    } catch (error) {
        textarea.value = originalContent;
        console.error(error);
        showError(form);
    } finally {
        crypto.reset();
    }
}

function initForm(form) {
    form.addEventListener('submit', event => {
        event.preventDefault();
        submitPaste(form);

        return false;
    }, false);

    const copyLink = document.getElementById('copy-link');
    if (!copyLink) {
        return;
    }

    copyLink.addEventListener('click', event => {
        const copyLinkElement = event.currentTarget;
        const link = document.querySelector('#link .link a');
        if (!link) {
            return;
        }

        copyToClipboard(link.textContent.trim())
            .then(() => {
                copyLinkElement.classList.add('copied');
                setTimeout(() => {
                    copyLinkElement.classList.remove('copied');
                }, 3000);
            })
            .catch(error => {
                console.error(error);
            })
        ;
    });
}

async function displayPaste(pasteContent, paste) {
    const crypto = new Crypto();
    const decryptedValue = await crypto.decrypt(paste.encryptedValue.split(','));

    if (!paste.isMarkdown) {
        pasteContent.innerHTML = encode(decryptedValue);

        if (paste.isCode) {
            highlight(pasteContent);
        }

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
        return;
    }

    let message = 0 === paste.nbOfReadAllowed
        ? pasteInformation.getAttribute('data-information-no-nb-read')
        : pasteInformation.getAttribute('data-information').replace('§nbAvailableRead§', availableRead);

    message = message.replace('§dateAvailable§', expireDate.toLocaleDateString());
    message = message.replace('§hourAvailable§', expireDate.toLocaleTimeString());

    pasteInformation.textContent = message;
}

// The paste reaches the page through data-* attributes rather than an inline
// script, which is what allows a Content-Security-Policy without
// 'unsafe-inline'.
function readPasteData(pasteContent) {
    const pasteType = pasteContent.getAttribute('data-type');

    return {
        encryptedValue: pasteContent.getAttribute('data-encrypted-value'),
        nbOfReadAllowed: Number(pasteContent.getAttribute('data-nb-of-read-allowed')),
        nbOfRead: Number(pasteContent.getAttribute('data-nb-of-read')),
        expireTime: pasteContent.getAttribute('data-expire-time'),
        isCode: 'code' === pasteType,
        isMarkdown: 'markdown' === pasteType,
    };
}

(() => {
    const form = document.querySelector('form[name="paste"]');
    if (form) {
        initForm(form);
    }

    const pasteContent = document.getElementById('paste-content');
    if (!pasteContent) {
        return;
    }

    const paste = readPasteData(pasteContent);
    if (!paste.encryptedValue) {
        return;
    }

    displayPaste(pasteContent, paste).catch(error => {
        console.error(error);
    });

    displayPasteInformation(paste);
})();
