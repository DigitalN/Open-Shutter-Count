"""The page and the Api it calls agree."""

import re
from pathlib import Path

import main

PAGE = (Path(main.__file__).resolve().parent / "index.html").read_text(encoding="utf-8")


def test_every_call_the_page_makes_is_an_api_method():
    called = set(re.findall(r"call\('(\w+)'", PAGE)) | {"log_error"}
    assert called
    for name in called:
        assert callable(getattr(main.Api, name, None)), name


def test_every_element_the_script_uses_is_on_the_page():
    script = PAGE[PAGE.index("<script>"):]
    used = set(re.findall(r"\$\('([\w-]+)'\)", script))
    used |= {f"s-{step}" for step in ("update", "setup", "waiting", "reading", "result")}
    ids = set(re.findall(r'id="([\w-]+)"', PAGE))
    assert used <= ids, used - ids


def test_the_page_reaches_nothing_on_the_network():
    assert "default-src 'none'" in PAGE
    assert "connect-src 'none'" in PAGE
    assert not re.search(r'(src|href)="https?:', PAGE)
