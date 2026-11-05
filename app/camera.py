"""Talking to the camera, through the openshuttercounter tool bundled with the app.

`list` only asks macOS which USB devices are attached: nothing is sent to the camera,
so it's safe to run every second while waiting. `read` reads the camera and says what
it did. Both print JSON (see internal/cmd in the repository); `read --trace` also saves
everything exchanged with the camera, minus its serial number.

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

# How the tool names the kinds of failure (internal/cmd/read.go).
NO_CAMERA = "no_camera"
SEVERAL_CAMERAS = "several_cameras"
WRONG_USB_MODE = "wrong_usb_mode"
BUSY = "busy"
NOT_SUPPORTED = "not_supported"
NO_ANSWER = "no_answer"
FAILED = "failed"

USB_MODE_SETTING = ("MENU > Setup (wrench) > Choose USB connection app > Photo Import/Remote Control. "
                    "On the EOS R8 it's on page 4 of the Setup menu.")
NO_CAMERA_HELP = (
    "- Switch the camera on, with a card in it.\n"
    f"- Set its USB connection to Photo Import/Remote Control: {USB_MODE_SETTING}\n"
    "- Turn its Wi-Fi and Bluetooth off.\n"
    "- Plug it straight into the Mac with a data cable, not through a hub or dock.\n"
    "- If macOS asks whether to allow the accessory, click Allow.")
RESTART_CAMERA = ("Switch the camera off and on again (take the battery out if it won't switch off), "
                  "then click Read Again.")
SEND_THE_LOG = "Please save the log and open an issue, so it can be worked out."


class ToolError(Exception):
    pass


def tool_path():
    """The bundled tool: beside the app's executable, or mac/ when running from source."""
    if getattr(sys, "frozen", False):
        return Path(sys.executable).parent / "openshuttercounter"
    return Path(__file__).resolve().parent.parent / "mac" / "openshuttercounter"


class Camera:
    """Runs the tool. `run` is subprocess.run, replaceable in tests."""

    def __init__(self, tool, run=subprocess.run, note=""):
        self._tool = str(tool)
        self._run = run
        self._note = note

    def list(self, everything=False):
        """The cameras attached, as the tool lists them; with `everything`, every USB device."""
        arguments = [self._tool, "list", "--json"] + (["--all"] if everything else [])
        try:
            result = self._run(arguments, capture_output=True, text=True, timeout=LIST_TIMEOUT)
        except (OSError, subprocess.TimeoutExpired) as e:
            raise ToolError(f"The openshuttercounter tool didn't run: {e}") from None
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
        """Reads the camera. Returns (what the tool printed, parsed; its messages; the trace)."""
        with tempfile.TemporaryDirectory(prefix="openshuttercount-") as folder:
            trace = Path(folder) / "trace.json"
            arguments = [self._tool, "read", "--json", "--raw", "--trace", str(trace)]
            if self._note:
                arguments += ["--note", self._note]
            try:
                result = self._run(arguments, capture_output=True, text=True, timeout=READ_TIMEOUT)
            except subprocess.TimeoutExpired as e:
                output = failure(NO_ANSWER, f"The camera didn't finish answering in {READ_TIMEOUT} seconds.",
                                 RESTART_CAMERA)
                return output, _text(e.stderr), _read_text(trace)
            except OSError as e:
                return failure(FAILED, f"The openshuttercounter tool didn't run: {e}"), "", ""
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


def camera_name(output, devices=()):
    """The camera's model, as plainly as it's known."""
    reading = output.get("reading") or {}
    camera = output.get("camera") or {}
    model = reading.get("model") or camera.get("model") or ""
    if model.startswith("Canon "):
        model = model[len("Canon "):]
    if model:
        return model
    for device in list(devices) + [output.get("device") or {}]:
        name = device.get("name") or device.get("product")
        if name and name not in ("Canon camera", "Canon Digital Camera", "unrecognised device"):
            return name[len("Canon "):] if name.startswith("Canon ") else name
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


def summarize(output, devices=()):
    """What the result screen shows for what `read --json` printed.

    ok says whether it read; tested is only true for the bodies the tool marks
    confirmed (the EOS R8 and the EOS 5D Mark IV)."""
    model = camera_name(output, devices)
    the_camera = f"the {model}" if model else "the camera"
    error = output.get("error")

    if not error:
        reading = output.get("reading") or {}
        tested = bool(reading.get("confirmed"))
        return {
            "ok": True,
            "tested": tested,
            "model": model,
            "headline": f"Read {the_camera}",
            "detail": (f"Tested on {the_camera}." if tested else
                       f"Not fully tested on {the_camera}; if these numbers look wrong, please send the log."),
            "counters": [{"label": c.get("label", ""), "text": count_text(c), "location": c.get("location", "")}
                         for c in output.get("counters") or []],
            "note": " ".join((reading.get("note") or "").split()),
            "hint": "",
            "kind": "",
        }

    kind = error.get("kind") or FAILED
    message = error.get("message") or ""
    hint = unwrap(error.get("hint") or "")
    headline, detail = {
        NO_CAMERA: ("No camera found",
                    "Only Canon cameras are looked for. If yours is another make, it can't be read yet: "
                    "please save the log and open an issue."),
        SEVERAL_CAMERAS: ("More than one camera is plugged in", "Unplug all but one, then click Read Again."),
        WRONG_USB_MODE: ("Change the camera's USB connection setting",
                         f"Change it to Photo Import/Remote Control: {USB_MODE_SETTING} "
                         "Then unplug the camera, plug it back in, and click Read Again."),
        BUSY: ("Another app is using the camera", ""),
        NOT_SUPPORTED: (f"Can't read {the_camera} yet", SEND_THE_LOG),
        NO_ANSWER: ("The camera stopped answering", RESTART_CAMERA),
    }.get(kind, (f"Couldn't read {the_camera}", SEND_THE_LOG))

    if kind == WRONG_USB_MODE:
        hint = ""  # the detail names the setting
    elif kind == NO_CAMERA:
        hint, message = NO_CAMERA_HELP, ""  # the tool's are for its command line
    return {
        "ok": False,
        "tested": False,
        "model": model,
        "headline": headline,
        "detail": detail,
        "counters": [],
        "note": "",
        "hint": hint,
        "message": message,
        "kind": kind,
    }
