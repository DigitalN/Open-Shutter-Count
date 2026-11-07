"""Talking to the camera, through the openshuttercount tool bundled with the app.

`list` only asks macOS which USB devices are attached: nothing is sent to the camera,
so it's safe to run every second while waiting. `read` reads the camera and says what
it did. Both print JSON (see tool/internal/cmd in the repository); `read --trace` also saves
everything exchanged with the camera, minus its serial number. `read --serial` adds the serial
number on the body, which only the result screen shows: the log and the issue leave it out.
`read --tested-list` gives it the list of tested cameras fetched at launch (tested_list.py).

What the tool says is turned into what the result screen says by `summarize`.
"""

import json
import subprocess
import sys
import tempfile
from pathlib import Path

LIST_TIMEOUT = 10  # seconds
# A camera that stops answering times out in the tool well before this; it's for one
# that hangs the tool itself.
READ_TIMEOUT = 120

# How the tool names the kinds of failure (tool/internal/cmd/read.go).
NO_CAMERA = "no_camera"
SEVERAL_CAMERAS = "several_cameras"
WRONG_USB_MODE = "wrong_usb_mode"
BUSY = "busy"
NOT_SUPPORTED = "not_supported"
NO_ANSWER = "no_answer"
FAILED = "failed"

USB_MODE_SETTING = ("MENU > Setup (wrench) > Choose USB connection app > Photo Import/Remote Control. "
                    "On the Canon R8 it's on page 4 of the Setup menu.")
NO_CAMERA_HELP = (
    "- Switch the camera on, with a card in it.\n"
    f"- Set its USB connection to Photo Import/Remote Control: {USB_MODE_SETTING}\n"
    "- Turn its Wi-Fi and Bluetooth off.\n"
    "- Plug it straight into the Mac with a data cable, not through a hub or dock.\n"
    "- If macOS asks whether to allow the accessory, click Allow.")
RESTART_CAMERA = ("Switch the camera off and on again (take the battery out if it won't switch off), "
                  "then click Read Again.")
OPEN_AN_ISSUE = "Please open an issue, so it can be worked out."
READ_IT = "Success! We've read your shutter count."


class ToolError(Exception):
    pass


def tool_path():
    """The bundled tool: beside the app's executable, or tool/mac/ when running from source."""
    if getattr(sys, "frozen", False):
        return Path(sys.executable).parent / "openshuttercount"
    return Path(__file__).resolve().parent.parent / "tool" / "mac" / "openshuttercount"


class Camera:
    """Runs the tool. `run` is subprocess.run, replaceable in tests. `tested_list` is the
    tested_list.TestedList fetched at launch, if any: a read waits for it, and passes it
    with --tested-list if it came."""

    def __init__(self, tool, run=subprocess.run, note="", tested_list=None):
        self._tool = str(tool)
        self._run = run
        self._note = note
        self._tested_list = tested_list

    def list(self, everything=False):
        """The cameras attached, as the tool lists them; with `everything`, every USB device."""
        arguments = [self._tool, "list", "--json"] + (["--all"] if everything else [])
        try:
            result = self._run(arguments, capture_output=True, text=True, timeout=LIST_TIMEOUT)
        except (OSError, subprocess.TimeoutExpired) as e:
            raise ToolError(f"The openshuttercount tool didn't run: {e}") from None
        if result.returncode != 0:
            raise ToolError((result.stdout + result.stderr).strip() or f"exit status {result.returncode}")
        try:
            devices = json.loads(result.stdout or "null") or []
        except ValueError:
            raise ToolError(f"The tool's device list wasn't JSON: {result.stdout[:200]!r}") from None
        if not isinstance(devices, list):
            raise ToolError("The tool's device list wasn't a list.")
        return [d for d in devices if isinstance(d, dict)]

    def read(self):
        """Reads the camera. Returns (what the tool printed, parsed; its messages; the trace).

        The trace, and the tested list if one was fetched, go in a temporary folder that's
        deleted straight after."""
        fetched = self._tested_list.wait() if self._tested_list is not None else None
        with tempfile.TemporaryDirectory(prefix="openshuttercount-") as folder:
            trace = Path(folder) / "trace.json"
            arguments = [self._tool, "read", "--json", "--raw", "--serial", "--trace", str(trace)]
            if fetched is not None and fetched.text is not None:
                listed = Path(folder) / "tested.json"
                try:
                    listed.write_text(fetched.text, encoding="utf-8")
                    arguments += ["--tested-list", str(listed)]
                except OSError:
                    pass  # the tool's built-in list, then
            if self._note:
                arguments += ["--note", self._note]
            try:
                result = self._run(arguments, capture_output=True, text=True, timeout=READ_TIMEOUT)
            except subprocess.TimeoutExpired as e:
                output = failure(NO_ANSWER, f"The camera didn't finish answering in {READ_TIMEOUT} seconds.",
                                 RESTART_CAMERA)
                return output, _text(e.stderr), _read_text(trace)
            except OSError as e:
                return failure(FAILED, f"The openshuttercount tool didn't run: {e}"), "", ""
            return parse_read(result.stdout, result.stderr, result.returncode), result.stderr, _read_text(trace)


def failure(kind, message, hint=None):
    return {"error": {"kind": kind, "message": message, **({"hint": hint} if hint else {})}}


