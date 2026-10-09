// Package drmbackground owns one static black primary plane behind independent video overlays.
// It uses the caller's shared DRM master; it never writes the console framebuffer or runs a renderer.
package drmbackground

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
)

// UAPI layouts and operations come from Linux include/uapi/drm/{drm,drm_mode}.h.
// Fixed byte layouts preserve the kernel's 64-bit alignment on both ARMv6 and amd64.
const (
	opSetCap       = 0x0d
	opResources    = 0xa0
	opCRTC         = 0xa1
	opEncoder      = 0xa6
	opConnector    = 0xa7
	opProperty     = 0xaa
	opRemoveFB     = 0xaf
	opCreateDumb   = 0xb2
	opMapDumb      = 0xb3
	opDestroyDumb  = 0xb4
	opPlanes       = 0xb5
	opPlane        = 0xb6
	opSetPlane     = 0xb7
	opAddFB        = 0xb8
	opProperties   = 0xb9
	xrgb8888       = 0x34325258 // DRM_FORMAT_XRGB8888 (XR24), with no alpha blending.
	rgb565         = 0x36314752 // DRM_FORMAT_RGB565 (RG16), preferred to reduce scanout bandwidth.
	objectPlane    = 0xeeeeeeee
	primaryPlane   = 1
	maxObjects     = 4096
	maxBufferBytes = 256 << 20
)

type array struct {
	offset int // offset of the __u64 userspace pointer in the ioctl argument
	data   []byte
}

type device interface {
	ioctl(op uint8, arg []byte, arrays ...array) error
	mmap(offset uint64, size int) ([]byte, error)
	munmap([]byte) error
}

// Info identifies the actual selected output and owned image, for deployment verification.
type Info struct {
	ConnectorID, CRTCID, PlaneID, FramebufferID uint32
	Width, Height                               uint32
	PixelFormat, BitsPerPixel                   uint32
}

type Background struct {
	d         device
	info      Info
	handle    uint32
	mapping   []byte
	previous  []byte // exact legacy SETPLANE layout, reconstructed from plane properties
	installed bool
	closed    bool
}

// Open attaches one black image to the selected active HDMI output. connectorID=0 selects the
// first active HDMI connector; callers should use Info().ConnectorID for their video sinks too.
// The caller retains ownership of file and must keep it open until Close returns.
func Open(file *os.File, connectorID int) (*Background, error) {
	if file == nil || connectorID < 0 || uint64(connectorID) > uint64(^uint32(0)) {
		return nil, fmt.Errorf("DRM background: invalid device or connector")
	}
	d, err := nativeDevice(file)
	if err != nil {
		return nil, err
	}
	return open(d, uint32(connectorID))
}

func (b *Background) Info() Info { return b.info }

