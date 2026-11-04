"""Serve the app's page in a browser, with a stand-in for pywebview's API (stub.js).

    python3 app/preview/serve.py [port]

Then open http://127.0.0.1:8766/?s=<scenario> (listed at the top of stub.js). The page is
rebuilt from app/index.html and stub.js on every load, so a reload shows the latest edit.

What each result screen says comes from the app's own camera.summarize, run on the
fixtures in app/tests/fixtures, which the Go tests make from the real tool. Nothing here
ships in the app; it's a development tool on localhost only.
"""

import importlib
import json
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

HERE = Path(__file__).resolve().parent
APP_DIR = HERE.parent
FIXTURES = APP_DIR / "tests" / "fixtures"
sys.path.insert(0, str(APP_DIR))

import camera  # noqa: E402  (pure: runs nothing until asked)
import version  # noqa: E402

MARKER = "<script>\n'use strict';"

RESULTS = {
    "r8": "read-r8.json", "untested": "read-r6.json", "not-supported": "read-90d.json",
    "busy": "read-busy.json", "wrong-mode": "read-wrong-usb-mode.json", "no-camera": "read-no-camera.json",
    "no-answer": "read-no-answer.json",
}


def app_facts():
    importlib.reload(camera)
    importlib.reload(version)
    results = {name: camera.summarize(json.loads((FIXTURES / file).read_text(encoding="utf-8")))
               for name, file in RESULTS.items()}
    return {"version": version.VERSION, "results": results}


def build():
    page = (APP_DIR / "index.html").read_text(encoding="utf-8")
    if MARKER not in page:
        raise SystemExit("index.html's script no longer starts with 'use strict'; update MARKER in serve.py")
    stub = (HERE / "stub.js").read_text(encoding="utf-8")
    facts = json.dumps(app_facts())
    return page.replace(MARKER, f"<script>\nwindow.__app = {facts};\n{stub}\n</script>\n{MARKER}", 1)


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path.split("?")[0] not in ("/", "/index.html"):
            self.send_error(404)
            return
        body = build().encode("utf-8")
        self.send_response(200)
        self.send_header("Content-Type", "text/html; charset=utf-8")
        self.send_header("Cache-Control", "no-store")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *args):
        pass


if __name__ == "__main__":
    port = int(sys.argv[1]) if len(sys.argv) > 1 else 8766
    build()  # fail now, not on the first load
    print(f"Serving the page at http://127.0.0.1:{port}/", flush=True)
    ThreadingHTTPServer(("127.0.0.1", port), Handler).serve_forever()
