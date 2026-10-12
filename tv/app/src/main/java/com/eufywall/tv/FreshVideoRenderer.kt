package com.eufywall.tv

import android.graphics.SurfaceTexture
import android.opengl.EGL14
import android.opengl.EGLExt
import android.opengl.EGLConfig
import android.opengl.GLES11Ext
import android.opengl.GLES20
import android.os.Handler
import android.os.HandlerThread
import android.view.Surface
import android.util.Log
import java.nio.ByteBuffer
import java.nio.ByteOrder
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.atomic.AtomicLong

/** Owned GPU admission gate. Codec callbacks are diagnostic, never frame identity.
 * SurfaceTexture timestamps identify the exact acquired buffer; only that buffer's
 * stamp may authorize drawing. Hardware decode and external-texture sampling avoid
 * CPU pixel copies. The renderer does not add a frame queue or pacing delay.
 */
internal class FreshVideoRenderer(
    output: Surface, width: Int, height: Int,
    private val cameraSn: String,
    private val maxAgeMs: Int,
    private val clockSample: (Long) -> BridgeClockSample?,
    private val onPresented: (FrameStamp, Long, Long) -> Unit,
    private val onState: (String?) -> Unit,
    private val onFailure: (String) -> Unit,
) {
    private val thread = HandlerThread("EufyFreshGL").apply { start() }
    private val handler = Handler(thread.looper)
    private val pictures = SurfaceFrameRegistry()
    @Volatile private var closed = false
    private var display = EGL14.EGL_NO_DISPLAY
    private var context = EGL14.EGL_NO_CONTEXT
    private var window = EGL14.EGL_NO_SURFACE
    private lateinit var texture: SurfaceTexture
    lateinit var inputSurface: Surface
        private set
    private var program = 0
    private var acquired: SurfacePicture? = null
    private var lastFrameId = 0L
    private var lastEpoch = ""
    private var visibleDeadlineNs = 0L
    private var state: String? = "Timing unavailable"
    private var drawCause = "startup"
    private val blankCounts = ConcurrentHashMap<String,AtomicLong>()
    fun diagnostics() = blankCounts.entries.sortedBy { it.key }.joinToString(",") { "${it.key}:${it.value.get()}" }
    private fun logBlank(reason:String,now:Long,picture:SurfacePicture?,clock:BridgeClockSample?,until:Long?,desired:Long) {
        blankCounts.computeIfAbsent(reason) { AtomicLong() }.incrementAndGet()
        Log.i("EufyFreshness", "$cameraSn blank reason=$reason cause=$drawCause atNs=$now frameId=${picture?.stamp?.frameId ?: -1} ingressMonoUs=${picture?.stamp?.ingressMonoUs ?: -1} ageUpperMs=${picture?.let { clock?.ageUpperNs(it.stamp,now)?.div(1_000_000) } ?: -1} clockRttMs=${clock?.let { (it.responseNs-it.requestStartNs)/1_000_000 } ?: -1} clockLeaseNs=${clock?.expiresNs ?: -1} epochMatch=${picture!=null && clock?.epoch==picture.stamp.epoch} previousDeadlineNs=$visibleDeadlineNs targetNs=${picture?.targetNs ?: -1} desiredNs=$desired deadlineNs=${until ?: -1} budgetMs=$maxAgeMs captureClockValidated=false")
    }
    private var viewportWidth = width
    private var viewportHeight = height
    private val transform = FloatArray(16)
    private val vertices = ByteBuffer.allocateDirect(16 * 4).order(ByteOrder.nativeOrder()).asFloatBuffer().apply {
        put(floatArrayOf(-1f,-1f,0f,0f, 1f,-1f,1f,0f, -1f,1f,0f,1f, 1f,1f,1f,1f)); position(0)
    }
    private val draw = Runnable { guarded { drawFrame() } }
    init {
        val ready = CountDownLatch(1)
        var error: Throwable? = null
        handler.post {
            try {
                display = EGL14.eglGetDisplay(EGL14.EGL_DEFAULT_DISPLAY)
                check(EGL14.eglInitialize(display, IntArray(2), 0, IntArray(2), 1)) { "EGL initialization failed" }
                val configs = arrayOfNulls<EGLConfig>(1)
                check(EGL14.eglChooseConfig(display, intArrayOf(EGL14.EGL_RENDERABLE_TYPE,EGL14.EGL_OPENGL_ES2_BIT,EGL14.EGL_SURFACE_TYPE,EGL14.EGL_WINDOW_BIT,EGL14.EGL_RED_SIZE,8,EGL14.EGL_GREEN_SIZE,8,EGL14.EGL_BLUE_SIZE,8,EGL14.EGL_NONE),0,configs,0,1,IntArray(1),0))
                context = EGL14.eglCreateContext(display,configs[0],EGL14.EGL_NO_CONTEXT,intArrayOf(EGL14.EGL_CONTEXT_CLIENT_VERSION,2,EGL14.EGL_NONE),0)
                window = EGL14.eglCreateWindowSurface(display,configs[0],output,intArrayOf(EGL14.EGL_NONE),0)
                check(EGL14.eglMakeCurrent(display,window,window,context)) { "EGL surface unavailable" }
                val name = IntArray(1); GLES20.glGenTextures(1,name,0)
                GLES20.glBindTexture(GLES11Ext.GL_TEXTURE_EXTERNAL_OES,name[0])
                GLES20.glTexParameteri(GLES11Ext.GL_TEXTURE_EXTERNAL_OES,GLES20.GL_TEXTURE_MIN_FILTER,GLES20.GL_LINEAR)
                GLES20.glTexParameteri(GLES11Ext.GL_TEXTURE_EXTERNAL_OES,GLES20.GL_TEXTURE_MAG_FILTER,GLES20.GL_LINEAR)
                GLES20.glTexParameteri(GLES11Ext.GL_TEXTURE_EXTERNAL_OES,GLES20.GL_TEXTURE_WRAP_S,GLES20.GL_CLAMP_TO_EDGE)
                GLES20.glTexParameteri(GLES11Ext.GL_TEXTURE_EXTERNAL_OES,GLES20.GL_TEXTURE_WRAP_T,GLES20.GL_CLAMP_TO_EDGE)
                texture = SurfaceTexture(name[0])
                texture.setDefaultBufferSize(width.coerceAtLeast(1),height.coerceAtLeast(1))
                inputSurface = Surface(texture)
                program = GLES20.glCreateProgram()
                fun shader(type:Int, source:String):Int {
                    val s=GLES20.glCreateShader(type); GLES20.glShaderSource(s,source); GLES20.glCompileShader(s)
                    val ok=IntArray(1); GLES20.glGetShaderiv(s,GLES20.GL_COMPILE_STATUS,ok,0); check(ok[0]!=0) { GLES20.glGetShaderInfoLog(s) }; return s
                }
                GLES20.glAttachShader(program,shader(GLES20.GL_VERTEX_SHADER,"attribute vec2 position; attribute vec2 uv; uniform mat4 transform; varying vec2 tex; void main(){gl_Position=vec4(position,0.0,1.0); tex=(transform*vec4(uv,0.0,1.0)).xy;}"))
                GLES20.glAttachShader(program,shader(GLES20.GL_FRAGMENT_SHADER,"#extension GL_OES_EGL_image_external : require\nprecision mediump float; uniform samplerExternalOES image; varying vec2 tex; void main(){gl_FragColor=texture2D(image,tex);}"))
                GLES20.glLinkProgram(program)
                val ok=IntArray(1); GLES20.glGetProgramiv(program,GLES20.GL_LINK_STATUS,ok,0); check(ok[0]!=0) { GLES20.glGetProgramInfoLog(program) }
                texture.setOnFrameAvailableListener({ guarded {
                    texture.updateTexImage(); texture.getTransformMatrix(transform)
                    acquired = pictures.acquire(texture.timestamp)
                    if(Log.isLoggable("EufyFrameTiming",Log.DEBUG)) Log.d("EufyFrameTiming","GPU latched targetNs=${texture.timestamp} atNs=${System.nanoTime()} frameId=${acquired?.stamp?.frameId ?: -1}")
                    drawCause="frame"; handler.removeCallbacks(draw); handler.post(draw)
                } }, handler)
                drawFrame()
            } catch (t: Throwable) { error=t } finally { ready.countDown() }
        }
        if(!ready.await(3,TimeUnit.SECONDS)) { close(); throw IllegalStateException("GPU renderer startup timed out") }
        error?.let { close(); throw IllegalStateException("GPU freshness renderer unavailable",it) }
    }
    /** Register before release: decoder may produce an image immediately. */
    fun register(stamp:FrameStamp, targetNs:Long):Long = pictures.register(stamp,targetNs)
    fun resize(width:Int,height:Int) { handler.post { viewportWidth=width; viewportHeight=height; drawCause="resize"; handler.removeCallbacks(draw); handler.post(draw) } }
    private fun guarded(action:()->Unit) {
        if(closed)return
        try { action() } catch(t:Throwable) { onFailure(t.message ?: "GPU render failed"); close() }
    }
    private fun drawFrame() {
        if(closed)return
        val now=System.nanoTime(); val picture=acquired
        val clock=clockSample(now)
        val until=picture?.let { clock?.deadline(it.stamp,now,maxAgeMs) }
        val ordered=picture!=null && (picture.stamp.epoch!=lastEpoch || picture.stamp.frameId>=lastFrameId)
        val desired=picture?.let { surfacePresentationNs(it.targetNs,visibleDeadlineNs,now,MARGIN_NS) } ?: now
        val valid=ordered && until!=null && desired+MARGIN_NS<until
        // Submit immediately. The output SurfaceView's native buffer timestamp
        // owns VSYNC pacing. Deferring CPU draws for every new future picture
        // starves presentation when pictures arrive faster than the pacing budget.
        GLES20.glViewport(0,0,viewportWidth,viewportHeight)
        GLES20.glClearColor(0f,0f,0f,1f); GLES20.glClear(GLES20.GL_COLOR_BUFFER_BIT)
        if(valid) {
            GLES20.glUseProgram(program)
            val position=GLES20.glGetAttribLocation(program,"position"); val uv=GLES20.glGetAttribLocation(program,"uv")
            vertices.position(0); GLES20.glVertexAttribPointer(position,2,GLES20.GL_FLOAT,false,16,vertices); GLES20.glEnableVertexAttribArray(position)
            vertices.position(2); GLES20.glVertexAttribPointer(uv,2,GLES20.GL_FLOAT,false,16,vertices); GLES20.glEnableVertexAttribArray(uv)
            GLES20.glUniformMatrix4fv(GLES20.glGetUniformLocation(program,"transform"),1,false,transform,0)
            GLES20.glDrawArrays(GLES20.GL_TRIANGLE_STRIP,0,4)
        }
        // Recheck after drawing and before submission; use the previous picture's
        // cutoff until replacement is scheduled no later than that cutoff.
        val swapNow=System.nanoTime()
        val swapClock=clockSample(swapNow)
        val swapUntil=picture?.let { swapClock?.deadline(it.stamp,swapNow,maxAgeMs) }
        val presentNs=picture?.let { surfacePresentationNs(it.targetNs,visibleDeadlineNs,swapNow,MARGIN_NS) } ?: swapNow
        val show=valid && swapUntil!=null && presentNs+MARGIN_NS<swapUntil
        if(!show) {
            GLES20.glClear(GLES20.GL_COLOR_BUFFER_BIT)
            val reason=when {
                picture==null -> "missingStamp"
                !ordered -> "frameOrder"
                swapClock==null -> "clockUnavailable"
                swapClock.epoch!=picture.stamp.epoch -> "epochMismatch"
                swapClock.ageUpperNs(picture.stamp,swapNow)==null -> "clockBoundInvalid"
                swapUntil==null -> "ingressExpired"
                else -> "targetTooLate"
            }
            logBlank(reason,swapNow,picture,swapClock,swapUntil,presentNs)
        }
        check(EGLExt.eglPresentationTimeANDROID(display,window,if(show) presentNs else swapNow)) { "GPU presentation timestamp unavailable" }
        check(EGL14.eglSwapBuffers(display,window)) { "GPU swap failed" }
        val submittedNs=System.nanoTime()
        if(Log.isLoggable("EufyFrameTiming",Log.DEBUG)) Log.d("EufyFrameTiming","GPU swapped atNs=$submittedNs presentNs=${if(show) presentNs else swapNow} frameId=${if(show) picture!!.stamp.frameId else -1} deadlineNs=${swapUntil ?: -1}")
        if(show && submittedNs+MARGIN_NS>=swapUntil!!) {
            // Completion overran admission: clear now instead of certifying a late
            // queued buffer. GPU submission is not physical display confirmation.
            GLES20.glClear(GLES20.GL_COLOR_BUFFER_BIT)
            check(EGLExt.eglPresentationTimeANDROID(display,window,System.nanoTime()))
            check(EGL14.eglSwapBuffers(display,window))
            logBlank("swapOverrun",submittedNs,picture,swapClock,swapUntil,presentNs)
            visibleDeadlineNs=0L
            state="Stale video"; onState(state)
            return
        }
        if(show) {
            val p=picture!!; lastEpoch=p.stamp.epoch; lastFrameId=p.stamp.frameId; visibleDeadlineNs=swapUntil!!
            onPresented(p.stamp,submittedNs,swapUntil)
            if(state!=null) { state=null; onState(null) }
            drawCause="expiry"
            handler.postDelayed(draw,((swapUntil-MARGIN_NS-System.nanoTime())/1_000_000).coerceAtLeast(1))
        } else {
            visibleDeadlineNs=0L
            val message=if(swapClock!=null) "Stale video" else "Timing unavailable"
            if(state!=message) { state=message; onState(message) }
        }
    }
    fun close() {
        if(closed)return
        closed=true; handler.removeCallbacksAndMessages(null)
        handler.post {
            if(::inputSurface.isInitialized) inputSurface.release()
            if(::texture.isInitialized) texture.release()
            EGL14.eglMakeCurrent(display,EGL14.EGL_NO_SURFACE,EGL14.EGL_NO_SURFACE,EGL14.EGL_NO_CONTEXT)
            if(window!=EGL14.EGL_NO_SURFACE) EGL14.eglDestroySurface(display,window)
            if(context!=EGL14.EGL_NO_CONTEXT) EGL14.eglDestroyContext(display,context)
            // EGL display may be shared with other tiles; do not terminate it.
            thread.quitSafely()
        }
    }
    companion object { private const val MARGIN_NS=20_000_000L }
}
