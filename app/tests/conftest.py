import json
import sys
from pathlib import Path

import pytest

APP = Path(__file__).resolve().parent.parent
FIXTURES = Path(__file__).resolve().parent / "fixtures"
sys.path.insert(0, str(APP))

# The serial number on the scripted cameras' bodies. The read fixtures carry it, as `read
# --serial` prints it, because the result screen shows it; the log and the issue mustn't.
BODY_SERIAL = "072022001307"
# What tool/internal/cmd/fixtures_test.go's scripted cameras say that must never reach a log.
SECRETS = ["0123456789abcdef0123456789abcdef", BODY_SERIAL, "USBSERIAL0001", "Jane Doe"]


@pytest.fixture
def fixture():
    """Loads one of the files tool/internal/cmd/fixtures_test.go makes from the Go code."""
    def load(name):
        text = (FIXTURES / name).read_text(encoding="utf-8")
        return text if name.startswith("trace-") else json.loads(text)
    return load
