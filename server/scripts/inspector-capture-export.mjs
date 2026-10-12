// Export a completed metadata capture in bounded inspector responses. Hooks must
// already be restored before calling this; the cache has its own expiry guard.
export async function exportCaptureRows(evaluate, header, {
  batchSize = 500, yieldBatch = () => new Promise(resolve => setTimeout(resolve, 10)),
} = {}) {
  if (!Number.isInteger(batchSize) || batchSize < 1 || batchSize > 500) throw Error("Invalid export batch size");
  if (!header || typeof header.captureId !== "string" || !Number.isInteger(header.rowCount)
      || header.rowCount < 0 || header.rowCount > 200000) throw Error("Invalid capture header");
  const captureId = JSON.stringify(header.captureId), rows = [];
  try {
    for (let offset = 0; offset < header.rowCount; offset += batchSize) {
      const count = Math.min(batchSize, header.rowCount - offset);
      const batch = await evaluate(`(()=>{const c=globalThis.__ewbGapCapture;if(!c||c.id!==${captureId})throw Error('Capture cache expired');return c.rows.slice(${offset},${offset + count});})()`);
      if (!Array.isArray(batch) || batch.length !== count) throw Error("Incomplete capture export");
      rows.push(...batch);
      // Let the target event loop run between serialization requests; this is
      // still instrumentation and does not establish zero observer overhead.
      await yieldBatch();
    }
    return { ...header, rows };
  } finally {
    await clearCaptureCache(evaluate, header.captureId).catch(() => {});
  }
}

export function clearCaptureCache(evaluate, captureId) {
  return evaluate(`(()=>{const c=globalThis.__ewbGapCapture;if(c?.id===${JSON.stringify(captureId)}){clearTimeout(c.guard);delete globalThis.__ewbGapCapture;}return true;})()`);
}
