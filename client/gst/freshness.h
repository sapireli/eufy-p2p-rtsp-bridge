#ifndef EUFY_FRESHNESS_H
#define EUFY_FRESHNESS_H
#include <stddef.h>
#include <stdint.h>
#include <string.h>
typedef struct {
  uint8_t epoch[8];
  uint16_t max_age_ms;
  uint32_t source;
  uint64_t ingress_us, frame;
} FreshStamp;
/* Last observed age is bound to its own picture identity, never a histogram's
 * most recent frame label. Missing/unknown bounds explicitly clear validity. */
typedef struct {
  FreshStamp stamp;
  uint64_t mono_ns, lower_ns, upper_ns;
  int valid;
} FreshAgeSample;
static inline void fresh_age_sample(FreshAgeSample *sample,const FreshStamp *stamp,
    uint64_t now,uint64_t lower,uint64_t upper,int valid) {
  memset(sample,0,sizeof(*sample));sample->mono_ns=now;
  if(stamp)sample->stamp=*stamp;
  sample->valid=stamp && valid && lower<=upper;
  if(sample->valid){sample->lower_ns=lower;sample->upper_ns=upper;}
}
static uint64_t fresh_be(const uint8_t *p, unsigned n) {
  uint64_t v = 0;
  while (n--)
    v = (v << 8) | *p++;
  return v;
}
static int fresh_payload(const uint8_t *p, size_t n, FreshStamp *out) {
  static const uint8_t uuid[] = "EWFRESHNESS00001";
  if (n != 48 || memcmp(p, uuid, 16) || p[16] != 1 || p[17] != 1 ||
      fresh_be(p + 18, 2) == 0)
    return 0;
  memcpy(out->epoch, p + 20, 8);
  out->max_age_ms = (uint16_t)fresh_be(p + 18, 2);
  out->source = (uint32_t)fresh_be(p + 28, 4);
  out->ingress_us = fresh_be(p + 32, 8);
  out->frame = fresh_be(p + 40, 8);
  return 1;
}
/* Annex B only: h264parse/h265parse are explicitly negotiated byte-stream, AU
 * aligned. */
static int fresh_extract(const uint8_t *p, size_t n, int hevc,
                         FreshStamp *out) {
  for (size_t i = 0; i + 4 < n; i++) {
    if (p[i] || p[i + 1] || p[i + 2] != 1)
      continue;
    size_t start = i + 3, end = start;
    while (end < n) {
      if (end + 2 < n && !p[end] && !p[end + 1] && p[end + 2] == 1)
        break;
      end++;
    }
    unsigned type = hevc ? ((p[start] >> 1) & 63) : (p[start] & 31);
    size_t head = hevc ? 2 : 1;
    if (type != (hevc ? 39 : 6) || end - start < head)
      continue;
    uint8_t rbsp[512];
    size_t len = 0;
    unsigned zeros = 0;
    for (size_t j = start + head; j < end && len < sizeof(rbsp); j++) {
      if (zeros >= 2 && p[j] == 3) {
        zeros = 0;
        continue;
      }
      rbsp[len++] = p[j];
      zeros = p[j] == 0 ? zeros + 1 : 0;
    }
    size_t j = 0;
    while (j < len) {
      unsigned t = 0, s = 0;
      while (j < len && rbsp[j] == 255) {
        t += 255;
        j++;
      }
      if (j >= len)
        break;
      t += rbsp[j++];
      while (j < len && rbsp[j] == 255) {
        s += 255;
        j++;
      }
      if (j >= len)
        break;
      s += rbsp[j++];
      if (s > len - j)
        break;
      if (t == 5 && fresh_payload(rbsp + j, s, out))
        return 1;
      j += s;
    }
  }
  return 0;
}
/* Earliest possible local ingress: request start + (ingress - server sample).
 * Unix/wall clocks never enter this calculation. */
static int fresh_deadline(const FreshStamp *s, const uint8_t c[48],
                          uint64_t now, uint64_t *deadline) {
  if (memcmp(c, "EWCLK001", 8) || memcmp(c + 8, s->epoch, 8) ||
      now > fresh_be(c + 40, 8))
    return 0;
  uint64_t server = fresh_be(c + 16, 8), t0 = fresh_be(c + 24, 8),
           t1 = fresh_be(c + 32, 8), earliest;
  if (t1 < t0 || t1 - t0 > 1000000000ULL)
    return 0;
  if (s->ingress_us >= server) {
    uint64_t delta = s->ingress_us - server;
    if (delta > UINT64_MAX / 1000 || UINT64_MAX - t0 < delta * 1000)
      return 0;
    earliest = t0 + delta * 1000;
  } else {
    uint64_t delta = server - s->ingress_us;
    if (delta > t0 / 1000)
      return 0;
    earliest = t0 - delta * 1000;
  }
  /* A stamp cannot arrive before its latest possible mapped ingress. Reject
   * implausible future domains. */
  /* Relative host clocks may differ by1000ppm (two opposing500ppm slews).
   * Delta is in microseconds, so its1000ppm projection error is delta ns. */
  uint64_t projection_error =
      s->ingress_us >= server ? s->ingress_us - server : server - s->ingress_us;
  if (earliest < projection_error)
    return 0;
  earliest -= projection_error;
  if (earliest > now)
    return 0;
  *deadline = earliest + (uint64_t)s->max_age_ms * 1000000;
  uint64_t valid_until = fresh_be(c + 40, 8);
  if (*deadline > valid_until)
    *deadline = valid_until;
  return now < *deadline;
}
/* Observe-only age interval. Unlike fresh_deadline(), this remains valid for an
 * expired picture and never clamps the age to its presentation budget. */
static int fresh_age_bounds(const FreshStamp *s, const uint8_t c[48],
                            uint64_t now, uint64_t *lower,
                            uint64_t *upper, uint64_t *uncertainty) {
  if (memcmp(c, "EWCLK001", 8) || memcmp(c + 8, s->epoch, 8) ||
      now > fresh_be(c + 40, 8)) return 0;
  uint64_t server = fresh_be(c + 16, 8), t0 = fresh_be(c + 24, 8),
           t1 = fresh_be(c + 32, 8), earliest, latest;
  if (t1 < t0 || t1 - t0 > 1000000000ULL) return 0;
  uint64_t delta = s->ingress_us >= server ? s->ingress_us - server : server - s->ingress_us;
  if (delta > UINT64_MAX / 1000) return 0;
  if (s->ingress_us >= server) {
    if (UINT64_MAX - t1 < delta * 1000) return 0;
    earliest = t0 + delta * 1000; latest = t1 + delta * 1000;
  } else {
    if (delta * 1000 > t0) return 0;
    earliest = t0 - delta * 1000; latest = t1 - delta * 1000;
  }
  /* Same conservative relative-clock slew allowance as enforcement. */
  if (earliest < delta || UINT64_MAX - latest < delta) return 0;
  earliest -= delta; latest += delta;
  if (earliest > now) return 0;
  *upper = now - earliest;
  *lower = now > latest ? now - latest : 0;
  *uncertainty = latest - earliest;
  return 1;
}
#endif
