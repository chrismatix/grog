import sys
import os
import pytest

# In your real project you would probably have some packaging setup
sys.path.append(os.path.abspath(os.path.join(os.path.dirname(__file__), '..', 'protobuf')))

from pb.person_pb2 import Person

def test_hello_request_repr():
    req = Person(name="World")
    assert req.name == "World"
    assert "name: \"World\"" in str(req)
