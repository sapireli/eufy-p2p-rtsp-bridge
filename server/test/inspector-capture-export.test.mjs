import test from "node:test";
import assert from "node:assert/strict";
import vm from "node:vm";
import {exportCaptureRows} from "../scripts/inspector-capture-export.mjs";

function inspector(rows, {failBatch = -1} = {}) {
  const context = vm.createContext({clearTimeout() {}});
  context.__ewbGapCapture = {id: "capture-test", rows};
  let batches = 0, inFlight = 0, maximumInFlight = 0, maximumRows = 0;
  return {context, stats: () => ({batches, maximumInFlight, maximumRows}),
    evaluate: async expression => {
      inFlight++; maximumInFlight = Math.max(maximumInFlight, inFlight);
      try {
        await new Promise(resolve => setImmediate(resolve));
        if (expression.includes(".rows.slice")) {
          if (batches++ === failBatch) throw Error("Simulated inspector disconnect");
        }
        const result = vm.runInContext(expression, context);
        if (Array.isArray(result)) maximumRows = Math.max(maximumRows, result.length);
        return result;
      } finally {inFlight--;}
    },
  };
}

test("large metadata capture exports exact ordered rows in bounded sequential batches with yields", async () => {
  const rows = Array.from({length: 200000}, (_, n) => ({n, phase: "udp-video", bytes: 1032}));
  const mock = inspector(rows); let yields = 0;
  const capture = await exportCaptureRows(mock.evaluate, {captureId: "capture-test", rowCount: rows.length, ended: 10}, {yieldBatch: async () => {yields++;}});
  assert.deepEqual(capture.rows, rows);
  assert.deepEqual(mock.stats(), {batches: 400, maximumInFlight: 1, maximumRows: 500});
  assert.equal(yields, 400);
  assert.equal(mock.context.__ewbGapCapture, undefined);
});

test("failed middle batch deletes cache instead of leaking target memory", async () => {
  const mock = inspector(Array.from({length: 1100}, (_, n) => ({n})), {failBatch: 1});
  await assert.rejects(exportCaptureRows(mock.evaluate, {captureId: "capture-test", rowCount: 1100}, {yieldBatch: async () => {}}), /disconnect/);
  assert.equal(mock.context.__ewbGapCapture, undefined);
});

test("expired/replaced capture is rejected without deleting another observer's cache", async () => {
  const mock = inspector([{n: 0}]); mock.context.__ewbGapCapture.id = "replacement";
  await assert.rejects(exportCaptureRows(mock.evaluate, {captureId: "capture-test", rowCount: 1}), /expired/);
  assert.equal(mock.context.__ewbGapCapture.id, "replacement");
});

test("partial batch is rejected and cache cleaned", async () => {
  const mock = inspector([{n: 0}]);
  await assert.rejects(exportCaptureRows(mock.evaluate, {captureId: "capture-test", rowCount: 2}), /Incomplete/);
  assert.equal(mock.context.__ewbGapCapture, undefined);
});
