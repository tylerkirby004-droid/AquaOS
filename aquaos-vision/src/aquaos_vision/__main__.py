"""Minimal health process; model/MQTT wiring is deployment supplied."""

from http.server import BaseHTTPRequestHandler, HTTPServer
import json
import logging
import os


class HealthHandler(BaseHTTPRequestHandler):
    """Expose explicit degraded health when no model is configured."""

    def do_GET(self):  # noqa: N802 - required by BaseHTTPRequestHandler
        if self.path != "/health/ready":
            self.send_response(404)
            self.end_headers()
            return
        payload = json.dumps({"ready": False, "state": "model_unavailable"}).encode()
        self.send_response(503)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def log_message(self, message, *args):
        logging.info(json.dumps({"message": message % args}))


logging.basicConfig(level=logging.INFO, format="%(message)s")
HTTPServer((os.getenv("AQUAOS_VISION_HEALTH_HOST", "127.0.0.1"),
            int(os.getenv("AQUAOS_VISION_HEALTH_PORT", "8091"))), HealthHandler).serve_forever()
