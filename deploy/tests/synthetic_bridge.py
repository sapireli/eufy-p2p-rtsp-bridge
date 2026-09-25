#!/usr/bin/env python3
"""Minimal bridge API fixture for the packaged client's local RTSP trial."""

import json
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        responses = {
            "/healthz": {"ok": True, "auth": {"state": "ok"}, "go2rtc": "running"},
            "/api/cameras": [{"sn": "SYNTHETIC1", "name": "Moving pattern", "codec": "h264",
                              "mode": "always", "powered": True, "streamKey": "test"}],
        }
        if self.path not in responses:
            self.send_error(404)
            return
        body = json.dumps(responses[self.path]).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *_args):
        pass


if __name__ == "__main__":
    ThreadingHTTPServer(("127.0.0.1", int(sys.argv[1])), Handler).serve_forever()
