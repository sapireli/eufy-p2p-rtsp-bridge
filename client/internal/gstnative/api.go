package gstnative

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

const (
	stateNull    = 1
	statePlaying = 4
	probeBuffer  = 1 << 4
	probeOK      = 1
	messageError = 1 << 1
)

// The symbols below are part of GStreamer's public C ABI. Resolving them at runtime keeps the
// eufy-wall executable cross-buildable with CGO_ENABLED=0 and gives a clear prerequisite error.
type gstAPI struct {
	init             func(uintptr, uintptr)
	parse            func(string, uintptr) uintptr
	setState         func(uintptr, int32) int32
	getState         func(uintptr, uintptr, uintptr, uint64) int32
	byName           func(uintptr, string) uintptr
	staticPad        func(uintptr, string) uintptr
	objectUnref      func(uintptr)
	addProbe         func(uintptr, uint64, uintptr, uintptr, uintptr) uint64
	removeProbe      func(uintptr, uint64)
	getBus           func(uintptr) uintptr
	popBus           func(uintptr, uint64, uint32) uintptr
	parseError       func(uintptr, uintptr, uintptr)
	streamErrorQuark func() uint32
	miniUnref        func(uintptr)
	miniRef          func(uintptr) uintptr
	freeError        func(uintptr)
	free             func(uintptr)
	macosMain        func(uintptr, uintptr) int32
	appSinkPull      func(uintptr, uint64) uintptr
	appSrcPush       func(uintptr, uintptr) int32
	appSrcLevel      func(uintptr) uint64
	sampleBuffer     func(uintptr) uintptr
	bufferNew        func() uintptr
	bufferSize       func(uintptr) uintptr
	bufferCopyInto   func(uintptr, uintptr, uint32, uintptr, uintptr) int32
}

var loaded struct {
	sync.Once
	api *gstAPI
	err error
}

func load() (*gstAPI, error) {
	loaded.Do(func() { loaded.api, loaded.err = openAPI() })
	return loaded.api, loaded.err
}

func openAPI() (*gstAPI, error) {
	core, err := openLibrary(gstLibraries())
	if err != nil {
		return nil, fmt.Errorf("GStreamer library: %w", err)
	}
	glib, err := openLibrary(glibLibraries())
	if err != nil {
		return nil, fmt.Errorf("GLib library: %w", err)
	}
	app, err := openLibrary(appLibraries())
	if err != nil {
		return nil, fmt.Errorf("GStreamer app library: %w", err)
	}
	a := new(gstAPI)
	for _, item := range []struct {
		name string
		fn   any
		lib  uintptr
	}{
		{"gst_init", &a.init, core}, {"gst_parse_launch", &a.parse, core},
		{"gst_element_set_state", &a.setState, core},
		{"gst_element_get_state", &a.getState, core}, {"gst_bin_get_by_name", &a.byName, core},
		{"gst_element_get_static_pad", &a.staticPad, core},
		{"gst_object_unref", &a.objectUnref, core}, {"gst_pad_add_probe", &a.addProbe, core},
		{"gst_pad_remove_probe", &a.removeProbe, core}, {"gst_element_get_bus", &a.getBus, core},
		{"gst_bus_timed_pop_filtered", &a.popBus, core}, {"gst_message_parse_error", &a.parseError, core},
		{"gst_stream_error_quark", &a.streamErrorQuark, core},
		{"gst_mini_object_unref", &a.miniUnref, core}, {"g_error_free", &a.freeError, glib},
		{"gst_mini_object_ref", &a.miniRef, core},
		{"g_free", &a.free, glib},
		{"gst_app_sink_try_pull_sample", &a.appSinkPull, app},
		{"gst_app_src_push_buffer", &a.appSrcPush, app},
		{"gst_app_src_get_current_level_bytes", &a.appSrcLevel, app},
		{"gst_sample_get_buffer", &a.sampleBuffer, core},
		{"gst_buffer_new", &a.bufferNew, core},
		{"gst_buffer_get_size", &a.bufferSize, core},
		{"gst_buffer_copy_into", &a.bufferCopyInto, core},
	} {
		if err := bind(item.lib, item.name, item.fn); err != nil {
			return nil, err
		}
	}
	if runtime.GOOS == "darwin" {
		if err := bind(core, "gst_macos_main_simple", &a.macosMain); err != nil {
			return nil, err
		}
	}
	a.init(0, 0)
	return a, nil
}

