#!/usr/bin/env python3
"""Read-only RTSP observer: bridge-ingress to observer receipt, NOT presentation/capture age."""
import argparse
import concurrent.futures
import importlib.util
import json
from pathlib import Path
import socket
import struct
import threading
import time
from urllib.parse import urlsplit
from urllib.request import urlopen

_spec = importlib.util.spec_from_file_location("rtsp_clock", Path(__file__).with_name("measure-rtsp-clock.py"))
_clock = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(_clock)
UUID = bytes.fromhex("455746524553484e4553533030303031")


def parse_stamp(nal, codec="H264"):
    header = 2 if codec == "H265" else 1
    if not nal or (codec == "H265" and (len(nal) < 2 or (nal[0] >> 1) & 63 != 39)) or (codec == "H264" and nal[0] & 31 != 6):
        return None
    rbsp = bytearray()
    zeros = 0
    for i, byte in enumerate(nal[header:]):
        if zeros >= 2 and byte == 3:
            if i + header + 1 >= len(nal) or nal[i + header + 1] > 3:
                return None
            zeros = 0
            continue
        rbsp.append(byte)
        zeros = zeros + 1 if byte == 0 else 0
    at = 0
    while at < len(rbsp):
        if rbsp[at] == 128 and not any(rbsp[at + 1:]):
            break
        values = []
        for _ in range(2):
            value = 0
            while at < len(rbsp) and rbsp[at] == 255:
                value += 255
                at += 1
            if at == len(rbsp):
                return None
            value += rbsp[at]
            at += 1
            values.append(value)
        kind, size = values
        if size > len(rbsp) - at:
            return None
        payload = rbsp[at:at + size]
        at += size
        if kind != 5 or size != 48 or payload[:16] != UUID:
            continue
        if payload[16] != 1 or payload[17] != 1:
            continue
        age = struct.unpack_from("!H", payload, 18)[0]
        if not age:
            continue
        return {"serverEpoch": bytes(payload[20:28]).hex(), "sourceTimestamp": struct.unpack_from("!I", payload, 28)[0],
                "ingressMonoUs": struct.unpack_from("!Q", payload, 32)[0], "frameId": struct.unpack_from("!Q", payload, 40)[0], "maxAgeMs": age}
    return None


def rtp_payload(packet):
    if len(packet) < 12 or packet[0] >> 6 != 2:
        raise ValueError("Invalid RTP header")
    at = 12 + (packet[0] & 15) * 4
    if at > len(packet):
        raise ValueError("Truncated RTP CSRC list")
    if packet[0] & 16:
        if at + 4 > len(packet):
            raise ValueError("Truncated RTP extension")
        at += 4 + struct.unpack_from("!H", packet, at + 2)[0] * 4
    end = len(packet)
    if packet[0] & 32:
        if not packet[-1] or packet[-1] > end - at:
            raise ValueError("Invalid RTP padding")
        end -= packet[-1]
    if at >= end:
        raise ValueError("Empty/truncated RTP payload")
    return struct.unpack_from("!H", packet, 2)[0], struct.unpack_from("!I", packet, 4)[0], struct.unpack_from("!I", packet, 8)[0], bool(packet[1] & 128), packet[at:end]


