package gstnative

import (
	"errors"
	"fmt"
	"strings"

	"eufy-wall/internal/layout"
	"eufy-wall/internal/pipeline"
)

const blackSource = `videotestsrc is-live=true pattern=black ! capsfilter caps="video/x-raw,format=I420,width=1,height=1,framerate=1/1"`

var nativeDecoders = map[string]map[string]string{
	"v4l2":     {"h264": "v4l2h264dec", "h265": "v4l2h265dec"},
	"va":       {"h264": "vah264dec", "h265": "vah265dec"},
	"software": {"h264": "avdec_h264", "h265": "avdec_h265"},
}

func sourceKind(tile layout.Placed) string {
	if tile.StillURL != "" {
		return "still"
	}
	if tile.URL != "" {
		return "live"
	}
	return "black"
}

func tileID(tile layout.Placed) string {
	if tile.ID != "" {
		return tile.ID
	}
	return fmt.Sprintf("tile%d", tile.Index)
}

func sourceDescription(tile layout.Placed, caps pipeline.Caps, latency int) (string, error) {
	switch sourceKind(tile) {
	case "black":
		return blackSource, nil
	case "still":
		quoted, err := quoteProperty(tile.StillURL)
		if err != nil {
			return "", err
		}
		return "souphttpsrc location=" + quoted + " is-live=false ! jpegdec ! imagefreeze ! videoconvert", nil
	}
	codec := tile.Codec
	if codec == "" {
		codec = "h264"
	}
	family, ok := nativeDecoders[caps.Decoder]
	if !ok || family[codec] == "" {
		return "", fmt.Errorf("native compositor: decoder %q cannot decode %s (tile %s)", caps.Decoder, codec, tileID(tile))
	}
	depay, parse := "rtph264depay", "h264parse"
	rtpCodec := "H264"
	if codec == "h265" {
		depay, parse = "rtph265depay", "h265parse"
		rtpCodec = "H265"
	}
	quoted, err := quoteProperty(tile.URL)
	if err != nil {
		return "", err
	}
	if latency < 0 || latency > 10000 {
		return "", errors.New("native compositor latency must be 0..10000 ms")
	}
	return fmt.Sprintf("rtspsrc location=%s latency=%d protocols=tcp ! application/x-rtp,media=video,encoding-name=%s ! %s ! %s ! %s ! watchdog timeout=15000 ! videoconvert", quoted, latency, rtpCodec, depay, parse, family[codec]), nil
}

func quoteProperty(value string) (string, error) {
	if value == "" {
		return "", errors.New("source URL is empty")
	}
	if strings.ContainsAny(value, "\x00\r\n") {
		return "", errors.New("source URL contains a control character")
	}
	return `"` + strings.ReplaceAll(strings.ReplaceAll(value, `\`, `\\`), `"`, `\"`) + `"`, nil
}
