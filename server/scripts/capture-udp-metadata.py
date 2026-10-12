#!/usr/bin/env python3
"""Linux eth0 packet-tap metadata observer. Never writes packet payload bytes."""
import argparse
import ctypes
import ipaddress
import json
import os
import socket
import struct
import time


def parse_udp_metadata(packet, address, peers, command_headers=False, logical_tracker=None, observed_ns=0):
    """Parse only IPv4 UDP/protocol metadata; reject fragments and short headers.

    AF_PACKET SOCK_DGRAM removes the link header. ETH_P_ALL is required to
    observe Linux outgoing packet taps; address[1] is the observed EtherType.
    No payload bytes or IP/MAC addresses are returned.
    """
    if len(address) < 3 or address[1] != 0x0800:
        return None
    if len(packet) < 28 or packet[0] >> 4 != 4 or packet[9] != 17:
        return None
    ihl = (packet[0] & 15) * 4
    total = struct.unpack_from("!H", packet, 2)[0]
    if ihl < 20 or ihl + 8 > total or total > len(packet):
        return None
    if struct.unpack_from("!H", packet, 6)[0] & 0x3fff:
        return None
    source, destination = packet[12:16], packet[16:20]
    outgoing = address[2] == 4
    peer_bytes = destination if outgoing else source
    if peer_bytes not in peers:
        return None
    source_port, destination_port, udp_size = struct.unpack_from("!HHH", packet, ihl)
    if udp_size < 8 or ihl + udp_size > total:
        return None
    body = memoryview(packet)[ihl+8:ihl+udp_size]
    if len(body) < 4 or body[0] != 0xf1:
        return None
    row = {"direction": "out" if outgoing else "in", "peer": peers[peer_bytes],
           "localPort": source_port if outgoing else destination_port,
           "peerPort": destination_port if outgoing else source_port,
           "bytes": len(body), "messageType": body[1]}
    if command_headers:
        row["declaredPayloadBytes"] = struct.unpack_from("!H", body, 2)[0]
    if logical_tracker is not None:
        logical_tracker.protocol(row, body)
    if body[1] == 0xd0:
        if len(body) < 8:
            return None
        row.update(dataType=body[5], sequence=struct.unpack_from("!H", body, 6)[0])
        if logical_tracker is not None and not outgoing:
            row["incomingLogical"] = logical_tracker.consume(row, body, observed_ns)
        # Inspect only the fixed 16-byte logical header at the packet start.
        # Never scan continuations, decrypt a body or retain header/raw bytes.
        if command_headers and outgoing and body[5] == 0 and len(body) >= 24:
            header = body[8:24]
            if header[:4] == b"XZYH":
                row.update(commandId=struct.unpack_from("<H", header, 4)[0],
                           commandChannel=header[12], commandSignCode=header[13],
                           commandType=header[14])
    elif body[1] == 0xd1:
        if len(body) < 10:
            return None
        count = struct.unpack_from("!H", body, 6)[0]
        if not 1 <= count <= 4096 or 8 + 2*count > len(body):
            return None
        seq = list(struct.unpack_from("!" + "H"*count, body, 8))
        row.update(dataType=body[5], ackCount=count, sequence=seq[0], ackSequences=seq)
    return row