class H264Frames:
    """Bounded AU assembly. A sequence gap or incomplete FU invalidates the AU."""
    def __init__(self, max_frame_bytes=4 * 1024 * 1024):
        self.limit = max_frame_bytes
        self.sequence = None
        self.ssrc = None
        self.timestamp = None
        self.nals = []
        self.fu = None
        self.size = 0
        self.invalid = False
        self.abandoned = 0

    def push(self, packet):
        seq, ts, ssrc, marker, body = rtp_payload(packet)
        gap = self.sequence is not None and (seq - self.sequence) % 65536 != 1
        if self.ssrc is not None and ssrc != self.ssrc:
            gap = True
        self.sequence, self.ssrc = seq, ssrc
        if self.timestamp != ts:
            if self.nals or self.fu:
                self.abandoned += 1
            self.timestamp, self.nals, self.fu, self.size, self.invalid = ts, [], None, 0, gap
        else:
            self.invalid |= gap
        kind = body[0] & 31
        self.size += len(body)
        if self.size > self.limit:
            raise ValueError("RTP access-unit size limit exceeded")
        if 1 <= kind <= 23:
            if self.fu is not None:
                self.invalid = True
            self.nals.append(body)
        elif kind == 24:  # STAP-A: every length must fit the packet exactly.
            at = 1
            while at < len(body):
                if at + 2 > len(body):
                    self.invalid = True
                    break
                size = struct.unpack_from("!H", body, at)[0]
                at += 2
                if not size or at + size > len(body):
                    self.invalid = True
                    break
                nal = body[at:at + size]
                if not 1 <= (nal[0] & 31) <= 23:
                    self.invalid = True
                self.nals.append(nal)
                at += size
        elif kind == 28 and len(body) >= 3:  # FU-A
            start, end, nal_kind = bool(body[1] & 128), bool(body[1] & 64), body[1] & 31
            if not 1 <= nal_kind <= 23 or body[1] & 32 or (start and end):
                self.invalid = True
            if start:
                if self.fu is not None:
                    self.invalid = True
                self.fu = bytearray([(body[0] & 224) | nal_kind])
            elif self.fu is None or (self.fu[0] & 31) != nal_kind:
                self.invalid = True
            if self.fu is not None:
                self.fu.extend(body[2:])
                if end:
                    self.nals.append(bytes(self.fu))
                    self.fu = None
        else:
            self.invalid = True
        if not marker:
            return None
        invalid = self.invalid or self.fu is not None
        stamps = [stamp for nal in self.nals if (stamp := parse_stamp(nal)) is not None]
        has_vcl = any(1 <= (nal[0] & 31) <= 5 for nal in self.nals)
        result = {"rtpTimestamp": ts, "transportValid": not invalid, "stamp": stamps[0] if not invalid and has_vcl and len(stamps) == 1 else None}
        self.timestamp, self.nals, self.fu, self.size, self.invalid = None, [], None, 0, False
        return result



class H265Frames(H264Frames):
    """RFC7798 single NAL/AP/FU without DONL; same bounded AU identity checks."""
    def push(self, packet):
        seq, ts, ssrc, marker, body = rtp_payload(packet)
        gap = self.sequence is not None and (seq-self.sequence) % 65536 != 1
        if self.ssrc is not None and ssrc != self.ssrc:
            gap = True
        self.sequence, self.ssrc = seq, ssrc
        if self.timestamp != ts:
            if self.nals or self.fu:
                self.abandoned += 1
            self.timestamp, self.nals, self.fu, self.size, self.invalid = ts, [], None, 0, gap
        else:
            self.invalid |= gap
        self.size += len(body)
        if self.size > self.limit:
            raise ValueError("RTP access-unit size limit exceeded")
        if len(body) < 2 or body[0] & 128 or not body[1] & 7:
            self.invalid = True
        else:
            kind = (body[0] >> 1) & 63
            if kind < 48:
                if self.fu is not None:
                    self.invalid = True
                self.nals.append(body)
            elif kind == 48:
                if self.fu is not None:
                    self.invalid = True
                at = 2
                while at < len(body):
                    if at + 2 > len(body):
                        self.invalid = True
                        break
                    size = struct.unpack_from("!H", body, at)[0]
                    at += 2
                    if size < 2 or at+size > len(body):
                        self.invalid = True
                        break
                    nal = body[at:at+size]
                    if nal[0] & 128 or not nal[1] & 7 or (nal[0] >> 1) & 63 >= 48:
                        self.invalid = True
                    self.nals.append(nal)
                    at += size
            elif kind == 49 and len(body) >= 4:
                start, end, nal_kind = bool(body[2] & 128), bool(body[2] & 64), body[2] & 63
                header = bytes([(body[0] & 129) | (nal_kind << 1), body[1]])
                if nal_kind >= 48 or start and end:
                    self.invalid = True
                if start:
                    if self.fu is not None:
                        self.invalid = True
                    self.fu = bytearray(header)
                elif self.fu is None or self.fu[:2] != header:
                    self.invalid = True
                if self.fu is not None:
                    self.fu.extend(body[3:])
                    if end:
                        self.nals.append(bytes(self.fu))
                        self.fu = None
            else:
                self.invalid = True
        if not marker:
            return None
        invalid = self.invalid or self.fu is not None
        stamps = [stamp for nal in self.nals if (stamp := parse_stamp(nal,"H265")) is not None]
        has_vcl = any((nal[0] >> 1) & 63 <= 31 for nal in self.nals)
        result = {"rtpTimestamp": ts, "transportValid": not invalid, "stamp": stamps[0] if not invalid and has_vcl and len(stamps) == 1 else None}
        self.timestamp, self.nals, self.fu, self.size, self.invalid = None, [], None, 0, False
        return result

