import Crypto from './crypto.js';

function encode(e){return e.replace(/[^]/g,function(e){return"&#"+e.charCodeAt(0)+";"})}

(() => {
    const crypto = new Crypto();

    const form = document.querySelector('form[name="paste"]');
    const pasteContent = document.getElementById('paste-content');

    if (form) {
        form.addEventListener('submit', (event) => {
            event.preventDefault();
            const textarea = form.querySelector('textarea[name="paste"]');
            if (!textarea) {
                return;
            }

            crypto
                .encrypt(textarea.value)
                .then(encrypted_value => {
                    const originalContent = textarea.value;
                    textarea.value = encrypted_value.join(',');
                    const fetchInit = {
                        method: 'POST',
                        headers : new Headers(),
                        body: new FormData(form)
                    };

                    fetch(form.action, fetchInit).then(response => {
                        textarea.value = originalContent;
                        if (response.ok) {
                            return response.text();
                        }
                        // TODO: gestion des erreurs
                    }).then(text => {
                        const counterPass = crypto.getPasswordAndCounter();
                        const linkDiv = document.getElementById('link');
                        linkDiv.innerHTML = `
                            <a href="/p/${text}#${counterPass}">
                                ${form.action}p/${text}#${counterPass}
                            </a>
                        `;
                        linkDiv.classList.add('show');
                    }).catch(error => {
                        console.error(error);
                    })
                }).catch(error => {
                    console.error(error);
                })
            ;

            return false;
        }, false);
    }

    if (pasteContent && encryptedValue) {
        crypto
            .decrypt(encryptedValue.split(','))
            .then(decryptedValue => {
                if (false === isMarkdown) {
                    pasteContent.innerHTML = encode(decryptedValue);
                    if (true === isCode) {
                        hljs.highlightBlock(pasteContent);
                    }
                } else {
                    const converter = new showdown.Converter();
                    pasteContent.innerHTML = converter.makeHtml(decryptedValue);
                    pasteContent.classList.add('markdown');
                    pasteContent.querySelectorAll('pre code').forEach(block => {
                        hljs.highlightBlock(block);
                    });
                }
            })
            .catch(error => {
                console.error(error);
            })
        ;
    }
})();
