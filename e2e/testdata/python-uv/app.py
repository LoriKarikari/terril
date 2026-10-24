from fastapi import FastAPI

api = FastAPI()


@api.get("/")
def root():
    return "hello from uv"
