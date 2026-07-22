#!/usr/bin/env python3
import json
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        length = int(self.headers.get("Content-Length", "0"))
        payload = json.loads(self.rfile.read(length) or b"{}")
        if self.path == "/api/v1/monitor/agents/enroll":
            self.server.enrollments += 1
            self.send_json(201, {"agent_id": "test-agent", "access_token": "test-session-token", "binding_code": "DM-TEST-2345-CODE", "observed_ip": "127.0.0.1", "fingerprint": "A1B2C3D4", "binding_expires_at": "2026-07-22T13:30:00Z"})
            return
        if self.path == "/api/v1/monitor/agents/heartbeat":
            if self.headers.get("Authorization") != "Bearer test-session-token":
                self.send_json(401, {"error": "invalid session"})
                return
            self.server.heartbeats += 1
            self.send_json(200, {"ok": True})
            return
        if self.path == "/test/stats":
            self.send_json(200, {"enrollments": self.server.enrollments, "heartbeats": self.server.heartbeats})
            return
        self.send_json(404, {"error": "not found"})

    def log_message(self, message, *args):
        sys.stdout.write((message % args) + "\n")
        sys.stdout.flush()

    def send_json(self, status, payload):
        body = json.dumps(payload).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


server = ThreadingHTTPServer(("127.0.0.1", 4188), Handler)
server.enrollments = 0
server.heartbeats = 0
server.serve_forever()