def sample_clock(url, samples=3):
    best = None
    for _ in range(samples):
        t0 = time.perf_counter_ns() // 1000
        with urlopen(url, timeout=3) as response:
            body = response.read(8193)
        t1 = time.perf_counter_ns() // 1000
        if len(body) > 8192:
            raise ValueError("Clock response size limit exceeded")
        value = json.loads(body)
        epoch, server = value.get("serverEpoch"), value.get("monotonicUs")
        if not isinstance(epoch, str) or len(epoch) != 16 or any(c not in "0123456789abcdefABCDEF" for c in epoch):
            raise ValueError("Invalid server clock epoch")
        if not isinstance(server, int) or isinstance(server, bool) or server < 0:
            raise ValueError("Invalid server monotonic clock")
        sample = {"epoch": epoch.lower(), "offsetLowUs": server - t1, "offsetHighUs": server - t0, "rttUs": t1 - t0, "sampleLocalUs": t1}
        if best is not None and sample["epoch"] != best["epoch"]:
            raise ValueError("Bridge epoch changed during clock sampling")
        if best is None or sample["rttUs"] < best["rttUs"]:
            best = sample
    return best


def age_bounds(stamp, local_us, clock, drift_ppm=100):
    if stamp["serverEpoch"] != clock["epoch"]:
        raise ValueError("Frame/server clock epoch mismatch")
    drift_us = abs(local_us - clock["sampleLocalUs"]) * drift_ppm / 1000000
    low = (local_us + clock["offsetLowUs"] - drift_us - stamp["ingressMonoUs"]) / 1000
    high = (local_us + clock["offsetHighUs"] + drift_us - stamp["ingressMonoUs"]) / 1000
    return low, high


def percentile(values, fraction):
    ordered = sorted(values)
    return round(ordered[min(len(ordered)-1, int((len(ordered)-1)*fraction))], 3) if ordered else None


class ClockSampler:
    """Refresh calibration independently so an HTTP stall cannot stop RTSP reads."""
    def __init__(self, initial, sample, interval=5):
        self.clock, self.sample, self.interval = initial, sample, interval
        self.history, self.error = [initial], None
        self.lock, self.stop = threading.Lock(), threading.Event()
        self.worker = threading.Thread(target=self._run, name="rtsp-observer-clock", daemon=True)

    def _run(self):
        while not self.stop.wait(self.interval):
            try:
                fresh = self.sample()
                with self.lock:
                    if self.stop.is_set():
                        return
                    if fresh["epoch"] != self.clock["epoch"]:
                        raise ValueError("Bridge clock epoch changed during observation")
                    self.clock = fresh
                    self.history.append(fresh)
            except Exception as error:
                with self.lock:
                    self.error = error
                return

    def snapshot(self):
        with self.lock:
            if self.error is not None:
                raise self.error
            return self.clock

    def samples(self):
        with self.lock:
            return list(self.history)

    def __enter__(self):
        self.worker.start()
        return self

    def __exit__(self, *unused):
        self.stop.set()
        self.worker.join(7)
        if self.worker.is_alive():
            raise ValueError("Clock observer did not stop within its shutdown bound")