func open(d device, connectorID uint32) (_ *Background, err error) {
	cap := make([]byte, 16)
	put64(cap, 0, 2) // DRM_CLIENT_CAP_UNIVERSAL_PLANES
	put64(cap, 8, 1)
	if err := d.ioctl(opSetCap, cap); err != nil {
		return nil, fmt.Errorf("DRM universal planes: %w", err)
	}
	// Atomic-capable drivers expose precise CRTC/SRC rectangle properties with this cap.
	// Older drivers still support the legacy full-CRTC primary-plane rectangle.
	put64(cap, 0, 3) // DRM_CLIENT_CAP_ATOMIC (also enables universal planes)
	_ = d.ioctl(opSetCap, cap)
	connector, crtc, crtcIndex, err := output(d, connectorID)
	if err != nil {
		return nil, err
	}
	w, h := uint32(binary.LittleEndian.Uint16(crtc[40:42])), uint32(binary.LittleEndian.Uint16(crtc[50:52]))
	if w == 0 || h == 0 || uint64(w)*uint64(h)*4 > maxBufferBytes {
		return nil, fmt.Errorf("DRM background: invalid or oversized active mode %dx%d", w, h)
	}
	plane, props, format, err := findPrimary(d, get32(crtc, 12), crtcIndex)
	if err != nil {
		return nil, err
	}
	bits := uint32(32)
	if format == rgb565 {
		bits = 16
	}
	b := &Background{d: d, info: Info{ConnectorID: connector, CRTCID: get32(crtc, 12), PlaneID: get32(plane, 0), Width: w, Height: h, PixelFormat: format, BitsPerPixel: bits}}
	b.previous = planeState(plane, crtc, props)
	ok := false
	defer func() {
		if !ok {
			err = errors.Join(err, b.Close())
		}
	}()
	dumb := make([]byte, 32)
	put32(dumb, 0, h)
	put32(dumb, 4, w)
	put32(dumb, 8, bits)
	if err = d.ioctl(opCreateDumb, dumb); err != nil {
		return nil, fmt.Errorf("DRM create black image: %w", err)
	}
	b.handle = get32(dumb, 16)
	pitch, size := get32(dumb, 20), get64(dumb, 24)
	if b.handle == 0 || uint64(pitch) < uint64(w)*uint64(bits/8) || size < uint64(pitch)*uint64(h) || size > maxBufferBytes {
		return nil, fmt.Errorf("DRM black image: invalid allocation handle=%d pitch=%d size=%d", b.handle, pitch, size)
	}
	mapArg := make([]byte, 16)
	put32(mapArg, 0, b.handle)
	if err = d.ioctl(opMapDumb, mapArg); err != nil {
		return nil, fmt.Errorf("DRM map black image: %w", err)
	}
	b.mapping, err = d.mmap(get64(mapArg, 8), int(size))
	if err != nil {
		return nil, fmt.Errorf("DRM map black pixels: %w", err)
	}
	clear(b.mapping) // One fill, including pitch padding; no work is needed per video frame.
	fb := make([]byte, 104)
	put32(fb, 4, w)
	put32(fb, 8, h)
	put32(fb, 12, format)
	put32(fb, 20, b.handle)
	put32(fb, 36, pitch)
	if err = d.ioctl(opAddFB, fb); err != nil {
		return nil, fmt.Errorf("DRM register black framebuffer: %w", err)
	}
	b.info.FramebufferID = get32(fb, 0)
	state := make([]byte, 48)
	put32(state, 0, b.info.PlaneID)
	put32(state, 4, b.info.CRTCID)
	put32(state, 8, b.info.FramebufferID)
	put32(state, 24, w)
	put32(state, 28, h)
	put32(state, 40, h<<16)
	put32(state, 44, w<<16)
	if err = d.ioctl(opSetPlane, state); err != nil {
		return nil, fmt.Errorf("DRM display black background: %w", err)
	}
	b.installed = true
	ok = true
	return b, nil
}

// Close restores the previous primary-plane image, then releases this client's image. Call it
// only after all children sharing the DRM descriptor have stopped. It is safe to call repeatedly.
func (b *Background) Close() error {
	if b == nil || b.closed {
		return nil
	}
	b.closed = true
	var errs []error
	retain := false
	if b.installed {
		current := make([]byte, 32)
		put32(current, 0, b.info.PlaneID)
		if err := b.d.ioctl(opPlane, current); err != nil {
			errs = append(errs, fmt.Errorf("DRM inspect background on shutdown: %w", err))
			retain = true
		} else if get32(current, 8) == b.info.FramebufferID {
			if err := b.d.ioctl(opSetPlane, b.previous); err != nil {
				errs = append(errs, fmt.Errorf("DRM restore primary plane: %w", err))
				retain = true
			}
		}
	}
	if b.mapping != nil {
		if err := b.d.munmap(b.mapping); err != nil {
			errs = append(errs, err)
		}
		b.mapping = nil
	}
	if retain {
		// Do not explicitly remove a possibly active framebuffer and blank the display.
		// The caller's final DRM fd close releases these resources after child shutdown.
		return errors.Join(errs...)
	}
	if b.info.FramebufferID != 0 {
		arg := make([]byte, 4)
		put32(arg, 0, b.info.FramebufferID)
		if err := b.d.ioctl(opRemoveFB, arg); err != nil {
			errs = append(errs, fmt.Errorf("DRM remove black framebuffer: %w", err))
		}
	}
	if b.handle != 0 {
		arg := make([]byte, 4)
		put32(arg, 0, b.handle)
		if err := b.d.ioctl(opDestroyDumb, arg); err != nil {
			errs = append(errs, fmt.Errorf("DRM free black image: %w", err))
		}
	}
	return errors.Join(errs...)
}

