//go:build !linux

package freshness

import "time"

var origin = time.Now()

func monotonicNs() uint64 { return uint64(time.Since(origin)) }
