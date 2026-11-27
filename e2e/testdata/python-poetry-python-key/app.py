import sys

from flask import Flask

app = Flask(__name__)


@app.get("/")
def index():
    return f"hello from python {sys.version_info.major}.{sys.version_info.minor}"
