package drmbackground

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
)

type recorded struct {
	op  uint8
	arg []byte
}
type fakeDevice struct {
	calls             []recorded
	seen              map[uint8]int
	failOp            uint8
	failCall          int
	formats           []uint32
	currentFB         uint32
	pixels            []byte
	badPitch          bool
	wrongPrimary      bool
	atomicUnavailable bool
}

func fake() *fakeDevice {
	return &fakeDevice{seen: map[uint8]int{}, formats: []uint32{xrgb8888, rgb565}, currentFB: 90}
}

var propNames = []string{"type", "CRTC_X", "CRTC_Y", "CRTC_W", "CRTC_H", "SRC_X", "SRC_Y", "SRC_H", "SRC_W"}
var previousRect = []uint64{1, ^uint64(6), 9, 1800, 1000, 2 << 16, 3 << 16, 1000 << 16, 1800 << 16}

func (f *fakeDevice) ioctl(op uint8, arg []byte, arrays ...array) error {
	f.calls = append(f.calls, recorded{op, append([]byte(nil), arg...)})
	f.seen[op]++
	if op == f.failOp && f.seen[op] == f.failCall {
		return errors.New("injected ioctl failure")
	}
	ref := func(offset int) []byte {
		for _, a := range arrays {
			if a.offset == offset {
				return a.data
			}
		}
		return nil
	}
	fillIDs := func(data []byte, ids []uint32) {
		for i, id := range ids {
			if (i+1)*4 <= len(data) {
				put32(data, i*4, id)
			}
		}
	}
	expectSize := map[uint8]int{opSetCap: 16, opResources: 64, opConnector: 80, opEncoder: 20, opCRTC: 104, opPlanes: 16, opPlane: 32, opProperties: 32, opProperty: 64, opCreateDumb: 32, opMapDumb: 16, opAddFB: 104, opSetPlane: 48, opRemoveFB: 4, opDestroyDumb: 4}
	if len(arg) != expectSize[op] {
		return errors.New("incorrect UAPI structure size")
	}
	switch op {
	case opSetCap:
		if f.atomicUnavailable && get64(arg, 0) == 3 {
			return errors.New("legacy driver")
		}
	case opResources:
		if get32(arg, 32) != 0 || get32(arg, 44) != 0 {
			return errors.New("nonzero capacity for absent framebuffer/encoder pointers")
		}
		put32(arg, 32, 3)
		put32(arg, 44, 2)
		put32(arg, 36, 2)
		put32(arg, 40, 2)
		fillIDs(ref(8), []uint32{19, 37})
		fillIDs(ref(16), []uint32{71, 81})
	case opConnector:
		if get32(arg, 32) != 1 {
			return errors.New("forced connector reprobe")
		}
		if get32(arg, 48) == 81 {
			put32(arg, 44, 55)
			put32(arg, 52, 11)
			put32(arg, 60, 1)
		}
	case opEncoder:
		put32(arg, 8, 37)
	case opCRTC:
		put32(arg, 16, 90)
		put32(arg, 32, 1)
		arg[40] = 0x80
		arg[41] = 0x07 // hdisplay=1920
		arg[50] = 0x38
		arg[51] = 0x04 // vdisplay=1080
	case opPlanes:
		put32(arg, 8, 4)
		fillIDs(ref(0), []uint32{310, 320, 330, 340})
	case opPlane:
		id := get32(arg, 0)
		put32(arg, 12, 2)
		if id == 320 {
			put32(arg, 12, 1)
			put32(arg, 4, 19)
		}
		if id == 340 {
			put32(arg, 4, 37)
			put32(arg, 8, f.currentFB)
		}
		put32(arg, 20, uint32(len(f.formats)))
		fillIDs(ref(24), f.formats)
	case opProperties:
		count := len(propNames)
		if f.atomicUnavailable {
			count = 1
		}
		put32(arg, 16, uint32(count))
		ids := ref(0)
		vals := ref(8)
		for i := 0; i < count; i++ {
			if (i+1)*4 <= len(ids) {
				put32(ids, i*4, uint32(i+1))
			}
			if (i+1)*8 <= len(vals) {
				v := previousRect[i]
				if i == 0 {
					switch get32(arg, 20) {
					case 310:
						v = 0
					case 330:
						v = 2
					case 340:
						if f.wrongPrimary {
							v = 0
						}
					}
				}
				put64(vals, i*8, v)
			}
		}
	case opProperty:
		copy(arg[24:56], propNames[get32(arg, 16)-1])
	case opCreateDumb:
		pitch := get32(arg, 4)*(get32(arg, 8)/8) + 16
		if f.badPitch {
			pitch = 1
		}
		put32(arg, 16, 123)
		put32(arg, 20, pitch)
		put64(arg, 24, uint64(pitch)*uint64(get32(arg, 0)))
	case opMapDumb:
		put64(arg, 8, 4096)
	case opAddFB:
		if get32(arg, 20) != 123 || get32(arg, 12) != rgb565 && get32(arg, 12) != xrgb8888 {
			return errors.New("incorrect framebuffer handle or format offset")
		}
		if !bytes.Equal(arg[40:], make([]byte, 64)) {
			return errors.New("unused framebuffer fields not zero")
		}
		put32(arg, 0, 888)
	case opSetPlane:
		if get32(arg, 0) != 340 {
			return errors.New("wrong plane modified")
		}
		f.currentFB = get32(arg, 8)
	case opRemoveFB, opDestroyDumb:
	default:
		return errors.New("unknown ioctl")
	}
	return nil
}

