#include "freshness.h"
#include <assert.h>
static void be(uint8_t *p, uint64_t v, unsigned n) {
  while (n) {
    p[--n] = (uint8_t)v;
    v >>= 8;
  }
}
int main(void) {
  uint8_t payload[48] = {0};
  memcpy(payload, "EWFRESHNESS00001", 16);
  payload[16] = 1;
  payload[17] = 1;
  be(payload + 18, 500, 2);
  memcpy(payload + 20, "epoch123", 8);
  be(payload + 28, 123, 4);
  be(payload + 32, 1000100, 8);
  be(payload + 40, 7, 8);
  FreshStamp s;
  assert(fresh_payload(payload, 48, &s));
  assert(s.frame == 7 && s.source == 123);
  uint8_t nal[128] = {0, 0, 0, 1, 6, 5, 48};
  size_t len = 7;
  unsigned zeros = 0;
  for (unsigned i = 0; i < 48; i++) {
    if (zeros >= 2 && payload[i] <= 3) {
      nal[len++] = 3;
      zeros = 0;
    }
    nal[len++] = payload[i];
    zeros = payload[i] == 0 ? zeros + 1 : 0;
  }
  nal[len++] = 128;
  FreshStamp decoded;
  assert(fresh_extract(nal, len, 0, &decoded));
  assert(decoded.ingress_us == 1000100 && decoded.frame == 7);
  assert(!fresh_extract(nal, 12, 0, &decoded));
  uint8_t clock[48] = {0};
  memcpy(clock, "EWCLK001epoch123", 16);
  be(clock + 16, 1000000, 8);
  be(clock + 24, 80000000000ULL, 8);
  be(clock + 32, 80010000000ULL, 8);
  be(clock + 40, 85010000000ULL, 8);
  uint64_t deadline;
  assert(fresh_deadline(&s, clock, 80100000000ULL, &deadline));
  assert(deadline == 80500099900ULL);
  /* Same frame continuously delivered later is stale, regardless of arrival
   * cadence. */
  assert(!fresh_deadline(&s, clock, 80500100000ULL, &deadline));
  /* Expiry decision requires only monotonic now: no incoming frames or Unix
   * clock. */
  assert(80500100001ULL > deadline);
  assert(!fresh_deadline(&s, clock, 86000000000ULL, &deadline));
  /* A frame near lease expiry cannot outlive the clock mapping itself. */
  be(clock + 40, 80110000000ULL, 8);
  assert(fresh_deadline(&s, clock, 80100000000ULL, &deadline));
  assert(deadline == 80110000000ULL);
  assert(!fresh_deadline(&s, clock, 80110000000ULL, &deadline));
  uint64_t lower, upper, uncertainty;
  be(clock + 40, 85010000000ULL, 8);
  assert(fresh_age_bounds(&s, clock, 80600000000ULL, &lower, &upper, &uncertainty));
  assert(lower == 589899900ULL && upper == 599900100ULL && uncertainty == 10000200ULL);
  /* Observe reports expired ages rather than enforcing or clamping to 500ms. */
  assert(upper > 500000000ULL);
  assert(!fresh_age_bounds(&s, clock, 86000000000ULL, &lower, &upper, &uncertainty));
  clock[8] ^= 1;
  assert(!fresh_deadline(&s, clock, 80100000000ULL, &deadline));
  assert(!fresh_age_bounds(&s, clock, 80100000000ULL, &lower, &upper, &uncertainty));
  return 0;
}