def parse_read(stdout, stderr, returncode):
    """What `read --json` printed, or a failure saying what went wrong if it printed no JSON."""
    try:
        output = json.loads(stdout)
    except (TypeError, ValueError):
        output = None
    if not isinstance(output, dict):
        said = (stdout or "").strip() or (stderr or "").strip() or f"exit status {returncode}"
        return failure(FAILED, said)
    if returncode != 0 and "error" not in output:
        output["error"] = {"kind": FAILED, "message": (stderr or "").strip() or f"exit status {returncode}"}
    return output


def _text(value):
    if isinstance(value, bytes):
        return value.decode("utf-8", "replace")
    return value or ""


def _read_text(path):
    try:
        return Path(path).read_text(encoding="utf-8")
    except OSError:
        return ""


# ── what the result screen says ─────────────────────────────

def count_text(counter):
    """'1,001 to 2,000' for a range, '26,374' for an exact count."""
    if counter.get("error"):
        return "couldn't be read"
    value = int(counter.get("value") or 0)
    low = int(counter.get("min") or 0)
    if counter.get("range") and low != value:
        return f"{low:,} to {value:,}"
    return f"{value:,}"


def date_text(when):
    """'November 5, 2026'."""
    return f"{when:%B} {when.day}, {when.year}" if when else ""


def plain_name(model):
    """The name people call a Canon by: "Canon R8" for the EOS R8, which the camera calls
    "Canon EOS R8"."""
    name = model[len("Canon "):] if model.startswith("Canon ") else model
    name = name[len("EOS "):] if name.startswith("EOS ") else name
    return f"Canon {name}" if name else ""


def camera_name(output, devices=()):
    """The camera's model, as plainly as it's known."""
    reading = output.get("reading") or {}
    camera = output.get("camera") or {}
    model = reading.get("model") or camera.get("model") or ""
    if model:
        return plain_name(model)
    for device in list(devices) + [output.get("device") or {}]:
        name = device.get("name") or device.get("product")
        if name and name not in ("Canon camera", "Canon Digital Camera", "unrecognised device"):
            return plain_name(name)
    return ""


def unwrap(text):
    """The tool's hints are wrapped for a terminal. This joins each paragraph's lines back
    up, keeping list items ("- ...") on lines of their own."""
    paragraphs = []
    for block in text.strip().split("\n\n"):
        lines = []
        for line in block.splitlines():
            line = line.strip()
            if lines and line and not line.startswith("-"):
                lines[-1] += " " + line
            elif line:
                lines.append(line)
        paragraphs.append("\n".join(lines))
    return "\n\n".join(p for p in paragraphs if p)


def summarize(output, devices=(), read_at=None):
    """What the result screen shows for what `read --json` printed, read at `read_at`.

    ok says whether it read; tested is only true for a reading the tool marks confirmed,
    which it does when the tested list has the camera, read the way it was read. Any other
    reading says it's not fully tested: the counts aren't proven to be what their names
    say.

    A reading shows the camera's model, the serial number on its body (where the camera
    reported it) and the date above the counts, for a screenshot to go with a sale. The
    serial number is only for the screen: the log and the issue don't use it."""
    model = camera_name(output, devices)
    read_on = date_text(read_at)
    the_camera = f"the {model}" if model else "the camera"
    error = output.get("error")

    if not error:
        reading = output.get("reading") or {}
        tested = bool(reading.get("confirmed"))
        return {
            "ok": True,
            "tested": tested,
            "model": model,
            "serial": reading.get("serial_number") or "",
            "read_on": read_on,
            "headline": READ_IT,
            "detail": "" if tested else
                      f"Not fully tested on {the_camera}; if these numbers look wrong, please open an issue.",
            "counters": [{"label": c.get("label", ""), "text": count_text(c)} for c in output.get("counters") or []],
            "hint": "",
            "kind": "",
        }

    kind = error.get("kind") or FAILED
    message = error.get("message") or ""
    hint = unwrap(error.get("hint") or "")
    headline, detail = {
        NO_CAMERA: ("No camera found",
                    "Only Canon cameras are looked for. If yours is another make, it can't be read yet: "
                    "please open an issue."),
        SEVERAL_CAMERAS: ("More than one camera is plugged in", "Unplug all but one, then click Read Again."),
        WRONG_USB_MODE: ("Change the camera's USB connection setting",
                         f"Change it to Photo Import/Remote Control: {USB_MODE_SETTING} "
                         "Then unplug the camera, plug it back in, and click Read Again."),
        BUSY: ("Another app is using the camera", ""),
        NOT_SUPPORTED: (f"Can't read {the_camera} yet", OPEN_AN_ISSUE),
        NO_ANSWER: ("The camera stopped answering", RESTART_CAMERA),
    }.get(kind, (f"Couldn't read {the_camera}", OPEN_AN_ISSUE))

    if kind == WRONG_USB_MODE:
        hint = ""  # the detail names the setting
    elif kind == NO_CAMERA:
        hint, message = NO_CAMERA_HELP, ""  # the tool's are for its command line
    return {
        "ok": False,
        "tested": False,
        "model": model,
        "serial": "",
        "read_on": read_on,
        "headline": headline,
        "detail": detail,
        "counters": [],
        "hint": hint,
        "message": message,
        "kind": kind,
    }
