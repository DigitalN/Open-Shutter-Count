"""The log Save Log writes: what someone working out a camera needs, and nothing that
identifies the camera or its owner.

It holds the app's name and version, the repository, when the camera was read, the Mac's
model and macOS version, which list of tested cameras counted, the camera's model,
firmware and USB IDs, the commands it lists,
each step and what the camera answered, the result, and at the end the whole exchange
with the camera as openshuttercount's trace. The tool leaves the camera's serial number
out of all of it.

Open an Issue fills in a new GitHub issue with the log, all but the camera exchange.

`openshuttercount decode <log>` reads the trace at the end of a saved log.
"""

import platform
import subprocess
import textwrap

from camera import count_text

# Must match tool/pkg/trace's LogBegin and LogEnd, so `decode` finds the trace in a log.
TRACE_BEGIN = "-----BEGIN OPENSHUTTERCOUNT TRACE-----"
TRACE_END = "-----END OPENSHUTTERCOUNT TRACE-----"
# Starts the last part of the log, the camera exchange, which the issue leaves out.
TRACE_HEADING = "The whole exchange with the camera. To read it: openshuttercount decode <this file>"


def mac_info():
    """This Mac's model and macOS version. Nothing that identifies the Mac itself."""
    def run(*arguments):
        try:
            return subprocess.run(arguments, capture_output=True, text=True, timeout=5).stdout.strip()
        except (OSError, subprocess.TimeoutExpired):
            return ""

    return {
        "model": run("/usr/sbin/sysctl", "-n", "hw.model") or platform.machine(),
        "macos": platform.mac_ver()[0] or platform.platform(),
        "build": run("/usr/bin/sw_vers", "-buildVersion"),
        "arch": platform.machine(),
    }


def file_name(read_at):
    return f"Open Shutter Count log {read_at:%Y-%m-%d %H-%M}.txt"


def build_log(*, app_name, version, repository_url, read_at, mac, summary, output, tool_messages="",
              trace="", update_note="", usb_devices=None, tested_list=""):
    """The log, as text. `output` is what `read --json` printed, `summary` what
    camera.summarize made of it, and `trace` the trace file's contents. `tested_list` is
    the line saying which list of tested cameras counted (tested_list.log_line)."""
    lines = [
        f"{app_name} {version}",
        repository_url,
        f"Read on {read_at:%Y-%m-%d at %H:%M:%S %Z (UTC%z)}",
        "",
        f"Mac: {mac.get('model', '')}, macOS {mac.get('macos', '')}"
        + (f" ({mac['build']})" if mac.get("build") else "")
        + (f", {mac['arch']}" if mac.get("arch") else ""),
    ]
    if update_note:
        lines.append(f"Update: {update_note}")
    if tested_list:
        lines.append(tested_list)

    device = output.get("device") or {}
    camera = output.get("camera") or {}
    lines.append("")
    if device or camera:
        name = camera.get("model") or device.get("product") or "unknown"
        lines.append(f"Camera: {name}")
        if camera.get("manufacturer"):
            lines.append(f"  Manufacturer: {camera['manufacturer']}")
        if camera.get("firmware"):
            lines.append(f"  Firmware: {camera['firmware']}")
        if device:
            lines.append(f"  USB: {device.get('vendor_id', '?')}:{device.get('product_id', '?')}"
                         + (f" \"{device['product']}\"" if device.get("product") else ""))
        operations = camera.get("operations")
        if operations is not None:
            lines.append(f"  Commands it lists ({len(operations)}):")
            lines += textwrap.wrap(" ".join(operations), width=78, initial_indent="    ", subsequent_indent="    ")
    else:
        lines.append("Camera: none found")

    if usb_devices is not None:
        lines += ["", "USB devices this Mac sees (vendor:product):"]
        lines += [f"  {d.get('vendor_id', 0):04x}:{d.get('product_id', 0):04x}  {d.get('name', '')}".rstrip()
                  for d in usb_devices] or ["  none"]

    lines += ["", "Steps:"]
    lines += [f"  {step}" for step in output.get("steps") or []] or ["  none"]

    lines += ["", f"Result: {summary.get('headline', '')}"]
    if summary.get("ok"):
        reading = output.get("reading") or {}
        lines.append(f"  {'Tested' if summary.get('tested') else 'Not fully tested'}; read with {reading.get('method', '?')}")
        for counter in output.get("counters") or []:
            where = f"  ({counter['location']})" if counter.get("location") else ""
            lines.append(f"  {counter.get('label', ''):<22} {count_text(counter):>18}{where}")
            if counter.get("raw"):
                lines.append(f"  {'':<22} raw {counter['raw']}")
            if counter.get("error"):
                lines.append(f"  {'':<22} {counter['error']}")
    else:
        error = output.get("error") or {}
        lines.append(f"  Kind: {error.get('kind', '')}")
        lines += _indented("Message", error.get("message", ""))
        lines += _indented("Hint", error.get("hint", ""))

    if tool_messages.strip():
        lines += ["", "What the tool said:"]
        lines += [f"  {line}" for line in tool_messages.strip().splitlines()]

    lines += ["", TRACE_HEADING]
    if trace.strip():
        lines += [TRACE_BEGIN, trace.strip(), TRACE_END]
    else:
        lines.append("  none: nothing was sent to a camera")
    return "\n".join(lines) + "\n"


def _indented(label, text):
    if not text:
        return []
    first, *rest = text.strip().splitlines()
    return [f"  {label}: {first}"] + [f"    {line}" for line in rest]


ISSUE_TEMPLATE = "new-camera.yml"  # .github/ISSUE_TEMPLATE/new-camera.yml; its field ids are used below
MAX_SAID = 1500  # characters of what the app said
# GitHub turns down a new-issue link longer than about 6,000 characters: signed out, it
# sends you to sign in with the whole link inside another, and that one fails. An R8's
# log comes to about 3,500 once it's in the link.
MAX_URL = 5500
CUT = "… cut to fit. Save Log writes all of it."


def readable_log(log):
    """The log without the camera exchange at the end: that's far too long for a link, and
    it's only needed while a camera is being worked out."""
    return log.split(TRACE_HEADING)[0].rstrip()


def issue_url(repository_url, summary, log=""):
    """GitHub's new-issue page with the "New camera" template, filled in: the camera, what
    the app said, and the log without the camera exchange, cut to fit if it has to be.
    Nothing is posted until the person submits it."""
    from urllib.parse import quote, urlencode

    model = summary.get("model") or ""
    said = "\n\n".join(part for part in (summary.get("headline"), summary.get("detail"), summary.get("hint"),
                                         summary.get("message")) if part)
    if len(said) > MAX_SAID:
        said = said[:MAX_SAID - 1] + "…"
    title = f"{model or 'A camera'}: " + ("the numbers look wrong" if summary.get("ok") else summary.get("headline", ""))

    def url(text):
        fields = {"template": ISSUE_TEMPLATE, "title": title, "camera": model, "app_said": said, "log": text}
        return f"{repository_url}/issues/new?{urlencode(fields, quote_via=quote)}"

    lines = readable_log(log).splitlines()
    text = "\n".join(lines)
    while len(url(text)) > MAX_URL and lines:
        lines.pop()
        text = "\n".join(lines + [CUT])
    return url(text)
