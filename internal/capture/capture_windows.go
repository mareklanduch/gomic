//go:build windows

package capture

// The Windows implementation talks to WASAPI directly through COM.

import (
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"github.com/mareklanduch/gomic/internal/meter"
)

// Run captures forever, reopening the device when it changes or fails.
func (c *Capturer) Run() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if hr, _, _ := procCoInitializeEx.Call(0, coinitMultithreaded); failed(hr) {
		c.setStatus("", hresult("CoInitializeEx", hr))
		return
	}
	defer procCoUninitialize.Call()

	var enum unsafe.Pointer
	hr, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidMMDeviceEnumerator)), 0, clsctxAll,
		uintptr(unsafe.Pointer(&iidIMMDeviceEnumerator)), uintptr(unsafe.Pointer(&enum)))
	if failed(hr) {
		c.setStatus("", hresult("create device enumerator", hr))
		return
	}
	defer release(enum)

	for {
		if err := c.session(enum); err != nil {
			c.setStatus("", err)
			time.Sleep(time.Second)
		}
	}
}

// session meters one device until the default device or the source changes
// (returns nil) or something fails (returns the error).
func (c *Capturer) session(enum unsafe.Pointer) error {
	src := c.Source()
	dev, err := defaultDevice(enum, src)
	if err != nil {
		return err
	}
	defer release(dev)

	id, err := deviceID(dev)
	if err != nil {
		return err
	}

	var client unsafe.Pointer
	if hr := call(dev, 3, // IMMDevice::Activate
		uintptr(unsafe.Pointer(&iidIAudioClient)), clsctxAll, 0, uintptr(unsafe.Pointer(&client))); failed(hr) {
		return hresult("activate audio client", hr)
	}
	defer release(client)

	var wfx unsafe.Pointer
	if hr := call(client, 8, uintptr(unsafe.Pointer(&wfx))); failed(hr) { // GetMixFormat
		return hresult("get mix format", hr)
	}
	defer procCoTaskMemFree.Call(uintptr(wfx))

	format, blockAlign, err := parseFormat(wfx)
	if err != nil {
		return err
	}

	var flags uintptr
	if src == Output {
		flags = streamFlagsLoopback
	}
	const bufferHNS = 2_000_000 // 200 ms in 100 ns units
	if hr := call(client, 3,    // IAudioClient::Initialize
		shareModeShared, flags, bufferHNS, 0, uintptr(wfx), 0); failed(hr) {
		return hresult("initialize audio client", hr)
	}

	var capt unsafe.Pointer
	if hr := call(client, 14, // GetService
		uintptr(unsafe.Pointer(&iidIAudioCaptureClient)), uintptr(unsafe.Pointer(&capt))); failed(hr) {
		return hresult("get capture client", hr)
	}
	defer release(capt)

	if hr := call(client, 10); failed(hr) { // Start
		return hresult("start capture", hr)
	}
	defer call(client, 11) // Stop

	c.setStatus(friendlyName(dev), nil)

	lastCheck := time.Now()
	for {
		time.Sleep(10 * time.Millisecond)

		for {
			var frames uint32
			if hr := call(capt, 5, uintptr(unsafe.Pointer(&frames))); failed(hr) { // GetNextPacketSize
				return hresult("read packet size", hr)
			}
			if frames == 0 {
				break
			}
			var data *byte
			var bufFlags uint32
			if hr := call(capt, 3, // GetBuffer
				uintptr(unsafe.Pointer(&data)), uintptr(unsafe.Pointer(&frames)),
				uintptr(unsafe.Pointer(&bufFlags)), 0, 0); failed(hr) {
				return hresult("read buffer", hr)
			}
			if frames > 0 && data != nil && bufFlags&bufferFlagsSilent == 0 {
				c.Acc.Add(meter.Scan(unsafe.Slice(data, int(frames)*blockAlign), format))
			}
			if hr := call(capt, 4, uintptr(frames)); failed(hr) { // ReleaseBuffer
				return hresult("release buffer", hr)
			}
		}

		if c.Source() != src {
			return nil
		}
		if time.Since(lastCheck) > time.Second {
			lastCheck = time.Now()
			cur, err := defaultDevice(enum, src)
			if err != nil {
				return err
			}
			curID, err := deviceID(cur)
			release(cur)
			if err != nil {
				return err
			}
			if curID != id {
				return nil
			}
		}
	}
}

func defaultDevice(enum unsafe.Pointer, src Source) (unsafe.Pointer, error) {
	flow, what := uintptr(dataFlowCapture), "input"
	if src == Output {
		flow, what = dataFlowRender, "output"
	}
	var dev unsafe.Pointer
	hr := call(enum, 4, flow, roleConsole, uintptr(unsafe.Pointer(&dev))) // GetDefaultAudioEndpoint
	if uint32(hr) == hrNotFound {
		return nil, fmt.Errorf("no default %s device", what)
	}
	if failed(hr) {
		return nil, hresult("get default "+what+" device", hr)
	}
	return dev, nil
}

func deviceID(dev unsafe.Pointer) (string, error) {
	var p *uint16
	if hr := call(dev, 5, uintptr(unsafe.Pointer(&p))); failed(hr) { // IMMDevice::GetId
		return "", hresult("get device id", hr)
	}
	defer procCoTaskMemFree.Call(uintptr(unsafe.Pointer(p)))
	return utf16PtrToString(p), nil
}

