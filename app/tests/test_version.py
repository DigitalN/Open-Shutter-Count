"""One version, in app/version.py, for the app and the tool inside it."""

import re
from pathlib import Path

from version import VERSION

ROOT = Path(__file__).resolve().parents[2]


def test_the_version_is_three_numbers():
    assert re.fullmatch(r"\d+\.\d+\.\d+", VERSION)


def test_the_committed_tool_is_this_version():
    """tool/mac/build.sh builds it with app/version.py's version; rebuild and commit it after
    raising the version. app/scripts/build.sh checks the same on the Mac."""
    tool = (ROOT / "tool" / "mac" / "openshuttercounter").read_bytes()
    assert VERSION.encode() in tool, "run bash tool/mac/build.sh and commit tool/mac/openshuttercounter"


def test_the_spec_and_the_tool_build_read_it_from_version_py():
    spec = (ROOT / "app" / "OpenShutterCount.spec").read_text(encoding="utf-8")
    assert "'version.py'" in spec and "^VERSION = " in spec
    assert "app/version.py" in (ROOT / "tool" / "mac" / "build.sh").read_text(encoding="utf-8")


def test_the_icon_draws(tmp_path):
    import sys

    sys.path.insert(0, str(ROOT / "app" / "scripts"))
    import make_icon

    image = make_icon.render(32)
    assert len(image) == 32 and len(image[0]) == 32
    corner, middle = image[0][0], image[16][16]
    assert corner[3] < 0.05, "the corners are see-through"
    assert middle[3] > 0.99 and middle[2] > 200, "the opening is blue"
    assert make_icon.png(image).startswith(b"\x89PNG")