class IncomingLogicalTracker:
    """Passive bounded diagnostics; never retains media/crypto bodies or reorders production traffic.

    First valid packet-start header is tentative. A subsequent calculated valid
    boundary confirms the chain. Reordered packets retain lengths/times only;
    if later consumption would cross a boundary, synchronization is lost.
    """
    MAX_UNIT = 8 * 1024 * 1024
    MAX_STREAMS = 64
    MAX_REORDER = 64
    COMMANDS = {1: {1300, 1301}, 0: {1000, 1003, 1004, 1100, 1103, 1139, 1306, 1308, 1350, 1351, 1700},
                2: {1000, 1003, 1004, 1100, 1103, 1139, 1306, 1308, 1350, 1351, 1700}}

    def __init__(self):
        self.streams = {}

    def protocol(self, row, body):
        """Forget endpoint state on well-framed END or CAM_ID; never affect transport."""
        if body[1] not in (0xf0, 0x42) or struct.unpack_from("!H", body, 2)[0] != len(body)-4:
            return
        endpoint = (row["peer"], row["localPort"], row["peerPort"])
        for key in list(self.streams):
            if key[:3] == endpoint:
                del self.streams[key]

    def _header(self, data, kind):
        if len(data) < 16 or data[:4] != b"XZYH":
            return None
        command, length = struct.unpack_from("<HI", data, 4)
        if (command not in self.COMMANDS.get(kind, set()) or length > self.MAX_UNIT
                or data[10] not in (0, 1, 8) or data[11] != 0 or data[15] != 0
                or (data[12] not in range(65) and data[12] != 255)
                or data[13] not in (0, 1, 2, 8)):
            return None
        return {"commandId": command, "logicalPayloadBytes": length,
                "channel": data[12], "signCode": data[13], "type": data[14]}

    @staticmethod
    def _lose(state):
        state.update(unit=None, remaining=0, header=bytearray(), chain=False, candidate=False)

    def _parse(self, state, data, kind, events):
        offset = 0
        for _ in range(128):
            if state["unit"] is not None:
                size = min(state["remaining"], len(data) - offset)
                state["remaining"] -= size
                offset += size
                if state["remaining"]:
                    return
                events.append({"event": "unit-complete", "unitId": state["unitId"]})
                state["unit"] = None
                if offset == len(data):
                    return
            if offset == len(data):
                return
            if not state["candidate"] and not state["chain"]:
                header = self._header(data[offset:offset+16], kind)
                if header is None:
                    return
                state["candidate"] = True
                offset += 16
            else:
                need = 16 - len(state["header"])
                take = min(need, len(data) - offset)
                state["header"].extend(data[offset:offset+take])
                offset += take
                if len(state["header"]) < 16:
                    if state["header"][:min(4, len(state["header"]))] != b"XZYH"[:min(4, len(state["header"]))]:
                        self._lose(state)
                        events.append({"event": "invalid-header"})
                    return
                header = self._header(state["header"], kind)
                state["header"].clear()
                if header is None:
                    self._lose(state)
                    events.append({"event": "invalid-header"})
                    return
                state["chain"] = True
            state["unitId"] += 1
            state["unit"] = header
            state["remaining"] = header["logicalPayloadBytes"]
            events.append({"event": "unit-start", "unitId": state["unitId"],
                           "confidence": "chain" if state["chain"] else "tentative", **header})
        self._lose(state)
        events.append({"event": "packed-unit-limit"})

    def consume(self, row, body, observed_ns):
        kind = row["dataType"]
        if kind not in self.COMMANDS or body[4] != 0xd1 or struct.unpack_from("!H", body, 2)[0] != len(body)-4:
            return {"sequenceState": "unsupported-or-malformed", "boundary": "unknown"}
        key = (row["peer"], row["localPort"], row["peerPort"], kind)
        if key not in self.streams:
            if len(self.streams) >= self.MAX_STREAMS:
                self.streams.pop(next(iter(self.streams)))
            self.streams[key] = {"expected": row["sequence"], "pending": {}, "unit": None,
                "remaining": 0, "header": bytearray(), "chain": False, "candidate": False, "unitId": 0}
        state = self.streams[key]
        sequence = row["sequence"]
        distance = (sequence - state["expected"]) & 65535
        events = []
        if distance >= 32768:
            sequence_state = "duplicate-or-old"
        elif distance:
            sequence_state = "out-of-order"
            if distance > self.MAX_REORDER or len(state["pending"]) >= self.MAX_REORDER:
                self._lose(state)
                state["pending"].clear()
                state["expected"] = (sequence + 1) & 65535
                sequence_state = "reorder-limit-reset"
            else:
                state["pending"].setdefault(sequence, {"payloadBytes": len(body)-8, "observedNs": observed_ns})
        else:
            sequence_state = "ordered"
            self._parse(state, body[8:], kind, events)
            state["expected"] = (sequence + 1) & 65535
            while state["expected"] in state["pending"]:
                pending = state["pending"].pop(state["expected"])
                length = pending["payloadBytes"]
                if state["unit"] is not None and length <= state["remaining"]:
                    state["remaining"] -= length
                    if not state["remaining"]:
                        events.append({"event": "unit-complete", "unitId": state["unitId"], "reorderedLengthOnly": True})
                        state["unit"] = None
                else:
                    self._lose(state)
                    events.append({"event": "reordered-boundary-unknown"})
                state["expected"] = (state["expected"] + 1) & 65535
        trusted = state["chain"] and not state["pending"]
        result = {"sequenceState": sequence_state, "expectedSequence": state["expected"],
                  "pendingPackets": len(state["pending"]), "boundary": "unit" if trusted and state["unit"] else "partial-header" if trusted and state["header"] else "header" if trusted else "unknown"}
        if state["header"]:
            result["partialHeaderBytes"] = len(state["header"])
        if state["unit"] is not None:
            result.update(unitId=state["unitId"], remainingPayloadBytes=state["remaining"],
                          confidence="chain" if trusted else "tentative", **state["unit"])
        if events:
            result["events"] = events
        return result