func friendlyName(dev unsafe.Pointer) string {
	var store unsafe.Pointer
	if hr := call(dev, 4, stgmRead, uintptr(unsafe.Pointer(&store))); failed(hr) { // OpenPropertyStore
		return "Unknown device"
	}
	defer release(store)

	var pv propVariant
	if hr := call(store, 5, // IPropertyStore::GetValue
		uintptr(unsafe.Pointer(&pkeyDeviceFriendlyName)), uintptr(unsafe.Pointer(&pv))); failed(hr) {
		return "Unknown device"
	}
	defer procPropVariantClear.Call(uintptr(unsafe.Pointer(&pv)))
	if pv.vt != vtLPWSTR || pv.ptr == nil {
		return "Unknown device"
	}
	return utf16PtrToString((*uint16)(pv.ptr))
}

// parseFormat reads a WAVEFORMATEX / WAVEFORMATEXTENSIBLE. The struct is
// byte-packed, so fields are read by offset.
func parseFormat(wfx unsafe.Pointer) (meter.Format, int, error) {
	u16 := func(off uintptr) uint16 { return *(*uint16)(unsafe.Add(wfx, off)) }
	tag, channels, blockAlign, cbSize := u16(0), int(u16(2)), int(u16(12)), u16(16)

	if tag == waveFormatExtensible && cbSize >= 22 {
		// SubFormat GUID starts at offset 24; its first field is the classic tag.
		tag = uint16(*(*uint32)(unsafe.Add(wfx, 24)))
	}
	if channels < 1 || blockAlign < channels {
		return meter.Format{}, 0, errors.New("device reported an invalid format")
	}
	f := meter.Format{Channels: channels, BytesPerSample: blockAlign / channels}
	switch tag {
	case waveFormatIEEEFloat:
		if f.BytesPerSample != 4 {
			return meter.Format{}, 0, errors.New("unsupported float sample size")
		}
		f.Float = true
	case waveFormatPCM:
		if f.BytesPerSample < 2 || f.BytesPerSample > 4 {
			return meter.Format{}, 0, errors.New("unsupported PCM sample size")
		}
	default:
		return meter.Format{}, 0, fmt.Errorf("unsupported sample format 0x%X", tag)
	}
	return f, blockAlign, nil
}

// ---- COM plumbing ----

const (
	coinitMultithreaded  = 0
	clsctxAll            = 23
	dataFlowRender       = 0
	dataFlowCapture      = 1
	roleConsole          = 0
	shareModeShared      = 0
	streamFlagsLoopback  = 0x00020000
	bufferFlagsSilent    = 0x2
	stgmRead             = 0
	vtLPWSTR             = 31
	waveFormatPCM        = 1
	waveFormatIEEEFloat  = 3
	waveFormatExtensible = 0xFFFE
	hrNotFound           = 0x80070490
)

var (
	ole32                = syscall.NewLazyDLL("ole32.dll")
	procCoInitializeEx   = ole32.NewProc("CoInitializeEx")
	procCoUninitialize   = ole32.NewProc("CoUninitialize")
	procCoCreateInstance = ole32.NewProc("CoCreateInstance")
	procCoTaskMemFree    = ole32.NewProc("CoTaskMemFree")
	procPropVariantClear = ole32.NewProc("PropVariantClear")

	clsidMMDeviceEnumerator = syscall.GUID{Data1: 0xBCDE0395, Data2: 0xE52F, Data3: 0x467C, Data4: [8]byte{0x8E, 0x3D, 0xC4, 0x57, 0x92, 0x91, 0x69, 0x2E}}
	iidIMMDeviceEnumerator  = syscall.GUID{Data1: 0xA95664D2, Data2: 0x9614, Data3: 0x4F35, Data4: [8]byte{0xA7, 0x46, 0xDE, 0x8D, 0xB6, 0x36, 0x17, 0xE6}}
	iidIAudioClient         = syscall.GUID{Data1: 0x1CB9AD4C, Data2: 0xDBFA, Data3: 0x4C32, Data4: [8]byte{0xB1, 0x78, 0xC2, 0xF5, 0x68, 0xA7, 0x03, 0xB2}}
	iidIAudioCaptureClient  = syscall.GUID{Data1: 0xC8ADBD64, Data2: 0xE71E, Data3: 0x48A0, Data4: [8]byte{0xA4, 0xDE, 0x18, 0x5C, 0x39, 0x5C, 0xD3, 0x17}}

	pkeyDeviceFriendlyName = propertyKey{
		fmtid: syscall.GUID{Data1: 0xA45C254E, Data2: 0xDF1C, Data3: 0x4EFD, Data4: [8]byte{0x80, 0x20, 0x67, 0xD1, 0x46, 0xA8, 0x50, 0xE0}},
		pid:   14,
	}
)

type propertyKey struct {
	fmtid syscall.GUID
	pid   uint32
}

// propVariant matches the 64-bit PROPVARIANT layout for pointer-valued types.
type propVariant struct {
	vt       uint16
	reserved [3]uint16
	ptr      unsafe.Pointer
	extra    uintptr
}

// call invokes method number idx in the COM object's vtable.
func call(obj unsafe.Pointer, idx int, args ...uintptr) uintptr {
	vtbl := *(**[32]uintptr)(obj)
	all := append([]uintptr{uintptr(obj)}, args...)
	hr, _, _ := syscall.SyscallN(vtbl[idx], all...)
	return hr
}

func release(obj unsafe.Pointer) {
	if obj != nil {
		call(obj, 2) // IUnknown::Release
	}
}

func failed(hr uintptr) bool { return int32(hr) < 0 }

func hresult(what string, hr uintptr) error {
	return fmt.Errorf("%s failed (0x%08X)", what, uint32(hr))
}

func utf16PtrToString(p *uint16) string {
	if p == nil {
		return ""
	}
	n := 0
	for *(*uint16)(unsafe.Add(unsafe.Pointer(p), n*2)) != 0 {
		n++
	}
	return syscall.UTF16ToString(unsafe.Slice(p, n))
}
