#ifndef EUFY_PACING_H
#define EUFY_PACING_H
#include <stdint.h>
#include <string.h>
#define PACING_WINDOW_NS INT64_C(2000000000)
#define PACING_BOUND_NS INT64_C(67000000)
typedef struct { int64_t at, offset; } PacingSample;
typedef struct {
  PacingSample samples[512];
  unsigned next, count;
  int64_t last_pts;
  int initialized;
} PacingClock;
/* Reject superseded timestamps, never fresh pictures just because delivery paused.
 * The bounded raw queue replaces pictures when output is blocked. */
/* arrival is measured before the decoded queue; now is admission time. Mixing
 * them teaches the rolling clock its own sink waits as upstream latency. */
static int pacing_target_sampled(PacingClock *clock, int64_t pts, int64_t arrival,
    int64_t now, int reset, int64_t *target) {
  if (reset) memset(clock, 0, sizeof(*clock));
  /* Missing source PTS is immediate output, not a local-clock sample that could
   * poison the source timestamp ordering when valid media timestamps resume. */
  if (pts < 0) { *target = now; return 1; }
  if (clock->initialized && pts <= clock->last_pts) return 0;
  clock->initialized = 1;
  clock->last_pts = pts;
  clock->samples[clock->next] = (PacingSample){arrival, arrival - pts};
  clock->next = (clock->next + 1) % 512;
  if (clock->count < 512) clock->count++;
  int64_t offset = arrival - pts;
  for (unsigned i = 0; i < clock->count; i++)
    if (now - clock->samples[i].at <= PACING_WINDOW_NS && clock->samples[i].offset < offset)
      offset = clock->samples[i].offset;
  *target = pts + offset + PACING_BOUND_NS;
  if (*target > now + PACING_BOUND_NS) *target = now + PACING_BOUND_NS;
  /* A source pause must not become a longer freeze while its old clock offset
   * ages out. Render the newest late picture now; retain the drift estimate. */
  if (*target < now) *target = now;
  return 1;
}
static inline int pacing_target(PacingClock *clock, int64_t pts, int64_t now, int reset, int64_t *target) {
  return pacing_target_sampled(clock,pts,now,now,reset,target);
}
#endif
