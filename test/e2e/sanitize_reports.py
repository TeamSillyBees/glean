"""Redact ephemeral CI credentials from Playwright text and embedded report ZIPs."""

import base64
import io
import os
from pathlib import Path
import re
import sys
import zipfile


PACKED = re.compile(
    rb'(<template id="playwrightReportBase64">data:application/zip;base64,)'
    rb'([A-Za-z0-9+/=\s]+)(</template>)'
)
JWT = re.compile(rb'eyJ[A-Za-z0-9_-]+\.eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+')
LIMIT = 64 * 1024 * 1024


def redact(data, secrets):
    for value in secrets:
        data = data.replace(value, b"[REDACTED]")
    return JWT.sub(b"[REDACTED_TOKEN]", data)


def sanitize_zip(data, secrets):
    output = io.BytesIO()
    with zipfile.ZipFile(io.BytesIO(data)) as source, zipfile.ZipFile(
        output, "w", compression=zipfile.ZIP_DEFLATED
    ) as target:
        if sum(item.file_size for item in source.infolist()) > LIMIT:
            raise ValueError("report archive exceeds sanitization budget")
        for item in source.infolist():
            body = source.read(item)
            # The native HTML reporter stores JSON only in its embedded ZIP.
            # Reject unexpected binary payloads rather than accidentally publish a trace.
            body.decode("utf-8")
            clean = redact(body, secrets)
            if any(value in clean for value in secrets) or JWT.search(clean):
                raise ValueError("report redaction could not be verified")
            target.writestr(item.filename, clean)
    return output.getvalue()


def sanitize_file(data, secrets):
    if len(data) > LIMIT:
        raise ValueError("report file exceeds sanitization budget")
    if b'id="playwrightReportBase64"' in data and not PACKED.search(data):
        raise ValueError("unrecognized native HTML report archive")

    def replace(match):
        packed = base64.b64decode(re.sub(rb"\s", b"", match[2]), validate=True)
        return match[1] + base64.b64encode(sanitize_zip(packed, secrets)) + match[3]

    clean = redact(PACKED.sub(replace, data), secrets)
    if any(value in clean for value in secrets) or JWT.search(clean):
        raise ValueError("report redaction could not be verified")
    return clean


def main():
    secrets = [
        value.encode()
        for name in ["GLEAN_E2E_PASSWORD", "GLEAN_E2E_NEW_PASSWORD", "INITRA_AUTH_JWT_SECRET"]
        if (value := os.environ.get(name))
    ]
    root = Path(sys.argv[1])
    count = 0
    if root.exists():
        for path in root.rglob("*"):
            if path.is_symlink():
                raise ValueError("report must not contain symlinks")
            if not path.is_file():
                continue
            # Screenshots are unchanged. Inputs use type=password, and no auth
            # storage, response bodies, HAR or trace attachments are generated.
            if path.suffix.lower() == ".png":
                continue
            original = path.read_bytes()
            original.decode("utf-8")
            path.write_bytes(sanitize_file(original, secrets))
            count += 1
    print(f"Sanitized {count} native report text files and embedded ZIP data; no credentials printed.")


if __name__ == "__main__":
    main()
