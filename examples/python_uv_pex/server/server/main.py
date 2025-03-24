from fastapi import FastAPI
from format.make_sarcastic import make_sarcastic

app = FastAPI()


@app.get("/sarcastic/{text}")
async def get_sarcastic_text(text: str):
    return {"original": text, "sarcastic": make_sarcastic(text)}
