// Package rendition describes an optional bridge-native resource. Its freshness
// contract belongs to this exact URL, independently of the compatible stream.
package rendition

import "eufy-wall/internal/freshness"

type Native struct {
	RTSP      string               `json:"rtsp"`
	Codec     string               `json:"codec"`
	Profile   *int                 `json:"profile"`
	Level     *int                 `json:"level"`
	Tier      *int                 `json:"tier"`
	Width     int                  `json:"width"`
	Height    int                  `json:"height"`
	Freshness freshness.Capability `json:"freshness"`
}
