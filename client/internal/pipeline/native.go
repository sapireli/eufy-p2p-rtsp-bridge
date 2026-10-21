package pipeline

import (
	"eufy-wall/internal/config"
	"eufy-wall/internal/freshness"
	"eufy-wall/internal/layout"
	"eufy-wall/internal/rendition"
	"net/url"
	"strconv"
)

// Selection uses only a factory actually probed on this host. Explicit codec or
// URL choices remain authoritative. A failing native plan gets one compatible
// fallback; no software HEVC attempt is manufactured by this preference.
func selectNative(c *config.Config, tiles []layout.Placed, caps Caps) ([]layout.Placed, Caps, bool) {
	if caps.NativeDecoder == "" || len(caps.NativeRenditions) == 0 {
		return tiles, caps, false
	}
	switch caps.NativeDecoder {
	case "v4l2h265dec", "v4l2slh265dec", "vah265dec":
	default:
		return tiles, caps, false
	}
	selected := append([]layout.Placed(nil), tiles...)
	fresh := make(map[string]freshness.Capability, len(caps.Freshness))
	for sn, f := range caps.Freshness {
		fresh[sn] = f
	}
	overrides := make(map[string]string)
	changed := false
	for i, t := range selected {
		if t.StillURL != "" || (t.Codec != "" && t.Codec != "auto") {
			continue
		}
		var cfg *config.Tile
		if t.Index >= 0 && t.Index < len(c.Tiles) {
			cfg = &c.Tiles[t.Index]
		}
		if specific := c.TileFor(t.Camera); specific != nil {
			cfg = specific
		}
		if cfg != nil && (cfg.URL != "" || (cfg.Codec != "" && cfg.Codec != "auto")) {
			continue
		}
		n, ok := caps.NativeRenditions[t.Camera]
		u, err := url.Parse(n.RTSP)
		if !ok || n.Codec != "h265" || n.Width <= 0 || n.Height <= 0 || err != nil || u.Host == "" || (u.Scheme != "rtsp" && u.Scheme != "rtsps") {
			continue
		}
		if !rendition.ExplicitSinkSupport(n, caps.NativeHEVCSinkCaps) {
			continue
		}
		if n.Freshness.Enforced() && (!caps.DecodedPacing || !caps.StampReader || caps.ClockFile == "" || caps.Sink != "planes" || caps.DRMFD <= 0) {
			continue
		}
		if n.Freshness.Supported() && !n.Freshness.BoundTo(n.RTSP) {
			continue
		}
		if compatible, exists := caps.Freshness[t.Camera]; exists && compatible.Enforced() && !n.Freshness.Enforced() {
			continue
		}
		nativeURL := c.TileURLForStream(config.Tile{Camera: t.Camera, URL: n.RTSP}, t.Camera)
		// The compatible URI already carries validated automatic packet-size hints.
		// Inherit just that transport option, without copying its resource selector.
		if c.RTSPPacketSize == nil {
			prior, _ := url.Parse(t.URL)
			target, _ := url.Parse(nativeURL)
			if prior != nil && target != nil && !target.Query().Has("pkt_size") {
				hint := prior.Query().Get("pkt_size")
				if size, err := strconv.Atoi(hint); err == nil && size >= 256 && size <= 65535 {
					query := target.Query()
					query.Set("pkt_size", hint)
					target.RawQuery = query.Encode()
					nativeURL = target.String()
				}
			}
		}
		selected[i].URL, selected[i].Codec = nativeURL, n.Codec
		// Never reuse the compatible resource's age contract for the native URL.
		delete(fresh, t.Camera)
		if n.Freshness.Supported() && n.Freshness.BoundTo(n.RTSP) {
			fresh[t.Camera] = n.Freshness
		}
		overrides[t.Camera] = caps.NativeDecoder
		changed = true
	}
	caps.Freshness, caps.decoderOverrides = fresh, overrides
	return selected, caps, changed
}

func fallbackArgs(c *config.Config, tiles []layout.Placed, caps Caps) ([]string, error) {
	_, _, selected := selectNative(c, tiles, caps)
	if !selected {
		return nil, nil
	}
	caps.NativeRenditions = nil
	caps.NativeDecoder = ""
	return Build(c, tiles, caps)
}

// Software retry is available only for automatic, independent tile pipelines.
// It uses the same compatible resource, codec, timing and resolution.
func softwareFallbackArgs(c *config.Config, tiles []layout.Placed, caps Caps) ([]string, error) {
	if c.Decoder != "auto" || caps.Decoder == "software" || caps.Sink != "planes" {
		return nil, nil
	}
	for _, t := range tiles {
		if t.StillURL != "" || !caps.SoftwareDecoders[codecOf(t)] {
			return nil, nil
		}
	}
	caps.NativeDecoder = ""
	caps.NativeRenditions = nil
	caps.decoderOverrides = nil
	caps.Decoder = "software"
	return Build(c, tiles, caps)
}
