from fastapi import FastAPI
from greeting import greet

app = FastAPI()


@app.get("/")
def root():
    return greet("uv")
