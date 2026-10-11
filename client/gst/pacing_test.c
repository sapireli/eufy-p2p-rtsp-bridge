#include "pacing.h"
#include <assert.h>
int main(void) {
  PacingClock clock = {0}; int64_t target;
  assert(pacing_target(&clock, 0, 0, 0, &target) && target == PACING_BOUND_NS);
  /* Ten seconds of nominal video without accumulating presentation delay. */
  for (int i = 1; i < 150; i++) {
    int64_t pts = i * PACING_BOUND_NS, now = pts + (i % 4) * INT64_C(5000000);
    assert(pacing_target(&clock, pts, now, 0, &target));
    assert(target <= now + PACING_BOUND_NS && target >= now);
  }
  /* An hour of source clock drift plus periodic delivery jitter stays bounded. */
  memset(&clock, 0, sizeof(clock));
  for (int i = 0; i < 54000; i++) {
    int64_t pts = i * PACING_BOUND_NS, now = pts + i * INT64_C(10000) + (i % 7) * INT64_C(3000000);
    assert(pacing_target(&clock, pts, now, 0, &target));
    assert(target <= now + PACING_BOUND_NS && target >= now);
  }
  /* Forward source-clock jump is rebased, never a multi-second future wait. */
  memset(&clock, 0, sizeof(clock));
  assert(pacing_target(&clock, 0, 0, 0, &target));
  assert(pacing_target(&clock, INT64_C(20000000000), INT64_C(10000000000), 0, &target));
  assert(target <= INT64_C(10000000000) + PACING_BOUND_NS);
  memset(&clock, 0, sizeof(clock));
  assert(pacing_target(&clock, 0, 0, 0, &target));
  /* A half-second pause followed by normal arrivals must not freeze output until
   * the earlier two-second minimum ages out. Every newest picture renders now. */
  for (int i = 1; i <= 30; i++) {
    int64_t pts = i * PACING_BOUND_NS, now = pts + INT64_C(518000000);
    assert(pacing_target(&clock, pts, now, 0, &target));
    assert(target >= now && target <= now + PACING_BOUND_NS);
    if (i <= 22) assert(target == now);
  }
  /* Out-of-order and repeated pictures cannot supersede newer output. */
  assert(!pacing_target(&clock, 30 * PACING_BOUND_NS, INT64_C(3000000000), 0, &target));
  assert(!pacing_target(&clock, 29 * PACING_BOUND_NS, INT64_C(3000000000), 0, &target));
  /* An unclocked picture cannot advance source ordering into the local domain. */
  assert(pacing_target(&clock, -1, INT64_C(9000000000), 0, &target) && target == INT64_C(9000000000));
  assert(pacing_target(&clock, 31 * PACING_BOUND_NS, INT64_C(9000000000), 0, &target));
  /* Only an explicit discontinuity admits a source timestamp restart. */
  assert(pacing_target(&clock, 0, INT64_C(300000000), 1, &target));
  return 0;
}