func output(d device, want uint32) (uint32, []byte, uint32, error) {
	res := make([]byte, 64)
	if err := d.ioctl(opResources, res); err != nil {
		return 0, nil, 0, err
	}
	for retry := 0; retry < 4; retry++ {
		cs, err := listBytes(get32(res, 36), 4)
		if err != nil {
			return 0, nil, 0, err
		}
		conns, err := listBytes(get32(res, 40), 4)
		if err != nil {
			return 0, nil, 0, err
		}
		// We only retrieve CRTCs and connectors. Counts from the previous query are not
		// capacities for absent framebuffer/encoder arrays: leaving them nonzero can EFAULT.
		put32(res, 32, 0)
		put32(res, 44, 0)
		if err := d.ioctl(opResources, res, array{8, cs}, array{16, conns}); err != nil {
			return 0, nil, 0, err
		}
		if int(get32(res, 36))*4 > len(cs) || int(get32(res, 40))*4 > len(conns) {
			continue
		}
		for i := 0; i < int(get32(res, 40)); i++ {
			id := get32(conns, i*4)
			if want != 0 && id != want {
				continue
			}
			conn := make([]byte, 80)
			put32(conn, 48, id)
			put32(conn, 32, 1) // Avoid GETCONNECTOR's forced reprobe with count_modes=0.
			if err := d.ioctl(opConnector, conn, array{8, make([]byte, 68)}); err != nil {
				return 0, nil, 0, err
			}
			if get32(conn, 60) != 1 || (want == 0 && get32(conn, 52) != 11 && get32(conn, 52) != 12) {
				continue
			}
			encoder := make([]byte, 20)
			put32(encoder, 0, get32(conn, 44))
			if get32(encoder, 0) == 0 {
				continue
			}
			if err := d.ioctl(opEncoder, encoder); err != nil {
				return 0, nil, 0, err
			}
			crtc := make([]byte, 104)
			put32(crtc, 12, get32(encoder, 8))
			if get32(crtc, 12) == 0 {
				continue
			}
			if err := d.ioctl(opCRTC, crtc); err != nil {
				return 0, nil, 0, err
			}
			if get32(crtc, 32) == 0 {
				continue
			}
			for j := 0; j < int(get32(res, 36)); j++ {
				if get32(cs, j*4) == get32(crtc, 12) {
					return id, crtc, uint32(j), nil
				}
			}
		}
		return 0, nil, 0, fmt.Errorf("DRM background: no active connector/CRTC for output %d", want)
	}
	return 0, nil, 0, fmt.Errorf("DRM output resources changed repeatedly")
}

func findPrimary(d device, crtcID, crtcIndex uint32) ([]byte, map[string]uint64, uint32, error) {
	res := make([]byte, 16)
	if err := d.ioctl(opPlanes, res); err != nil {
		return nil, nil, 0, err
	}
	for retry := 0; retry < 4; retry++ {
		ids, err := listBytes(get32(res, 8), 4)
		if err != nil {
			return nil, nil, 0, err
		}
		if err := d.ioctl(opPlanes, res, array{0, ids}); err != nil {
			return nil, nil, 0, err
		}
		if int(get32(res, 8))*4 > len(ids) {
			continue
		}
		for i := 0; i < int(get32(res, 8)); i++ {
			plane := make([]byte, 32)
			put32(plane, 0, get32(ids, i*4))
			if err := d.ioctl(opPlane, plane); err != nil {
				return nil, nil, 0, err
			}
			if crtcIndex >= 32 || get32(plane, 12)&(1<<crtcIndex) == 0 || (get32(plane, 4) != 0 && get32(plane, 4) != crtcID) {
				continue
			}
			props, err := properties(d, get32(plane, 0))
			if err != nil {
				return nil, nil, 0, err
			}
			if props["type"] != primaryPlane {
				continue
			}
			formats, err := listBytes(get32(plane, 20), 4)
			if err != nil {
				return nil, nil, 0, err
			}
			if err := d.ioctl(opPlane, plane, array{24, formats}); err != nil {
				return nil, nil, 0, err
			}
			for _, preferred := range []uint32{rgb565, xrgb8888} {
				for j := 0; j < len(formats); j += 4 {
					if get32(formats, j) == preferred {
						return plane, props, preferred, nil
					}
				}
			}
		}
		return nil, nil, 0, fmt.Errorf("DRM background: no RGB565/XRGB8888 primary plane for CRTC %d", crtcID)
	}
	return nil, nil, 0, fmt.Errorf("DRM plane resources changed repeatedly")
}

