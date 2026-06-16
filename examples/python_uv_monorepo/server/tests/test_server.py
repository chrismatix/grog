from fastapi.testclient import TestClient
from pb import sarcasm_pb2 as demo_pb2
from server.app import app

client = TestClient(app)


def test_get_sarcastic_text():
    # Test with a simple text
    test_text = "hello world"
    response = client.get(f"/sarcastic/{test_text}")

    # Check if response is successful
    assert response.status_code == 200

    # Check response structure
    data = response.json()
    assert "original" in data
    assert "sarcastic" in data

    # Ensure payload aligns with our protobuf contract
    message = demo_pb2.SarcasticResponse(**data)

    # Check if original text is preserved
    assert message.original == test_text

    # Check if sarcastic version is different from original
    # and contains alternating case (a basic check)
    assert message.sarcastic != test_text
    assert any(c.isupper() for c in message.sarcastic)
    assert any(c.islower() for c in message.sarcastic)


def test_get_sarcastic_text_empty():
    # Test with empty text
    response = client.get("/sarcastic/")

    # Should return 404 as path parameter is required
    assert response.status_code == 404
