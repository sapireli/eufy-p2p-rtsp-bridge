// Joins FFmpeg's encoded data output with one stats_mux_pre record per packet:
// "{n} {pts} {tb} {size}". No Annex-B boundary guesses and no next-frame hold.
// Configure encoder timebase/copyts to preserve the input source PTS.
export class EncodedFrameReader {
  constructor({ onFrame, maxFrameBytes = 4 * 1024 * 1024, maxBufferedBytes = 8 * 1024 * 1024, maxPendingFrames = 120 } = {}) {
    if (typeof onFrame !== "function") throw new TypeError("onFrame must be a function");
    for (const [name, value] of Object.entries({ maxFrameBytes, maxBufferedBytes, maxPendingFrames })) {
      if (!Number.isSafeInteger(value) || value < 1) throw new TypeError(`${name} must be a positive safe integer`);
    }
    this.onFrame = onFrame;
    this.maxFrameBytes = maxFrameBytes;
    this.maxBufferedBytes = maxBufferedBytes;
    this.maxPendingFrames = maxPendingFrames;
    this.chunks = [];
    this.available = 0;
    this.offset = 0;
    this.records = [];
    this.statsRemainder = "";
    this.nextRecord = 0n;
    this.ended = false;
  }
  pushData(chunk) {
    this.assertOpen();
    if (!Buffer.isBuffer(chunk)) throw new TypeError("encoded data must be a Buffer");
    if (!chunk.length) return;
    if (this.available + chunk.length > this.maxBufferedBytes) throw new RangeError("encoded data buffer limit exceeded");
    this.chunks.push(chunk);
    this.available += chunk.length;
    this.drain();
  }
  pushStats(chunk) {
    this.assertOpen();
    if (!Buffer.isBuffer(chunk) && typeof chunk !== "string") throw new TypeError("stats must be a Buffer or string");
    if (Buffer.isBuffer(chunk) && chunk.some((byte) => byte > 127)) throw new TypeError("stats must be ASCII text");
    const text = Buffer.isBuffer(chunk) ? chunk.toString("ascii") : chunk;
    if (!/^[\x09\x0a\x0d\x20-\x7e]*$/.test(text)) throw new TypeError("stats must be ASCII text");
    // A stats write may contain many records; bound each unterminated record.
    for (const part of text.split(/(?<=\n)/)) {
      this.statsRemainder += part;
      if (this.statsRemainder.length > 256) throw new RangeError("stats line limit exceeded");
      if (this.statsRemainder.endsWith("\n")) {
        const line = this.statsRemainder.trim();
        this.statsRemainder = "";
        if (!line) throw new TypeError("empty stats record");
        this.addRecord(line);
        this.drain();
      }
    }
  }
  addRecord(line) {
    const match = /^(\d+)\s+(-?\d+)\s+(\d+)\/(\d+)\s+(\d+)$/.exec(line);
    if (!match || match.slice(1).some((v) => v.length > 21)) throw new TypeError("invalid encoded packet stats");
    const [n, pts, num, den, size] = match.slice(1).map(BigInt);
    if (n !== this.nextRecord) throw new Error("encoded packet record discontinuity");
    if (num <= 0n || den <= 0n) throw new TypeError("packet timebase must be positive");
    if (size <= 0n || size > BigInt(this.maxFrameBytes)) throw new RangeError("encoded packet size outside limit");
    const ticks = pts * num * 90000n;
    if (ticks % den !== 0n) throw new Error("packet PTS cannot be represented exactly at 90 kHz");
    if (this.records.length >= this.maxPendingFrames) throw new RangeError("pending encoded packet limit exceeded");
    this.records.push({ size: Number(size), pts90k: ticks / den });
    this.nextRecord++;
  }
  drain() {
    while (this.records.length && this.available >= this.records[0].size) {
      const record = this.records.shift();
      const data = Buffer.allocUnsafe(record.size);
      let copied = 0;
      while (copied < data.length) {
        const chunk = this.chunks[0];
        const count = Math.min(chunk.length - this.offset, data.length - copied);
        chunk.copy(data, copied, this.offset, this.offset + count);
        copied += count;
        this.offset += count;
        if (this.offset === chunk.length) { this.chunks.shift(); this.offset = 0; }
      }
      this.available -= record.size;
      this.onFrame({ data, pts90k: record.pts90k });
    }
  }
  assertOpen() { if (this.ended) throw new Error("encoded frame reader has ended"); }
  end() {
    this.assertOpen();
    this.ended = true;
    if (this.statsRemainder.length) { this.addRecord(this.statsRemainder.trim()); this.statsRemainder = ""; }
    this.drain();
    if (this.records.length || this.available) throw new Error("encoded packet/data length mismatch at EOF");
  }
}