def attach_udp_filter(sock):
    """Kernel classic-BPF filter, before receive-buffer allocation/userspace.

    ETH_P_ALL is needed for outgoing taps but without this filter unrelated TCP
    inspector export can fill the constrained packet socket receive buffer.
    SKF_AD_PROTOCOL reads EtherType for both incoming and outgoing packets.
    """
    class Insn(ctypes.Structure):
        _fields_ = [("code", ctypes.c_ushort), ("jt", ctypes.c_ubyte),
                    ("jf", ctypes.c_ubyte), ("k", ctypes.c_uint32)]
    class Program(ctypes.Structure):
        _fields_ = [("length", ctypes.c_ushort), ("filter", ctypes.POINTER(Insn))]
    instructions = (Insn * 6)(
        Insn(0x20, 0, 0, 0xfffff000),  # LD W ABS SKF_AD_PROTOCOL
        Insn(0x15, 0, 3, 0x0800),      # JEQ IPv4; else reject
        Insn(0x30, 0, 0, 9),           # LD B ABS IP protocol
        Insn(0x15, 0, 1, 17),          # JEQ UDP; else reject
        Insn(0x06, 0, 0, 65535),       # RET full datagram
        Insn(0x06, 0, 0, 0),           # RET reject
    )
    program = Program(len(instructions), instructions)
    sock.setsockopt(socket.SOL_SOCKET, 26, bytes(program))  # SO_ATTACH_FILTER


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--interface", default="eth0")
    parser.add_argument("--peer", action="append", required=True, help="IPv4 peer to observe; saved as an anonymous peer ID")
    parser.add_argument("--seconds", type=float, default=90)
    parser.add_argument("--out", required=True)
    parser.add_argument("--incoming-logical-headers", action="store_true", help="Track incoming fixed logical-header integer fields conservatively; never retain payloads")
    parser.add_argument("--command-headers", action="store_true", help="Record integer fields from outbound DATA fixed headers only; never encrypted bodies")
    args = parser.parse_args()
    if not 0 < args.seconds <= 600:
        parser.error("seconds must be >0 and <=600")
    peers = {socket.inet_aton(str(ipaddress.IPv4Address(host))): f"peer-{i+1}" for i,host in enumerate(args.peer)}
    if not hasattr(socket,"AF_PACKET"):
        parser.error("Linux AF_PACKET and root/CAP_NET_RAW are required")
    # SOCK_DGRAM strips the link header but retains the IP/UDP headers. The
    # ETH_P_ALL captures ingress AND outgoing packet taps. Explicit EtherType
    # filtering occurs before parsing. Kernel timestamps are software packet-tap
    # times, not NIC completion or peer receipt.
    sock = socket.socket(socket.AF_PACKET,socket.SOCK_DGRAM,socket.htons(0x0003))
    sock.bind((args.interface,0))
    attach_udp_filter(sock)
    timestamp_option = getattr(socket,"SO_TIMESTAMPNS",35)
    sock.setsockopt(socket.SOL_SOCKET,timestamp_option,1)
    sock.setsockopt(socket.SOL_SOCKET,socket.SO_RCVBUF,8*1024*1024)
    # This observer socket alone needs room for complete large-IDR bursts. Root
    # may bypass rmem_max without changing any sysctl or production socket.
    receive_buffer_forced = False
    if sock.getsockopt(socket.SOL_SOCKET,socket.SO_RCVBUF) < 8*1024*1024:
        try:
            sock.setsockopt(socket.SOL_SOCKET,33,8*1024*1024) # SO_RCVBUFFORCE
            receive_buffer_forced = True
        except OSError:
            pass  # Capture stats explicitly expose any resulting tap drops.
    sock.settimeout(.5)
    start_wall_ns,start_mono_ns = time.time_ns(),time.monotonic_ns()
    os.umask(0o077)
    logical_tracker = IncomingLogicalTracker() if args.incoming_logical_headers else None
    captured,ignored,untimestamped,truncated = 0,0,0,0
    with sock,open(args.out,"w",encoding="utf8") as out:
        out.write(json.dumps({"phase":"capture-start","scope":"kernel-interface-packet-tap","interface":args.interface,"startedUnixNs":str(start_wall_ns),"startedMonotonicNs":str(start_mono_ns),"receiveBufferBytes":sock.getsockopt(socket.SOL_SOCKET,socket.SO_RCVBUF),"peerIds":list(peers.values()),"payloadStored":False,"kernelFilter":"IPv4 UDP","receiveBufferForced":receive_buffer_forced,"commandHeaders":args.command_headers,"commandHeaderInspectionBytes":16 if args.command_headers else 0,"incomingLogicalHeaders":args.incoming_logical_headers,"logicalReorderMetadataOnly":True if logical_tracker else False})+"\n")
        while time.monotonic_ns()-start_mono_ns < args.seconds*1e9:
            try:
                packet,ancillary,flags,address=sock.recvmsg(65535,socket.CMSG_SPACE(32))
            except socket.timeout:
                continue
            if flags & socket.MSG_TRUNC:
                truncated += 1
                continue
            row = parse_udp_metadata(packet, address, peers, args.command_headers)
            if row is None:
                ignored += 1
                continue
            wall_ns=None
            for level,kind,value in ancillary:
                if level==socket.SOL_SOCKET and kind==timestamp_option:
                    if len(value)>=16:
                        sec,nsec=struct.unpack_from("@qq",value)
                    elif len(value)>=8:
                        sec,nsec=struct.unpack_from("@ii",value)
                    else:
                        continue
                    wall_ns=sec*1000000000+nsec
                    break
            if wall_ns is None:
                untimestamped += 1
                continue
            if logical_tracker is not None:
                row = parse_udp_metadata(packet, address, peers, args.command_headers, logical_tracker, wall_ns)
            row.update(phase="udp", unixNs=str(wall_ns), unixMs=wall_ns/1000000)
            out.write(json.dumps(row,separators=(",",":"))+"\n")
            captured += 1
        stats=sock.getsockopt(263,6,8) # SOL_PACKET / PACKET_STATISTICS
        seen,dropped=struct.unpack("=II",stats)
        summary={"phase":"capture-end","endedUnixNs":str(time.time_ns()),"endedMonotonicNs":str(time.monotonic_ns()),"captured":captured,"ignored":ignored,"untimestamped":untimestamped,"truncated":truncated,"packetTapSeen":seen,"packetTapDropped":dropped,"payloadStored":False}
        out.write(json.dumps(summary)+"\n")
    print(json.dumps(summary),flush=True)


if __name__=="__main__":
    main()