func properties(d device, id uint32) (map[string]uint64, error) {
	arg := make([]byte, 32)
	put32(arg, 20, id)
	put32(arg, 24, objectPlane)
	if err := d.ioctl(opProperties, arg); err != nil {
		return nil, err
	}
	for retry := 0; retry < 4; retry++ {
		ids, err := listBytes(get32(arg, 16), 4)
		if err != nil {
			return nil, err
		}
		values, _ := listBytes(get32(arg, 16), 8)
		if err := d.ioctl(opProperties, arg, array{0, ids}, array{8, values}); err != nil {
			return nil, err
		}
		if int(get32(arg, 16))*4 > len(ids) {
			continue
		}
		out := map[string]uint64{}
		for i := 0; i < int(get32(arg, 16)); i++ {
			property := make([]byte, 64)
			put32(property, 16, get32(ids, i*4))
			if err := d.ioctl(opProperty, property); err != nil {
				return nil, err
			}
			name := property[24:56]
			for j, ch := range name {
				if ch == 0 {
					name = name[:j]
					break
				}
			}
			out[string(name)] = get64(values, i*8)
		}
		return out, nil
	}
	return nil, fmt.Errorf("DRM plane properties changed repeatedly")
}

func planeState(plane, crtc []byte, props map[string]uint64) []byte {
	arg := make([]byte, 48)
	put32(arg, 0, get32(plane, 0))
	if get32(plane, 8) == 0 {
		return arg // Previously disabled plane.
	}
	put32(arg, 4, get32(plane, 4))
	put32(arg, 8, get32(plane, 8))
	w, h := uint32(binary.LittleEndian.Uint16(crtc[40:42])), uint32(binary.LittleEndian.Uint16(crtc[50:52]))
	put32(arg, 24, w)
	put32(arg, 28, h)
	put32(arg, 32, get32(crtc, 20)<<16)
	put32(arg, 36, get32(crtc, 24)<<16)
	put32(arg, 40, h<<16)
	put32(arg, 44, w<<16)
	// Exact rectangles when the driver's atomic properties are available.
	for name, offset := range map[string]int{"CRTC_X": 16, "CRTC_Y": 20, "CRTC_W": 24, "CRTC_H": 28, "SRC_X": 32, "SRC_Y": 36, "SRC_H": 40, "SRC_W": 44} {
		if value, ok := props[name]; ok {
			put32(arg, offset, uint32(value))
		}
	}
	return arg
}

func listBytes(count uint32, width int) ([]byte, error) {
	if count > maxObjects {
		return nil, fmt.Errorf("DRM returned excessive object count %d", count)
	}
	return make([]byte, int(count)*width), nil
}

func ioctlRequest(op uint8, size int) uintptr {
	direction := uint32(3) // _IOC_READ | _IOC_WRITE, DRM_IOWR
	if op == opSetCap {
		direction = 1 // DRM_IOW
	}
	return uintptr(direction<<30 | uint32(size)<<16 | uint32('d')<<8 | uint32(op))
}

func get32(b []byte, offset int) uint32    { return binary.LittleEndian.Uint32(b[offset:]) }
func get64(b []byte, offset int) uint64    { return binary.LittleEndian.Uint64(b[offset:]) }
func put32(b []byte, offset int, v uint32) { binary.LittleEndian.PutUint32(b[offset:], v) }
func put64(b []byte, offset int, v uint64) { binary.LittleEndian.PutUint64(b[offset:], v) }
