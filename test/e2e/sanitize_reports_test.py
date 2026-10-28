import base64
import io
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
import zipfile

from sanitize_reports import PACKED, main, sanitize_file


class ReportSanitizationTest(unittest.TestCase):
    def test_plain_and_native_embedded_steps_are_redacted(self):
        secret = b"generated-ci-only-password"
        token = b"eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiI3In0.signature"
        archive = io.BytesIO()
        with zipfile.ZipFile(archive, "w") as target:
            target.writestr("report.json", b'{"step":"fill(' + secret + b')","token":"' + token + b'"}')
        html = secret + b'<template id="playwrightReportBase64">data:application/zip;base64,' + base64.b64encode(archive.getvalue()) + b'</template>'
        clean = sanitize_file(html, [secret])
        self.assertNotIn(secret, clean)
        with zipfile.ZipFile(io.BytesIO(base64.b64decode(PACKED.search(clean)[2]))) as result:
            steps = result.read("report.json")
        self.assertNotIn(secret, steps)
        self.assertNotIn(token, steps)
        self.assertIn(b"[REDACTED_TOKEN]", steps)

    def test_unrecognized_embedded_data_fails_closed(self):
        with self.assertRaises(ValueError):
            sanitize_file(b'<template id="playwrightReportBase64">unknown</template>', [])

    def test_rotated_password_is_redacted_by_command_entrypoint(self):
        with tempfile.TemporaryDirectory() as directory:
            report = Path(directory) / "report.json"
            report.write_text('fill(test-only-new-password-mismatch)')
            with patch.dict(os.environ, {"GLEAN_E2E_NEW_PASSWORD": "test-only-new-password"}), patch("sys.argv", ["sanitize_reports.py", directory]):
                main()
            self.assertNotIn("test-only-new-password", report.read_text())


if __name__ == "__main__":
    unittest.main()
