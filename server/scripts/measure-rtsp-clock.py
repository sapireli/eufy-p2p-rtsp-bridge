#!/usr/bin/env python3
"""Compare the RTSP video RTP clock with arrival time, without decoding.

This detects clock drift; it does not measure camera-to-screen latency or a
constant delay. Intended for the bridge's unauthenticated RTSP endpoint.
"""
import argparse
import concurrent.futures
import json
import socket
import struct
import time
from urllib.parse import urlsplit


def timestamp_delta(new, old):
    # Signed difference handles both rollover and small timestamp regressions.
    return ((new - old + (1 << 31)) % (1 << 32)) - (1 << 31)


def measure(url, duration, settle):
    parsed = urlsplit(url)
    name = parsed.path.strip("/")
    if parsed.scheme != "rtsp" or not parsed.hostname or parsed.username:
        raise ValueError("Use an unauthenticated rtsp://host:port/stream URL")
    with socket.create_connection((parsed.hostname, parsed.port or 554), 5) as sock:
        sock.settimeout(5)
        pending = b""
        sequence = 0
        session = None

        def receive():
            data = sock.recv(65536)
            if not data:
                raise ConnectionError("RTSP connection closed")
            return data

        def request(method, target, headers=""):
            nonlocal sequence, pending, session
            sequence += 1
            message = f"{method} {target} RTSP/1.0\r\nCSeq: {sequence}\r\n{headers}"
            if session:
                message += f"Session: {session}\r\n"
            sock.sendall((message + "\r\n").encode())
            while b"\r\n\r\n" not in pending:
                pending += receive()
            raw, pending = pending.split(b"\r\n\r\n", 1)
            lines = raw.decode().split("\r\n")
            fields = {}
            for line in lines[1:]:
                key, value = line.split(":", 1)
                fields[key.lower()] = value.strip()
            size = int(fields.get("content-length", 0))
            while len(pending) < size:
                pending += receive()
            body, pending = pending[:size], pending[size:]
            if not lines[0].startswith("RTSP/1.0 200 "):
                raise ValueError(lines[0])
            if "session" in fields:
                session = fields["session"].split(";", 1)[0]
            return body, fields

        sdp, fields = request("DESCRIBE", url, "Accept: application/sdp\r\n")
        video = False
        control = None
        rate = None
        for line in sdp.decode().splitlines():
            if line.startswith("m="):
                if video:
                    break
                video = line.startswith("m=video ")
            if video and line.startswith("a=control:"):
                control = line[len("a=control:"):]
            if video and line.startswith("a=rtpmap:"):
                rate = int(line.split("/", 2)[1])
        if not control or not rate:
            raise ValueError("SDP has no usable video track")
        # urljoin does not treat rtsp as a relative-path scheme on all Python versions.
        base = fields.get("content-base", url.rstrip("/") + "/")
        track = control if control.startswith("rtsp://") else base.rstrip("/") + "/" + control.lstrip("/")
        request("SETUP", track, "Transport: RTP/AVP/TCP;unicast;interleaved=0-1\r\n")
        request("PLAY", url)
        start = time.monotonic()
        frames = []
        bytes_read = 0
        while time.monotonic() - start < duration:
            while len(pending) >= 4:
                if pending[0] != 36:
                    raise ValueError("Unexpected non-interleaved data during PLAY")
                channel, size = pending[1], struct.unpack("!H", pending[2:4])[0]
                if len(pending) < 4 + size:
                    break
                packet, pending = pending[4:4 + size], pending[4 + size:]
                bytes_read += len(packet)
                if channel == 0 and len(packet) >= 12 and packet[0] >> 6 == 2 and packet[1] & 128:
                    frames.append((time.monotonic() - start, struct.unpack("!I", packet[4:8])[0]))
            sock.settimeout(max(0.01, min(1, duration - (time.monotonic() - start))))
            try:
                pending += receive()
            except socket.timeout:
                pass

    stable = [frame for frame in frames if frame[0] > settle]
    if len(stable) < 2:
        return {"camera": name, "frames": len(frames), "error": "insufficient frames"}
    wall = stable[-1][0] - stable[0][0]
    steps = [timestamp_delta(b[1], a[1]) for a, b in zip(stable, stable[1:])]
    media = sum(steps) / rate
    if wall <= 0 or media <= 0:
        return {"camera": name, "error": "non-progressing clock"}
    return {
        "camera": name, "frames": len(frames), "stable_frames": len(stable),
        "unique_timestamps": len({f[1] for f in stable}), "clock_rate": rate,
        "wall_seconds": round(wall, 3), "rtp_seconds": round(media, 3),
        "rtp_to_wall_ratio": round(media / wall, 4),
        "arrival_fps": round((len(stable) - 1) / wall, 2),
        "rtp_fps": round((len(stable) - 1) / media, 2),
        "timestamp_steps": sorted(set(steps))[:12],
        "timestamp_regressions": sum(step < 0 for step in steps), "bytes": bytes_read,
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("urls", nargs="+")
    parser.add_argument("--duration", type=float, default=15)
    parser.add_argument("--settle", type=float, default=3)
    args = parser.parse_args()
    if not 0 <= args.settle < args.duration:
        parser.error("Require 0 <= settle < duration")

    def safe_measure(url):
        try:
            return measure(url, args.duration, args.settle)
        except (OSError, ValueError) as error:
            return {"camera": urlsplit(url).path.strip("/"), "error": str(error)}

    failed = False
    with concurrent.futures.ThreadPoolExecutor(max_workers=min(8, len(args.urls))) as pool:
        for result in pool.map(safe_measure, args.urls):
            print(json.dumps(result), flush=True)
            failed |= "error" in result
    return int(failed)


if __name__ == "__main__":
    raise SystemExit(main())