def measure(url, clock_url, duration, settle, drift_ppm):
    parsed = urlsplit(url)
    if parsed.scheme != "rtsp" or not parsed.hostname or parsed.username:
        raise ValueError("Use an unauthenticated rtsp://host:port/stream URL")
    clock = sample_clock(clock_url)
    frames, assembler = [], None
    with ClockSampler(clock, lambda: sample_clock(clock_url, 1)) as sampler, socket.create_connection((parsed.hostname, parsed.port or 554), 5) as sock:
        sock.settimeout(5)
        pending, seq, session = b"", 0, None
        def receive():
            data = sock.recv(65536)
            if not data:
                raise ConnectionError("RTSP connection closed")
            return data
        def request(method, target, headers=""):
            nonlocal pending, seq, session
            seq += 1
            msg = f"{method} {target} RTSP/1.0\r\nCSeq: {seq}\r\n{headers}" + (f"Session: {session}\r\n" if session else "") + "\r\n"
            sock.sendall(msg.encode())
            while b"\r\n\r\n" not in pending:
                pending += receive()
                if len(pending) > 1024 * 1024:
                    raise ValueError("RTSP response header limit exceeded")
            raw, pending = pending.split(b"\r\n\r\n", 1)
            lines = raw.decode().split("\r\n")
            fields = dict((k.lower(), v.strip()) for k, v in (line.split(":", 1) for line in lines[1:]))
            size = int(fields.get("content-length", "0"))
            if not 0 <= size <= 1024 * 1024:
                raise ValueError("RTSP response body size outside limit")
            while len(pending) < size:
                pending += receive()
            body, pending = pending[:size], pending[size:]
            if not lines[0].startswith("RTSP/1.0 200 "):
                raise ValueError(lines[0])
            if "session" in fields:
                session = fields["session"].split(";", 1)[0]
            return body, fields
        sdp, fields = request("DESCRIBE", url, "Accept: application/sdp\r\n")
        video, control, codec = False, None, None
        for line in sdp.decode().splitlines():
            if line.startswith("m="):
                if video:
                    break
                video = line.startswith("m=video ")
            if video and line.startswith("a=control:"):
                control = line[len("a=control:"):]
            if video and line.startswith("a=rtpmap:"):
                codec = line.split(" ", 1)[1].split("/", 1)[0].upper()
        if not control or codec not in ("H264", "H265"):
            raise ValueError("Observer requires an H264 or H265 video track")
        if codec == "H265" and any("sprop-max-don-diff=" in line and int(line.split("sprop-max-don-diff=",1)[1].split(";",1)[0]) != 0 for line in sdp.decode().splitlines()):
            raise ValueError("HEVC observer does not support DONL")
        assembler = H265Frames() if codec == "H265" else H264Frames()
        base = fields.get("content-base", url.rstrip("/")+"/")
        track = control if control.startswith("rtsp://") else base.rstrip("/")+"/"+control.lstrip("/")
        request("SETUP", track, "Transport: RTP/AVP/TCP;unicast;interleaved=0-1\r\n")
        request("PLAY", url)
        start = time.monotonic()
        keepalive_at = start
        keepalive_requests, keepalive_responses, bytes_read = 0, 0, 0
        while time.monotonic()-start < duration:
            now = time.monotonic()
            clock = sampler.snapshot()
            if now-keepalive_at >= 20:
                seq += 1
                sock.sendall((f"OPTIONS {url} RTSP/1.0\r\nCSeq: {seq}\r\nSession: {session}\r\n\r\n").encode())
                keepalive_at = now
                keepalive_requests += 1
            while len(pending) >= 4:
                message = _clock.play_message(pending)
                if message is None:
                    break
                channel, packet, pending = message
                if channel is None:
                    keepalive_responses += 1
                    continue
                bytes_read += len(packet)
                if channel != 0:
                    continue
                frame = assembler.push(packet)
                if frame is None:
                    continue
                local_us = time.perf_counter_ns()//1000
                frame["seconds"] = round(time.monotonic()-start, 6)
                frame["receiptMonotonicUs"] = local_us
                if frame["stamp"]:
                    low, high = age_bounds(frame["stamp"], local_us, clock, drift_ppm)
                    frame["ageLowerMs"], frame["ageUpperMs"] = round(low, 3), round(high, 3)
                    frame["clockUncertaintyMs"] = round(high-low, 3)
                frames.append(frame)
                if len(frames) > 100000:
                    raise ValueError("Observer frame count limit exceeded")
            remaining = duration-(time.monotonic()-start)
            sock.settimeout(max(0.01, min(1, remaining)))
            try:
                pending += receive()
                if len(pending) > 8*1024*1024:
                    raise ValueError("RTSP receive buffer limit exceeded")
            except socket.timeout:
                pass
    stable = [f for f in frames if f["seconds"] > settle]
    stamped = [f for f in stable if f["stamp"]]
    ids = [f["stamp"]["frameId"] for f in stamped]
    upper = [f["ageUpperMs"] for f in stamped]
    result = {"camera": parsed.path.strip("/"), "codec": codec, "scope": "bridge-ingress-to-observer-receipt", "captureClockValidated": False,
              "presentationMeasured": False, "serverEpoch": clock["epoch"], "clockDriftAllowancePpm": drift_ppm,
              "frames": len(frames), "stableFrames": len(stable), "stampedFrames": len(stamped),
              "missingOrInvalidStampFrames": len(stable)-len(stamped), "transportInvalidFrames": sum(not f["transportValid"] for f in stable),
              "abandonedAccessUnits": assembler.abandoned, "uniqueFrameIds": len(set(ids)),
              "frameIdDuplicates": len(ids)-len(set(ids)), "frameIdRegressions": sum(b<a for a,b in zip(ids,ids[1:])),
              "overDeadlineFrames": sum(f["ageUpperMs"] >= f["stamp"]["maxAgeMs"] for f in stamped),
              "negativeAgeUpperFrames": sum(age<0 for age in upper),
              "ageUpperMs": {"median": percentile(upper,.5),"p95":percentile(upper,.95),"p99":percentile(upper,.99),"maximum":round(max(upper),3) if upper else None},
              "maximumClockUncertaintyMs": max((f["clockUncertaintyMs"] for f in stamped),default=None),
              "clockSamples": sampler.samples(),"clockRefreshScope":"independent-observer-thread",
              "bytes":bytes_read,"keepaliveRequests":keepalive_requests,"keepaliveResponses":keepalive_responses,"samples":stable}
    if not stamped:
        result["error"] = "No complete stamped frames in stable observation window"
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("urls", nargs="+")
    parser.add_argument("--clock-url", help="HTTP /api/clock endpoint; default RTSP host port 3000")
    parser.add_argument("--duration", type=float, default=30)
    parser.add_argument("--settle", type=float, default=3)
    parser.add_argument("--clock-drift-ppm", type=float, default=100, help="Explicit oscillator drift allowance for age bounds")
    args = parser.parse_args()
    if not 0 <= args.settle < args.duration <= 600 or not 0 <= args.clock_drift_ppm <= 10000:
        parser.error("Require 0 <= settle < duration <= 600 and 0 <= drift ppm <= 10000")
    def safe(url):
        host = urlsplit(url).hostname
        clock_url = args.clock_url or f"http://{'['+host+']' if host and ':' in host else host}:3000/api/clock"
        try:
            return measure(url,clock_url,args.duration,args.settle,args.clock_drift_ppm)
        except (OSError,ValueError,KeyError,TypeError) as error:
            return {"camera":urlsplit(url).path.strip("/"),"error":str(error),"scope":"bridge-ingress-to-observer-receipt","presentationMeasured":False}
    with concurrent.futures.ThreadPoolExecutor(max_workers=min(8,len(args.urls))) as pool:
        results = list(pool.map(safe,args.urls))
    for result in results:
        print(json.dumps(result),flush=True)
    return int(any("error" in result for result in results))


if __name__ == "__main__":
    raise SystemExit(main())
