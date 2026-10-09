//go:build !linux

package drmbackground

import (
	"fmt"
	"os"
)

func nativeDevice(*os.File) (device, error) { return nil, fmt.Errorf("DRM background requires Linux") }
