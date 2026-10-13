"""Read-only process identity for bounded Linux playback diagnostics.
No URL credentials or command lines are returned. This is not playback policy.
"""
from contextlib import contextmanager
from urllib.parse import unquote, urlsplit


def camera_pids(command_lines, camera_keys):
    """Match each independent Gst child using actual RTSP paths, not log tags.
    Reject a composite/shared child or ambiguous aliases before any signal.
    """
    found = {}
    for pid, command in command_lines.items():
        locations = []
        for argument in command.split(b"\0"):
            if argument.startswith(b"location="):
                uri = argument[len(b"location="):].decode("utf-8", "strict")
                locations.append(unquote(urlsplit(uri).path).strip("/").casefold())
        for camera, keys in camera_keys.items():
            if any(path in {key.casefold() for key in keys} for path in locations):
                if camera in found or pid in found.values():
                    raise ValueError("ambiguous camera/process mapping")
                found[camera] = pid
    if set(found) != set(camera_keys):
        raise ValueError("not all camera processes are present")
    return found


def require_unchanged(expected, current):
    """Snapshots include PID and Linux start-time ticks to detect replacement.
    The caller must abort its work comparison rather than relabel restarted data.
    """
    if expected != current:
        raise RuntimeError("pipeline replaced; no work-stop allowed")


@contextmanager
def stopped_work(expected, snapshot, stop, resume):
    """Resume exactly the successfully stopped work even when sampling fails."""
    require_unchanged(expected, snapshot())
    stop()
    try:
        yield
    finally:
        resume()
