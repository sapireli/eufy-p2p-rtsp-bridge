/** Read only the HEVC SPS general_profile_tier_level prefix, never coded picture bytes. */
export function hevcProfileTierLevel(nal) {
  if (!Buffer.isBuffer(nal) || nal.length < 15 || nal.length > 65535 || (nal[0] & 128) || ((nal[0] >> 1) & 63) !== 33 || !(nal[1] & 7)) return undefined;
  const prefix = Buffer.alloc(13);
  let length = 0, zeros = 0;
  for (let at = 2; at < nal.length && at < 64 && length < prefix.length; at++) {
    const value = nal[at];
    if (zeros >= 2 && value === 3) {
      if (at + 1 >= nal.length || nal[at + 1] > 3) return undefined;
      zeros = 0; continue;
    }
    prefix[length++] = value;
    zeros = value === 0 ? zeros + 1 : 0;
  }
  if (length !== 13 || ((prefix[0] >> 1) & 7) > 6) return undefined;
  const profileSpace = prefix[1] >> 6, profile = prefix[1] & 31, tier = (prefix[1] >> 5) & 1, level = prefix[12];
  if (profileSpace !== 0 || !profile || !level) return undefined;
  return { profile, level, tier };
}
