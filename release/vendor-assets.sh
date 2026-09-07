#!/usr/bin/env bash
#
# Refresh the third party javascript shipped in server/assets/. Those files are
# committed, so nothing signals a new CVE on its own: package.json declares the
# versions for the dependency scanners, and this script is what applies them.
#
# Usage: release/vendor-assets.sh
set -euo pipefail

rootDirectory="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

readVersion() {
    node -e "process.stdout.write(require('${rootDirectory}/package.json').dependencies['$1'])"
}

highlightVersion="$(readVersion highlight.js)"
showdownVersion="$(readVersion showdown)"

download() {
    local url="$1"
    local destination="$2"

    echo "${url}"
    curl -sfS -o "${rootDirectory}/${destination}" "${url}"
    echo "  -> ${destination} ($(sha256sum "${rootDirectory}/${destination}" | cut -d' ' -f1))"
}

download \
    "https://cdnjs.cloudflare.com/ajax/libs/highlight.js/${highlightVersion}/highlight.min.js" \
    "server/assets/js/highlight.min.js"

download \
    "https://cdnjs.cloudflare.com/ajax/libs/highlight.js/${highlightVersion}/styles/androidstudio.min.css" \
    "server/assets/css/highlightjs/androidstudio.css"

download \
    "https://cdnjs.cloudflare.com/ajax/libs/showdown/${showdownVersion}/showdown.min.js" \
    "server/assets/js/showdown.min.js"

echo
echo "The syntax highlighting theme comes from the same release as the bundle:"
echo "check a code paste and a markdown paste in a browser before committing."
