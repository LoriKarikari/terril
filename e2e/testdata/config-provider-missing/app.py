from flask import Flask

server = Flask(__name__)


@server.get("/")
def index():
    return "hello from flask"
