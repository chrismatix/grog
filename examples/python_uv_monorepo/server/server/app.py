from fastapi import FastAPI
from format.make_sarcastic import make_sarcastic
from pb import sarcasm_pb2 as demo_pb2

app = FastAPI()


@app.get("/sarcastic/{text}")
async def get_sarcastic_text(text: str):
    # Use the protobuf messages to mirror the gRPC contract while serving JSON
    request_msg = demo_pb2.SarcasticRequest(text=text)
    response_msg = demo_pb2.SarcasticResponse(
        original=request_msg.text,
        sarcastic=make_sarcastic(request_msg.text),
    )
    return {"original": response_msg.original, "sarcastic": response_msg.sarcastic}
