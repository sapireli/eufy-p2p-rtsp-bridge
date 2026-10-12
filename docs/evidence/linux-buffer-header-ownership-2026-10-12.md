# Pacer buffer ownership and hardware pool progress

The generic in-place GStreamer transform may copy shared `NO_SHARE` raw memory
when it needs to edit timestamps. The pacer now creates an independent header
with references to the **same** GstMemory and GstParentBufferMeta retaining the
original pooled buffer. No pixel mapping or copy is performed. A writable input
is reused directly, without increasing its buffer reference count.

This distinction follows [GstBaseTransform source](https://raw.githubusercontent.com/GStreamer/gstreamer/1.26.2/subprojects/gstreamer/libs/gst/base/gstbasetransform.c):
when output and input are the same pointer, generate_output does not unref the
input separately. Returning an additional reference leaks it. The standard
prepare implementation assigns the original pointer without a new reference.
[GstParentBufferMeta](https://gstreamer.freedesktop.org/documentation/gstreamer/gstbuffer.html)
retains pooled parent ownership until the independent child header is released.

## Measurements

- A retained NO_SHARE native fixture through the former generic preparation
  changed memory identity. The candidate retains identical GstMemory, independent
  timestamps and metadata, and retains the parent until the child is released.
- Actual Pi hardware startup buffers were writable, input reference count one,
  dmabuf memory flags numerically one, and retained identical memory through the
  original preparation. This **does not prove copying caused actual stutter**.
- An initial new writable-header implementation erroneously returned an extra
  reference. Two live attempts stopped after exactly 32 KMS returns per decoder;
  removing a synchronous diagnostic query did not change the result and falsified
  that earlier hypothesis. A native ownership probe saw 64 inputs and no buffer
  finalizations after teardown. That initial fixture also emitted byte-segment
  timestamp warnings; the final regression uses a proper time segment.
- Corrected preparation passed the time-segment regression: 64 writable inputs,
  64 finalizations after EOS and teardown. The retained NO_SHARE parent-lifetime
  fixture also passed.
- Corrected hardware plugin SHA-256
  `bb897df6853083d9479941c37d68f0735fc689a4913635de4639a358164cd15a`
  completed 45.078 actual-frame-gated seconds, without restarting either child:
  Front Door 508 KMS returns and Garage 377, both advancing beyond 32. Hardware
  decoder output counts were 688 and 466. Other experimental pacing changes were
  present, so this establishes ownership progress, not their performance benefit.

The separate Garage receive backlog was still present: ingress-age upper bound
already reached 17.326 seconds at depay and 17.507 seconds at KMS. This ownership
change does **not** claim to fix that latency. Physical scanout is unverified.
The temporary test restored TV OFF and service inactive/disabled, with the runtime
override removed. No permanent power or service policy was changed.

## Reproduction

Compile `pacing_memory_probe.c` using the GStreamer base development package;
run with the candidate plugin directory in `GST_PLUGIN_PATH`:

```sh
./pacing-memory-probe --writable-ownership
./pacing-memory-probe --require-identity
```

The first checks every writable buffer is released. The second retains a shared
NO_SHARE input and child output, checks exact GstMemory identity, independent
header timestamps and metadata, then verifies the parent survives until the last
child reference is released. Neither test reads camera pixels.
