package com.alexvas.rtsp.parser;

/** Bounded metadata only; elapsed read time includes OS scheduling, not just network wait. */
public final class RtpReadObservation {
    public final long rtpTimestamp, firstHeaderStartNs, lastPayloadEndNs, auCallbackNs;
    public final long headerReadNs, payloadReadNs, parserBeforeCallbackNs;
    public final int packetCount, networkThreadId;
    public volatile long listenerElapsedNs, listenerCompletedNs;
    private RtpReadObservation(Accumulator a, long callbackNs) {
        rtpTimestamp=a.timestamp; firstHeaderStartNs=a.firstHeaderStartNs;
        lastPayloadEndNs=a.lastPayloadEndNs; auCallbackNs=callbackNs;
        headerReadNs=a.headerReadNs; payloadReadNs=a.payloadReadNs;
        parserBeforeCallbackNs=a.parserNs+Math.max(0, callbackNs-a.packetParserStartNs-a.packetListenerNs);
        packetCount=a.packetCount; networkThreadId=a.threadId;
        listenerElapsedNs=a.listenerNs;
    }
    public static final class Accumulator {
        private long timestamp=Long.MIN_VALUE,firstHeaderStartNs,lastPayloadEndNs,headerReadNs,payloadReadNs,parserNs,listenerNs,packetParserStartNs,packetListenerNs;
        private int packetCount;
        private final int threadId;
        public Accumulator(int threadId) { this.threadId=threadId; }
        public void packet(long timestamp,long headerStart,long headerEnd,long payloadEnd) {
            if (this.timestamp!=timestamp) {
                this.timestamp=timestamp;firstHeaderStartNs=headerStart;headerReadNs=payloadReadNs=parserNs=listenerNs=0;packetCount=0;
            }
            lastPayloadEndNs=payloadEnd;headerReadNs+=Math.max(0,headerEnd-headerStart);
            payloadReadNs+=Math.max(0,payloadEnd-headerEnd);packetCount++;
            packetParserStartNs=payloadEnd;packetListenerNs=0;
        }
        public RtpReadObservation snapshot(long callbackNs) { return new RtpReadObservation(this,callbackNs); }
        public void listenerCompleted(RtpReadObservation snapshot,long completedNs) {
            long elapsed=Math.max(0,completedNs-snapshot.auCallbackNs);
            listenerNs+=elapsed;packetListenerNs+=elapsed;
            snapshot.listenerElapsedNs=listenerNs;snapshot.listenerCompletedNs=completedNs;
        }
        public void packetCompleted(long completedNs) {
            parserNs+=Math.max(0,completedNs-packetParserStartNs-packetListenerNs);
        }
    }
}