func (f *fakeDevice) mmap(offset uint64, size int) ([]byte, error) {
	if offset != 4096 {
		return nil, errors.New("incorrect mapping offset")
	}
	f.pixels = bytes.Repeat([]byte{0x7f}, size)
	return f.pixels, nil
}
func (f *fakeDevice) munmap([]byte) error { f.calls = append(f.calls, recorded{op: 0xff}); return nil }

func TestPrimarySelectionBlackPixelsAndExactRestore(t *testing.T) {
	f := fake()
	b, err := open(f, 81)
	if err != nil {
		t.Fatal(err)
	}
	info := b.Info()
	if info.ConnectorID != 81 || info.CRTCID != 37 || info.PlaneID != 340 || info.FramebufferID != 888 || info.BitsPerPixel != 16 || info.PixelFormat != rgb565 {
		t.Fatalf("selected wrong output/plane/format: %+v", info)
	}
	if len(f.pixels) != (1920*2+16)*1080 || !bytes.Equal(f.pixels, make([]byte, len(f.pixels))) {
		t.Fatal("pixel rows or pitch padding were not filled black")
	}
	if f.seen[opCreateDumb] != 1 || f.seen[opAddFB] != 1 || f.seen[opSetPlane] != 1 {
		t.Fatal("background was not a single static image")
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	if f.currentFB != 90 {
		t.Fatalf("previous image not restored: %d", f.currentFB)
	}
	last := f.calls[len(f.calls)-4:]
	if !reflect.DeepEqual([]uint8{last[0].op, last[1].op, last[2].op, last[3].op}, []uint8{opSetPlane, 0xff, opRemoveFB, opDestroyDumb}) {
		t.Fatalf("unsafe cleanup ordering: %+v", last)
	}
	for i, off := range []int{16, 20, 24, 28, 32, 36, 40, 44} {
		if get32(last[0].arg, off) != uint32(previousRect[i+1]) {
			t.Fatalf("previous rectangle field%d not restored", off)
		}
	}
	n := len(f.calls)
	if err := b.Close(); err != nil || len(f.calls) != n {
		t.Fatal("Close was not idempotent")
	}
}

func TestXRGBFallbackAndLegacyDriver(t *testing.T) {
	f := fake()
	f.formats = []uint32{xrgb8888}
	f.atomicUnavailable = true
	b, err := open(f, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if b.Info().BitsPerPixel != 32 || b.Info().PixelFormat != xrgb8888 {
		t.Fatal("XRGB fallback failed")
	}
	if get32(b.previous, 24) != 1920 || get32(b.previous, 28) != 1080 || get32(b.previous, 44) != 1920<<16 || get32(b.previous, 40) != 1080<<16 {
		t.Fatal("legacy restoration is not full CRTC viewport")
	}
}

func TestInitializationFailuresReleaseOnlyAllocatedResources(t *testing.T) {
	for _, op := range []uint8{opCreateDumb, opMapDumb, opAddFB, opSetPlane} {
		t.Run(string(rune(op)), func(t *testing.T) {
			f := fake()
			f.failOp = op
			f.failCall = 1
			if b, err := open(f, 81); err == nil || b != nil {
				t.Fatal("injected failure not returned")
			}
			if f.currentFB != 90 {
				t.Fatal("initialization failure disturbed existing plane")
			}
			wantDestroy := 0
			if op != opCreateDumb {
				wantDestroy = 1
			}
			if f.seen[opDestroyDumb] != wantDestroy {
				t.Fatalf("allocation leak: destroys=%d", f.seen[opDestroyDumb])
			}
			wantRemove := 0
			if op == opSetPlane {
				wantRemove = 1
			}
			if f.seen[opRemoveFB] != wantRemove {
				t.Fatal("registered framebuffer cleanup mismatch")
			}
		})
	}
}

func TestDoNotClobberNewPrimaryOwner(t *testing.T) {
	f := fake()
	b, err := open(f, 81)
	if err != nil {
		t.Fatal(err)
	}
	f.currentFB = 999
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	if f.currentFB != 999 || f.seen[opSetPlane] != 1 || f.seen[opRemoveFB] != 1 {
		t.Fatal("shutdown overwrote another primary-plane owner")
	}
}

func TestRestoreFailureDoesNotRemoveActiveFramebuffer(t *testing.T) {
	f := fake()
	b, err := open(f, 81)
	if err != nil {
		t.Fatal(err)
	}
	f.failOp = opSetPlane
	f.failCall = 2
	if err := b.Close(); err == nil {
		t.Fatal("restore failure hidden")
	}
	if f.seen[opRemoveFB] != 0 || f.seen[opDestroyDumb] != 0 || f.currentFB != 888 {
		t.Fatal("failed restore explicitly removed active background")
	}
}

func TestInvalidAllocationAndMissingPrimaryFailBeforeMapping(t *testing.T) {
	f := fake()
	f.badPitch = true
	if _, err := open(f, 81); err == nil {
		t.Fatal("undersized allocation accepted")
	}
	if f.pixels != nil || f.seen[opDestroyDumb] != 1 {
		t.Fatal("invalid allocation mapped or leaked")
	}
	f = fake()
	f.wrongPrimary = true
	if _, err := open(f, 81); err == nil {
		t.Fatal("overlay accepted as primary")
	}
	if f.seen[opCreateDumb] != 0 {
		t.Fatal("allocated despite missing primary plane")
	}
	if _, err := listBytes(^uint32(0), 8); err == nil {
		t.Fatal("unbounded kernel object count accepted")
	}
}

func TestLinuxUAPIRequestSizes(t *testing.T) {
	// Header-generated request values use the same fixed __u64 layouts on ARM EABI and x86_64.
	for _, tc := range []struct {
		op   uint8
		size int
		want uintptr
	}{
		{opSetCap, 16, 0x4010640d}, {opResources, 64, 0xc04064a0}, {opCRTC, 104, 0xc06864a1},
		{opEncoder, 20, 0xc01464a6}, {opConnector, 80, 0xc05064a7}, {opProperty, 64, 0xc04064aa},
		{opPlanes, 16, 0xc01064b5}, {opPlane, 32, 0xc02064b6}, {opSetPlane, 48, 0xc03064b7},
		{opCreateDumb, 32, 0xc02064b2}, {opMapDumb, 16, 0xc01064b3}, {opDestroyDumb, 4, 0xc00464b4},
		{opAddFB, 104, 0xc06864b8}, {opProperties, 32, 0xc02064b9},
	} {
		if got := ioctlRequest(tc.op, tc.size); got != tc.want {
			t.Errorf("op%x request%x want%x", tc.op, got, tc.want)
		}
	}
}
