from fastapi.testclient import TestClient
from server.main import app

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

    # Check if original text is preserved
    assert data["original"] == test_text

    # Check if sarcastic version is different from original
    # and contains alternating case (a basic check)
    assert data["sarcastic"] != test_text
    assert any(c.isupper() for c in data["sarcastic"])
    assert any(c.islower() for c in data["sarcastic"])


def test_get_sarcastic_text_empty():
    # Test with empty text
    response = client.get("/sarcastic/")

    # Should return 404 as path parameter is required
    assert response.status_code == 404
