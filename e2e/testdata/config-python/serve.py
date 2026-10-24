import os
import subprocess
from http.server import BaseHTTPRequestHandler, HTTPServer

version = subprocess.run(["jq", "--version"], capture_output=True, text=True).stdout.strip()


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        self.end_headers()
        self.wfile.write(f"python with {version}".encode())


HTTPServer(("", int(os.environ["PORT"])), Handler).serve_forever()