func gstLibraries() []string {
	if runtime.GOOS == "darwin" {
		return []string{"/usr/local/lib/libgstreamer-1.0.dylib", "/opt/homebrew/lib/libgstreamer-1.0.dylib", "libgstreamer-1.0.dylib"}
	}
	return []string{"libgstreamer-1.0.so.0"}
}

func glibLibraries() []string {
	if runtime.GOOS == "darwin" {
		return []string{"/usr/local/lib/libglib-2.0.dylib", "/opt/homebrew/lib/libglib-2.0.dylib", "libglib-2.0.dylib"}
	}
	return []string{"libglib-2.0.so.0"}
}

func appLibraries() []string {
	if runtime.GOOS == "darwin" {
		return []string{"/usr/local/lib/libgstapp-1.0.dylib", "/opt/homebrew/lib/libgstapp-1.0.dylib", "libgstapp-1.0.dylib"}
	}
	return []string{"libgstapp-1.0.so.0"}
}

func openLibrary(candidates []string) (uintptr, error) {
	var last error
	for _, name := range candidates {
		if handle, err := purego.Dlopen(name, purego.RTLD_NOW|purego.RTLD_GLOBAL); err == nil {
			return handle, nil
		} else {
			last = err
		}
	}
	return 0, last
}

func bind(lib uintptr, name string, fn any) (err error) {
	symbol, err := purego.Dlsym(lib, name)
	if err != nil {
		return fmt.Errorf("GStreamer symbol %s: %w", name, err)
	}
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("GStreamer binding %s: %v", name, p)
		}
	}()
	purego.RegisterFunc(fn, symbol)
	return nil
}

func (a *gstAPI) parsed(description string) (uintptr, error) {
	var parseError unsafe.Pointer
	element := a.parse(description, uintptr(unsafe.Pointer(&parseError)))
	if parseError != nil {
		message := cString((*glibError)(parseError).Message, 4096)
		a.freeError(uintptr(parseError))
		if element != 0 {
			a.objectUnref(element)
		}
		return 0, fmt.Errorf("GStreamer pipeline: %s", message)
	}
	if element == 0 {
		return 0, errors.New("GStreamer returned no pipeline")
	}
	return element, nil
}

type glibError struct {
	Domain  uint32
	Code    int32
	Message unsafe.Pointer
}

//go:nocheckptr
func cString(pointer unsafe.Pointer, limit int) string {
	if pointer == nil {
		return "unknown error"
	}
	bytes := make([]byte, 0, min(limit, 128))
	for i := 0; i < limit; i++ {
		b := *(*byte)(unsafe.Add(pointer, i))
		if b == 0 {
			break
		}
		bytes = append(bytes, b)
	}
	return string(bytes)
}

func (a *gstAPI) busError(bus uintptr) error {
	return a.busFailure(bus).err
}

type busFailure struct {
	err    error
	decode bool
}

func (a *gstAPI) busFailure(bus uintptr) busFailure {
	message := a.popBus(bus, 0, messageError)
	if message == 0 {
		return busFailure{}
	}
	defer a.miniUnref(message)
	var detail, debug unsafe.Pointer
	a.parseError(message, uintptr(unsafe.Pointer(&detail)), uintptr(unsafe.Pointer(&debug)))
	text := "GStreamer pipeline error"
	decode := false
	if detail != nil {
		gerr := (*glibError)(detail)
		text = cString(gerr.Message, 4096)
		// GST_STREAM_ERROR_DECODE is 7 in GStreamer's public GstStreamError enum.
		decode = gerr.Domain == a.streamErrorQuark() && gerr.Code == 7
		a.freeError(uintptr(detail))
	}
	if debug != nil {
		a.free(uintptr(debug))
	}
	return busFailure{err: errors.New(text), decode: decode}
}
