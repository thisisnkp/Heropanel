#!/usr/bin/env python3
# A minimal HTTPS smart-git server: HTTP Basic auth over TLS, bridging to
# `git http-backend`. It exists only so the e2e can exercise HeroPanel's
# token/HTTPS clone path against a real server with a real (private-CA)
# certificate — the same shape as cloning a private GitHub repo with a PAT.
#
# Config comes from the environment:
#   GIT_PROJECT_ROOT  directory holding the bare repo(s)
#   GIT_USER/GIT_TOKEN the single credential accepted (Basic user:token)
#   TLS_CERT/TLS_KEY   PEM cert + key
#   PORT               listen port (default 9443)
import base64
import os
import ssl
import subprocess
from http.server import BaseHTTPRequestHandler, HTTPServer

ROOT = os.environ["GIT_PROJECT_ROOT"]
USER = os.environ["GIT_USER"]
TOKEN = os.environ["GIT_TOKEN"]
CERT = os.environ["TLS_CERT"]
KEY = os.environ["TLS_KEY"]
PORT = int(os.environ.get("PORT", "9443"))


class Handler(BaseHTTPRequestHandler):
    def _authed(self):
        h = self.headers.get("Authorization", "")
        if not h.startswith("Basic "):
            return False
        try:
            user, _, pw = base64.b64decode(h[6:]).decode().partition(":")
        except Exception:
            return False
        return user == USER and pw == TOKEN

    def _deny(self):
        self.send_response(401)
        self.send_header("WWW-Authenticate", 'Basic realm="git"')
        self.send_header("Content-Length", "0")
        self.end_headers()

    def _serve(self, method):
        if not self._authed():
            return self._deny()
        path, _, query = self.path.partition("?")
        env = dict(os.environ)
        env.update({
            "GIT_HTTP_EXPORT_ALL": "1",
            "GIT_PROJECT_ROOT": ROOT,
            "PATH_INFO": path,
            "QUERY_STRING": query,
            "REQUEST_METHOD": method,
            "REMOTE_USER": USER,
            "CONTENT_TYPE": self.headers.get("Content-Type", ""),
        })
        body = b""
        n = int(self.headers.get("Content-Length", 0) or 0)
        if n:
            body = self.rfile.read(n)
            env["CONTENT_LENGTH"] = str(n)
        proc = subprocess.run(["git", "http-backend"], input=body,
                              capture_output=True, env=env)
        out = proc.stdout
        head, sep, payload = out.partition(b"\r\n\r\n")
        if not sep:
            head, _, payload = out.partition(b"\n\n")
        status = 200
        headers = []
        for line in head.split(b"\n"):
            line = line.strip()
            if not line:
                continue
            k, _, v = line.partition(b":")
            key, val = k.strip().decode(), v.strip().decode()
            if key.lower() == "status":
                status = int(val.split()[0])
            else:
                headers.append((key, val))
        self.send_response(status)
        for k, v in headers:
            self.send_header(k, v)
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def do_GET(self):
        self._serve("GET")

    def do_POST(self):
        self._serve("POST")

    def log_message(self, *args):
        pass


ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
ctx.load_cert_chain(CERT, KEY)
srv = HTTPServer(("0.0.0.0", PORT), Handler)
srv.socket = ctx.wrap_socket(srv.socket, server_side=True)
srv.serve_forever()
