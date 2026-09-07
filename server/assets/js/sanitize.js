// Showdown hands back the raw HTML found in the markdown, so its output cannot
// be assigned to innerHTML as is: "<img src=x onerror=...>" in a paste would run
// on the origin of the service. This is an allowlist sanitizer, kept dependency
// free on purpose; the Content-Security-Policy is the second line of defence,
// not the first.
const allowedElements = new Set([
    'a', 'b', 'blockquote', 'br', 'code', 'dd', 'del', 'dl', 'dt', 'em',
    'h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'hr', 'i', 'img', 'ins', 'kbd', 'li',
    'ol', 'p', 'pre', 's', 'samp', 'small', 'span', 'strong', 'sub', 'sup',
    'table', 'tbody', 'td', 'tfoot', 'th', 'thead', 'tr', 'ul', 'var',
]);

// Elements whose text content is not worth keeping: unwrapping a <script> would
// paste its source as visible text.
const droppedElements = new Set([
    'script', 'style', 'iframe', 'object', 'embed', 'template', 'noscript',
    'svg', 'math', 'form', 'input', 'button', 'select', 'textarea', 'link',
    'meta', 'base',
]);

const allowedAttributes = {
    '*': ['class', 'title'],
    'a': ['href'],
    'img': ['src', 'alt', 'width', 'height'],
    'td': ['colspan', 'rowspan'],
    'th': ['colspan', 'rowspan'],
    'ol': ['start'],
};

const allowedUrlSchemes = ['http', 'https', 'mailto'];

// isSafeUrl only lets a relative url or an explicitly allowed scheme through.
// Anything carrying a colon before the path counts as a scheme, whatever the
// padding: control characters, and the U+FFFD the html parser substitutes for a
// NUL byte.
function isSafeUrl(value) {
    const normalized = value.replace(/[\u0000-\u0020\ufffd]/g, '').toLowerCase();
    const colonIndex = normalized.indexOf(':');

    if (colonIndex < 0) {
        return true;
    }

    // "a/b:c" is a path, not a scheme.
    const pathIndex = normalized.search(/[/?#]/);
    if (pathIndex >= 0 && pathIndex < colonIndex) {
        return true;
    }

    return allowedUrlSchemes.includes(normalized.slice(0, colonIndex));
}

function isAllowedAttribute(tagName, attributeName) {
    if (attributeName.startsWith('on')) {
        return false;
    }

    const allowed = (allowedAttributes['*'] || []).concat(allowedAttributes[tagName] || []);

    return allowed.includes(attributeName);
}

function sanitizeElement(element) {
    const tagName = element.tagName.toLowerCase();

    if (droppedElements.has(tagName)) {
        element.remove();
        return;
    }

    // Sanitize the children first: unwrapping the element moves them up, and
    // they have to be clean by then.
    Array.from(element.children).forEach(sanitizeElement);

    if (!allowedElements.has(tagName)) {
        element.replaceWith(...element.childNodes);
        return;
    }

    Array.from(element.attributes).forEach(attribute => {
        const attributeName = attribute.name.toLowerCase();

        if (!isAllowedAttribute(tagName, attributeName)) {
            element.removeAttribute(attribute.name);
            return;
        }

        if (('href' === attributeName || 'src' === attributeName) && !isSafeUrl(attribute.value)) {
            element.removeAttribute(attribute.name);
        }
    });
}

// sanitizeHtml parses the markup in an inert document: no request is issued and
// no script runs while it is being cleaned.
export default function sanitizeHtml(html) {
    const parsed = new DOMParser().parseFromString(html, 'text/html');

    Array.from(parsed.body.children).forEach(sanitizeElement);

    return parsed.body.innerHTML;
}
