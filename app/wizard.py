"""The wizard: update, set up the camera, wait for it, read it, show the result.

Every outcome ends on the result screen, including no camera found and a failed read.
The page polls `state()`; the buttons call the other public methods. Each step that
takes time runs on a thread of its own, and a newer step makes an older one's thread
stop (`_generation`).
"""

import logging
import threading
import time
from datetime import datetime

import camera as camera_module
import report
import tested_list as tested_list_module
import updater as updater_module

log = logging.getLogger("openshuttercount")

POLL_INTERVAL = 1.0  # seconds between looks for the camera
HELP_AFTER = 4.0  # seconds without a camera before the waiting screen offers help
GIVE_UP_AFTER = 180.0  # seconds without a camera before it says none was found
SETTLE = 2.0  # seconds to let a camera that's just been plugged in or switched on start up

UPDATE, SETUP, WAITING, READING, RESULT = "update", "setup", "waiting", "reading", "result"


class Wizard:
    """`tested_list` is the tested_list.TestedList the camera reads with, if any: it's
    fetched alongside the update check, and the log says which list counted."""

    def __init__(self, updater, camera, *, app_name, version, repository_url, mac_info=report.mac_info,
                 clock=time.monotonic, sleep=time.sleep, now=lambda: datetime.now().astimezone(),
                 tested_list=None):
        self._updater = updater
        self._camera = camera
        self._tested_list = tested_list
        self._app_name, self._version, self._repository_url = app_name, version, repository_url
        self._mac_info = mac_info
        self._clock, self._sleep, self._now = clock, sleep, now
        self._lock = threading.Lock()
        self._generation = 0
        self._step = UPDATE
        self._update = {"state": "checking", "version": None, "reason": None}
        self._waiting = {"since": None, "found": None}
        self._reading = {"camera": ""}
        self._result = None
        self._log = None  # what Save Log writes, made when the result is
        self._log_name = None

    # ── for the page ──

    def state(self):
        with self._lock:
            waited = self._clock() - self._waiting["since"] if self._waiting["since"] is not None else 0
            return {
                "step": self._step,
                "version": self._version,
                "update": dict(self._update),
                "update_note": self._update_note(),
                "waiting": {"help": waited >= HELP_AFTER, "found": self._waiting["found"]},
                "reading": dict(self._reading),
                "result": dict(self._result) if self._result else None,
                "can_save_log": self._log is not None,
            }

    def start(self):
        """Checks for an update, before anything else, and fetches the tested list alongside
        it on a thread of its own. The update step doesn't wait for the list; a read does."""
        if self._tested_list is not None:
            self._tested_list.start()
        self._spawn(self._check_for_update, name="update-check")

    def setup_done(self):
        with self._lock:
            if self._step != SETUP:
                return
        self._start_waiting()

    def back_to_setup(self):
        with self._lock:
            if self._step != WAITING:
                return
            self._generation += 1
            self._step = SETUP

    def stop_looking(self):
        with self._lock:
            if self._step != WAITING:
                return
            self._generation += 1
            generation = self._generation
        self._spawn(lambda: self._no_camera(generation), name="no-camera")

    def read_again(self):
        with self._lock:
            if self._step != RESULT:
                return
        self._start_waiting()

    def log(self):
        """(suggested file name, text) for Save Log, or None before there's a result."""
        with self._lock:
            return (self._log_name, self._log) if self._log is not None else None

    # ── steps ──

    def _spawn(self, target, name):
        threading.Thread(target=target, name=name, daemon=True).start()

    def on_update_status(self, state, version):
        """The updater says it's downloading or installing a new version."""
        with self._lock:
            self._update = {"state": state, "version": version, "reason": None}

    def _check_for_update(self):
        try:
            outcome = self._updater.check()
        except Exception as e:  # the update mustn't stop anyone reading
            log.exception("Update check failed")
            outcome = updater_module.Outcome(updater_module.FAILED, reason=str(e))
        with self._lock:
            self._update = outcome.to_dict()
            if outcome.state in (updater_module.BLOCKED, updater_module.INSTALLED):
                return  # blocked: reading isn't allowed; installed: the new copy is opening
            self._step = SETUP

    def _update_note(self):
        """What the screens after the update say about it, if anything."""
        update = self._update
        if update["state"] != updater_module.FAILED:
            return ""
        what = f"Updating to {update['version']}" if update.get("version") else "Checking for updates"
        reason = (update.get("reason") or "").rstrip(".")
        return f"{what} didn't work ({reason}). You're reading with version {self._version}."

    def _start_waiting(self):
        with self._lock:
            self._generation += 1
            generation = self._generation
            self._step = WAITING
            self._waiting = {"since": self._clock(), "found": None}
            self._result = None
            self._log = self._log_name = None
        self._spawn(lambda: self._wait_for_camera(generation), name="camera-wait")

    def _current(self, generation):
        with self._lock:
            return generation == self._generation

    def _wait_for_camera(self, generation):
        started = self._clock()
        first = True
        while self._current(generation):
            try:
                devices = self._camera.list()
            except camera_module.ToolError as e:
                self._finish(generation, camera_module.failure(camera_module.FAILED, str(e)), "", "")
                return
            if devices:
                if not first:  # it's only just appeared
                    self._sleep(SETTLE)
                self._read(generation, devices)
                return
            first = False
            if self._clock() - started >= GIVE_UP_AFTER:
                self._no_camera(generation)
                return
            self._sleep(POLL_INTERVAL)

    def _read(self, generation, devices):
        name = camera_module.camera_name({}, devices) or "camera"
        with self._lock:
            if generation != self._generation:
                return
            self._step = READING
            self._waiting["found"] = name
            self._reading = {"camera": name}
        output, messages, trace = self._camera.read()
        self._finish(generation, output, messages, trace, devices=devices)

    def _no_camera(self, generation):
        try:
            everything = self._camera.list(everything=True)
        except camera_module.ToolError:
            everything = []
        self._finish(generation, camera_module.failure(camera_module.NO_CAMERA, "no camera found"), "", "",
                     usb_devices=everything)

    def _finish(self, generation, output, messages, trace, devices=(), usb_devices=None):
        read_at = self._now()
        summary = camera_module.summarize(output, devices, read_at)
        with self._lock:
            if generation != self._generation:
                return
            note = self._update_note()
        # After a read the fetch is done; with none (no camera found), it can't take
        # longer than its limit.
        fetched = self._tested_list.wait() if self._tested_list is not None else None
        text = report.build_log(
            app_name=self._app_name, version=self._version, repository_url=self._repository_url,
            read_at=read_at, mac=self._mac_info(), summary=summary, output=output, tool_messages=messages,
            trace=trace, update_note=note, usb_devices=usb_devices,
            tested_list=tested_list_module.log_line(fetched, output.get("tested_list")))
        with self._lock:
            if generation != self._generation:
                return
            self._step = RESULT
            self._result = summary
            self._log, self._log_name = text, report.file_name(read_at)
