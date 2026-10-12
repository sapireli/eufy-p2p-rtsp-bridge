package com.alexvas.rtsp

import com.alexvas.rtsp.codec.FrameQueue
import com.alexvas.rtsp.codec.VideoCodecType
import com.alexvas.rtsp.codec.VideoFrameQueue
import java.io.BufferedOutputStream
import java.io.ByteArrayOutputStream
import java.io.OutputStream
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import org.junit.Assert.*
import org.junit.Test

class RtspBackpressureAuditTest {
    @Test fun queueHandoffDoesNotHoldProducerLockWhileConsumerWorkIsPaused() {
        val queue = VideoFrameQueue(1)
        val frame = FrameQueue.VideoFrame(VideoCodecType.H264,false,byteArrayOf(1),0,1,1)
        assertTrue(queue.push(frame))
        val consumerEntered = CountDownLatch(1)
        val releaseConsumer = CountDownLatch(1)
        val pool = Executors.newFixedThreadPool(2)
        try {
            val consumer = pool.submit {
                assertSame(frame,queue.pop(100))
                consumerEntered.countDown()
                assertTrue(releaseConsumer.await(2,TimeUnit.SECONDS))
            }
            assertTrue(consumerEntered.await(1,TimeUnit.SECONDS))
            // Simulates a paused codec stage after pop, not an actual MediaCodec test.
            assertTrue(pool.submit<Boolean> { queue.push(frame) }.get(1,TimeUnit.SECONDS))
            assertFalse(pool.submit<Boolean> { queue.push(frame) }.get(1,TimeUnit.SECONDS))
            assertEquals(1,queue.size)
            releaseConsumer.countDown()
            consumer.get(1,TimeUnit.SECONDS)
        } finally {
            releaseConsumer.countDown();pool.shutdownNow()
        }
    }

    @Test fun actualRtspCommandFlushCanBlockCallingThreadUntilTransportWriteCompletes() {
        val enteredWrite = CountDownLatch(1)
        val releaseWrite = CountDownLatch(1)
        val bytes = ByteArrayOutputStream()
        val transport = object : OutputStream() {
            override fun write(value: Int) = write(byteArrayOf(value.toByte()),0,1)
            override fun write(data: ByteArray,offset: Int,length: Int) {
                enteredWrite.countDown()
                check(releaseWrite.await(2,TimeUnit.SECONDS))
                bytes.write(data,offset,length)
            }
        }
        val buffered = BufferedOutputStream(transport)
        val method = RtspClient::class.java.getDeclaredMethod("sendSimpleCommand",
            String::class.java,OutputStream::class.java,String::class.java,Int::class.javaPrimitiveType,
            String::class.java,String::class.java,String::class.java).apply { isAccessible=true }
        val pool=Executors.newSingleThreadExecutor()
        try {
            val command = pool.submit {
                method.invoke(null,"OPTIONS",buffered,"rtsp://127.0.0.1/test",1,"audit",null,null)
            }
            assertTrue(enteredWrite.await(1,TimeUnit.SECONDS))
            assertFalse(command.isDone) // A read loop calling this synchronously cannot continue yet.
            releaseWrite.countDown()
            command.get(1,TimeUnit.SECONDS)
            assertEquals("OPTIONS rtsp://127.0.0.1/test RTSP/1.0\r\nCSeq: 1\r\nUser-Agent: audit\r\n\r\n",bytes.toString("UTF-8"))
        } finally {
            releaseWrite.countDown();pool.shutdownNow()
        }
    }
}
