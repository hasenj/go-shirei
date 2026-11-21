//go:build windows

package media

import (
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"github.com/ebitengine/purego"

	"go.hasen.dev/shirei"
)

// Player owns one Windows video playback instance.
//
// Default builds (CGO_ENABLED=0) use Windows Media Foundation's IMFMediaEngine
// in D3D11 frame-server mode, handing each frame's DXGI shared texture handle
// to shirei as ExternalD3D11Texture (or falling back to a staging-texture CPU
// RGBA copy when software rendering is active).
//
// Builds with -tags libav (cgo) use the shared in-process libav* engine (NV12
// shader on D3D11, RGBA on software, stereo waveOut audio). Set SHIREI_MEDIA=mf
// to force Media Foundation even when built with -tags libav.
type Player struct {
	mf     *mfPlayer
	stream *streamPlayer
}

// Open opens path for playback. Call it before Run. Decoding starts when the
// player is first viewed or played.
func Open(path string) (*Player, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(abs); err != nil {
		return nil, err
	}

	mode := strings.ToLower(strings.TrimSpace(os.Getenv("SHIREI_MEDIA")))
	if mode == "permissive" {
		eng, perr := openPermissiveEngine(abs)
		if perr != nil {
			return nil, perr
		}
		return &Player{stream: newStreamPlayer(eng)}, nil
	}

	if libavOpen != nil && mode != "mf" {
		eng, libErr := libavOpen(abs)
		if libErr == nil {
			return &Player{stream: newStreamPlayer(eng)}, nil
		}
		if mf, mfErr := openMFPlayer(abs); mfErr == nil {
			return &Player{mf: mf}, nil
		}
		if eng, perr := openPermissiveEngine(abs); perr == nil {
			return &Player{stream: newStreamPlayer(eng)}, nil
		}
		return nil, libErr
	}

	if mode != "mf" {
		if probe, ok := sniffFile(abs); ok && (probe.Container == containerWebM || probe.VideoCodec == videoCodecAV1 || probe.VideoCodec == videoCodecVP9 || probe.VideoCodec == videoCodecVP8 || probe.AudioCodec == audioCodecOpus || probe.AudioCodec == audioCodecVorbis) && canDecodePermissive(probe) {
			if eng, perr := openPermissiveEngine(abs); perr == nil {
				return &Player{stream: newStreamPlayer(eng)}, nil
			}
		}
	}

	mf, err := openMFPlayer(abs)
	if err != nil {
		if eng, perr := openPermissiveEngine(abs); perr == nil {
			return &Player{stream: newStreamPlayer(eng)}, nil
		}
		return nil, err
	}
	return &Player{mf: mf}, nil
}

func (p *Player) Play() {
	if p.stream != nil {
		p.stream.Play()
		return
	}
	p.mf.Play()
}

func (p *Player) Pause() {
	if p.stream != nil {
		p.stream.Pause()
		return
	}
	p.mf.Pause()
}

func (p *Player) Seek(sec float64) {
	if p.stream != nil {
		p.stream.Seek(sec)
		return
	}
	p.mf.Seek(sec)
}

func (p *Player) SetLoop(loop bool) {
	if p.stream != nil {
		p.stream.SetLoop(loop)
		return
	}
	p.mf.SetLoop(loop)
}

func (p *Player) SetMuted(muted bool) {
	if p.stream != nil {
		p.stream.SetMuted(muted)
		return
	}
	p.mf.SetMuted(muted)
}

func (p *Player) Muted() bool {
	if p.stream != nil {
		return p.stream.Muted()
	}
	return p.mf.Muted()
}

func (p *Player) SetVolume(volume float32) {
	if p.stream != nil {
		p.stream.SetVolume(volume)
		return
	}
	p.mf.SetVolume(volume)
}

func (p *Player) Volume() float32 {
	if p.stream != nil {
		return p.stream.Volume()
	}
	return p.mf.Volume()
}

func (p *Player) Playing() bool {
	if p.stream != nil {
		return p.stream.Playing()
	}
	return p.mf.Playing()
}

func (p *Player) Ended() bool {
	if p.stream != nil {
		return p.stream.Ended()
	}
	return p.mf.Ended()
}

func (p *Player) Position() time.Duration {
	if p.stream != nil {
		return p.stream.Position()
	}
	return p.mf.Position()
}

func (p *Player) Duration() time.Duration {
	if p.stream != nil {
		return p.stream.Duration()
	}
	return p.mf.Duration()
}

func (p *Player) Size() (int, int) {
	if p.stream != nil {
		return p.stream.Size()
	}
	return p.mf.Size()
}

func (p *Player) ImageId() shirei.ImageId {
	if p.stream != nil {
		return p.stream.ImageId()
	}
	return p.mf.ImageId()
}

func (p *Player) Err() error {
	if p.stream != nil {
		return p.stream.Err()
	}
	return p.mf.Err()
}

func (p *Player) View(maxSize shirei.Vec2) {
	if p.stream != nil {
		p.stream.View(maxSize)
		return
	}
	p.mf.View(maxSize)
}

func (p *Player) Close() {
	if p.stream != nil {
		p.stream.Close()
		return
	}
	p.mf.Close()
}

// -----------------------------------------------------------------------------
// Media Foundation IMFMediaEngine backend (pure Go COM, zero cgo)

const (
	coinitMultithreaded = 0x0
	clsctxInprocServer  = 0x1
	mfVersion           = 0x00020070
	mfStartupLite       = 0x1

	mfEventLoadedMetadata = 1001
	mfEventCanPlay        = 1003
	mfEventSeeked         = 1009
	mfEventEnded          = 1013
	mfEventError          = 1015

	eNoInterface = 0x80004002

	d3d11SDKVersion     = 7
	d3dDriverHardware   = 1
	d3dCreateBGRA       = 0x20
	d3dCreateVideo      = 0x800
	d3dFeature11_0      = 0xb000
	d3dFeature10_1      = 0xa100
	d3dFeature10_0      = 0xa000
	dxgiFmtBGRA8Unorm   = 87
	d3dUsageDefault     = 0
	d3dUsageStaging     = 3
	d3dBindSRV          = 0x8
	d3dBindRenderTarget = 0x20
	d3dCPURead          = 0x20000
	d3dMiscShared       = 0x2
	d3dMapRead          = 1

	comQueryInterface = 0
	comRelease        = 2

	attrSetUINT32  = 21
	attrSetUnknown = 27

	dxgiMgrResetDevice = 7
	dxgiResGetShared   = 8

	d3d10MTSetProtected = 5

	devCreateTexture2D = 5
	ctxMap             = 14
	ctxUnmap           = 15
	ctxCopyResource    = 47
	ctxFlush           = 111

	mfFactoryCreateInstance = 3

	mfEngineSetSource          = 6
	mfEngineGetCurrentTime     = 16
	mfEngineSetCurrentTime     = 17
	mfEngineGetDuration        = 19
	mfEngineSetLoop            = 31
	mfEnginePlay               = 32
	mfEnginePause              = 33
	mfEngineSetMuted           = 35
	mfEngineSetVolume          = 37
	mfEngineGetNativeVideoSize = 40
	mfEngineShutdown           = 42
	mfEngineTransferVideoFrame = 43
	mfEngineOnVideoStreamTick  = 44
)

type winGUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

var (
	mfplatDLL                  = syscall.NewLazyDLL("mfplat.dll")
	procMFStartup              = mfplatDLL.NewProc("MFStartup")
	procMFCreateAttributes     = mfplatDLL.NewProc("MFCreateAttributes")
	procMFCreateDXGIDevManager = mfplatDLL.NewProc("MFCreateDXGIDeviceManager")
	ole32DLL                   = syscall.NewLazyDLL("ole32.dll")
	procCoInitializeEx         = ole32DLL.NewProc("CoInitializeEx")
	procCoUninitialize         = ole32DLL.NewProc("CoUninitialize")
	procCoCreateInstance       = ole32DLL.NewProc("CoCreateInstance")
	oleaut32DLL                = syscall.NewLazyDLL("oleaut32.dll")
	procSysAllocString         = oleaut32DLL.NewProc("SysAllocString")
	procSysFreeString          = oleaut32DLL.NewProc("SysFreeString")
	mfD3D11DLL                 = syscall.NewLazyDLL("d3d11.dll")
	procMFD3D11CreateDevice    = mfD3D11DLL.NewProc("D3D11CreateDevice")

	iidIUnknown                 = winGUID{0x00000000, 0x0000, 0x0000, [8]byte{0xc0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}
	clsidMFMediaEngineFactory   = winGUID{0xb44392da, 0x499b, 0x446b, [8]byte{0xa4, 0xcb, 0x00, 0x5f, 0xea, 0xd0, 0xe6, 0xd5}}
	iidIMFMediaEngineFactory    = winGUID{0x4d645ace, 0x26aa, 0x4688, [8]byte{0x9b, 0xe1, 0xdf, 0x35, 0x16, 0x99, 0x0b, 0x93}}
	iidIMFMediaEngineNotify     = winGUID{0xfee7c112, 0xe776, 0x42b5, [8]byte{0x9b, 0xbf, 0x00, 0x48, 0x52, 0x4e, 0x2b, 0xd5}}
	iidID3D10Multithread        = winGUID{0x9b7e4e00, 0x342c, 0x4106, [8]byte{0xa1, 0x9f, 0x4f, 0x27, 0x04, 0xf6, 0x89, 0xf0}}
	iidIDXGIResource            = winGUID{0x035f3ab4, 0x482e, 0x4e50, [8]byte{0xb4, 0x1f, 0x8a, 0x7f, 0x8b, 0xd8, 0x96, 0x0b}}
	mfKeyMediaEngineCallback    = winGUID{0xc60381b8, 0x83a4, 0x41f8, [8]byte{0xa3, 0xd0, 0xde, 0x05, 0x07, 0x68, 0x49, 0xa9}}
	mfKeyMediaEngineDXGIManager = winGUID{0x065702da, 0x1094, 0x486d, [8]byte{0x86, 0x17, 0xee, 0x7c, 0xc4, 0xee, 0x46, 0x48}}
	mfKeyMediaEngineVideoOutFmt = winGUID{0x5066893c, 0x8cf9, 0x42bc, [8]byte{0x8b, 0x8a, 0x47, 0x22, 0x12, 0xe5, 0x27, 0x26}}

	mfInitOnce sync.Once
	mfInitErr  error
)

type mfTex2DDesc struct {
	Width, Height, MipLevels, ArraySize uint32
	Format                              uint32
	SampleCount, SampleQuality          uint32
	Usage, BindFlags, CPUAccess, Misc   uint32
}

type mfMappedSub struct {
	Data                 uintptr
	RowPitch, DepthPitch uint32
}

type mfNormRect struct {
	Left, Top, Right, Bottom float32
}

type mfRect struct {
	Left, Top, Right, Bottom int32
}

type mfARGB struct {
	B, G, R, A byte
}

type mfTexSlot struct {
	tex          uintptr
	sharedHandle uintptr
}

type mfNotifyVtbl struct {
	QueryInterface uintptr
	AddRef         uintptr
	Release        uintptr
	EventNotify    uintptr
}

type mfNotifyObj struct {
	vtbl *mfNotifyVtbl
	id   uintptr
	refs atomic.Int32
}

var (
	mfNotifyOnce sync.Once
	mfNotifyVT   mfNotifyVtbl
	mfNotifyPin  runtime.Pinner
	mfNotifySeq  atomic.Uintptr
	mfNotifyMap  sync.Map // uintptr -> *mfPlayer
)

func initMFNotifyVtbl() {
	mfNotifyOnce.Do(func() {
		mfNotifyVT = mfNotifyVtbl{
			QueryInterface: syscall.NewCallback(mfNotifyQI),
			AddRef:         syscall.NewCallback(mfNotifyAddRef),
			Release:        syscall.NewCallback(mfNotifyRelease),
			EventNotify:    syscall.NewCallback(mfNotifyEvent),
		}
		mfNotifyPin.Pin(&mfNotifyVT)
	})
}

func mfNotifyQI(this, riid, ppv uintptr) uintptr {
	if ppv == 0 || riid == 0 {
		return eNoInterface
	}
	g := *(*winGUID)(unsafe.Pointer(riid))
	if g == iidIUnknown || g == iidIMFMediaEngineNotify {
		*(*uintptr)(unsafe.Pointer(ppv)) = this
		mfNotifyAddRef(this)
		return 0
	}
	*(*uintptr)(unsafe.Pointer(ppv)) = 0
	return eNoInterface
}

func mfNotifyAddRef(this uintptr) uintptr {
	obj := (*mfNotifyObj)(unsafe.Pointer(this))
	return uintptr(obj.refs.Add(1))
}

func mfNotifyRelease(this uintptr) uintptr {
	obj := (*mfNotifyObj)(unsafe.Pointer(this))
	return uintptr(obj.refs.Add(-1))
}

func mfNotifyEvent(this, event, param1, param2 uintptr) uintptr {
	_ = param1
	_ = param2
	obj := (*mfNotifyObj)(unsafe.Pointer(this))
	if v, ok := mfNotifyMap.Load(obj.id); ok {
		v.(*mfPlayer).onEvent(uint32(event))
	}
	return 0
}

type mfPlayer struct {
	mfMu    sync.Mutex
	d3dDev  uintptr
	d3dCtx  uintptr
	dxgiMgr uintptr
	engine  uintptr

	fnGetCurrentTime func(uintptr) float64
	fnSetCurrentTime func(uintptr, float64) uintptr
	fnGetDuration    func(uintptr) float64
	fnSetVolume      func(uintptr, float64) uintptr

	notify    *mfNotifyObj
	notifyPin runtime.Pinner
	key       string

	ring    [2]mfTexSlot
	ringW   int
	ringH   int
	ringIdx int
	staging uintptr

	rgbaBufs [2]*image.RGBA
	rgbaNext int

	stop      chan struct{}
	done      chan struct{}
	wake      chan struct{}
	startOnce sync.Once
	started   atomic.Bool
	closeOnce sync.Once

	mu          sync.Mutex
	id          shirei.ImageId
	w, h        int
	playing     bool
	ended       bool
	loop        bool
	muted       bool
	volume      float32
	closed      bool
	needFrame   bool
	seekPending bool
	seekTarget  float64
	seekAt      time.Time
	err         error
	position    time.Duration
	duration    time.Duration
}

func ensureMF() error {
	mfInitOnce.Do(func() {
		if err := procMFStartup.Find(); err != nil {
			mfInitErr = fmt.Errorf("media: mfplat.dll unavailable: %w", err)
			return
		}
		if err := procCoCreateInstance.Find(); err != nil {
			mfInitErr = fmt.Errorf("media: ole32.dll unavailable: %w", err)
			return
		}
		if err := procMFD3D11CreateDevice.Find(); err != nil {
			mfInitErr = fmt.Errorf("media: d3d11.dll unavailable: %w", err)
			return
		}
		// Ensure COM is initialized on at least one MTA thread.
		procCoInitializeEx.Call(0, coinitMultithreaded)
		hr, _, _ := procMFStartup.Call(mfVersion, mfStartupLite)
		if mfFailed(hr) {
			mfInitErr = fmt.Errorf("media: MFStartup failed (%#x)", uint32(hr))
			return
		}
		initMFNotifyVtbl()
	})
	return mfInitErr
}

func openMFPlayer(abs string) (*mfPlayer, error) {
	if err := ensureMF(); err != nil {
		return nil, err
	}
	d3dDev, d3dCtx, err := createMFD3D11Device()
	if err != nil {
		return nil, err
	}

	var resetToken uint32
	var dxgiMgr uintptr
	hr, _, _ := procMFCreateDXGIDevManager.Call(uintptr(unsafe.Pointer(&resetToken)), uintptr(unsafe.Pointer(&dxgiMgr)))
	if mfFailed(hr) || dxgiMgr == 0 {
		mfReleaseCOM(&d3dCtx)
		mfReleaseCOM(&d3dDev)
		return nil, fmt.Errorf("media: MFCreateDXGIDeviceManager failed (%#x)", uint32(hr))
	}
	hr = mfCOMCall(dxgiMgr, dxgiMgrResetDevice, d3dDev, uintptr(resetToken))
	if mfFailed(hr) {
		mfReleaseCOM(&dxgiMgr)
		mfReleaseCOM(&d3dCtx)
		mfReleaseCOM(&d3dDev)
		return nil, fmt.Errorf("media: IMFDXGIDeviceManager::ResetDevice failed (%#x)", uint32(hr))
	}

	m := &mfPlayer{
		d3dDev:    d3dDev,
		d3dCtx:    d3dCtx,
		dxgiMgr:   dxgiMgr,
		volume:    1,
		needFrame: true,
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
		wake:      make(chan struct{}, 1),
	}
	m.key = fmt.Sprintf("media/%d", nextPlayerID.Add(1))

	notifyID := mfNotifySeq.Add(1)
	m.notify = &mfNotifyObj{vtbl: &mfNotifyVT, id: notifyID}
	m.notify.refs.Store(1)
	m.notifyPin.Pin(m.notify)
	mfNotifyMap.Store(notifyID, m)

	cleanup := func() {
		mfNotifyMap.Delete(notifyID)
		m.notifyPin.Unpin()
		mfReleaseCOM(&m.engine)
		mfReleaseCOM(&m.dxgiMgr)
		mfReleaseCOM(&m.d3dCtx)
		mfReleaseCOM(&m.d3dDev)
	}

	var attrs uintptr
	hr, _, _ = procMFCreateAttributes.Call(uintptr(unsafe.Pointer(&attrs)), 3)
	if mfFailed(hr) || attrs == 0 {
		cleanup()
		return nil, fmt.Errorf("media: MFCreateAttributes failed (%#x)", uint32(hr))
	}
	defer mfReleaseCOM(&attrs)

	mfCOMCall(attrs, attrSetUnknown, uintptr(unsafe.Pointer(&mfKeyMediaEngineCallback)), uintptr(unsafe.Pointer(m.notify)))
	mfCOMCall(attrs, attrSetUnknown, uintptr(unsafe.Pointer(&mfKeyMediaEngineDXGIManager)), dxgiMgr)
	mfCOMCall(attrs, attrSetUINT32, uintptr(unsafe.Pointer(&mfKeyMediaEngineVideoOutFmt)), dxgiFmtBGRA8Unorm)

	var factory uintptr
	hr, _, _ = procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidMFMediaEngineFactory)),
		0, clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidIMFMediaEngineFactory)),
		uintptr(unsafe.Pointer(&factory)),
	)
	if mfFailed(hr) || factory == 0 {
		cleanup()
		return nil, fmt.Errorf("media: CoCreateInstance(MFMediaEngineClassFactory) failed (%#x)", uint32(hr))
	}
	defer mfReleaseCOM(&factory)

	hr = mfCOMCall(factory, mfFactoryCreateInstance, 0, attrs, uintptr(unsafe.Pointer(&m.engine)))
	if mfFailed(hr) || m.engine == 0 {
		cleanup()
		return nil, fmt.Errorf("media: IMFMediaEngineClassFactory::CreateInstance failed (%#x)", uint32(hr))
	}

	purego.RegisterFunc(&m.fnGetCurrentTime, mfCOMMethod(m.engine, mfEngineGetCurrentTime))
	purego.RegisterFunc(&m.fnSetCurrentTime, mfCOMMethod(m.engine, mfEngineSetCurrentTime))
	purego.RegisterFunc(&m.fnGetDuration, mfCOMMethod(m.engine, mfEngineGetDuration))
	purego.RegisterFunc(&m.fnSetVolume, mfCOMMethod(m.engine, mfEngineSetVolume))

	bstr := allocBSTR(abs)
	if bstr == 0 {
		cleanup()
		return nil, fmt.Errorf("media: SysAllocString failed")
	}
	hr = mfCOMCall(m.engine, mfEngineSetSource, bstr)
	freeBSTR(bstr)
	if mfFailed(hr) {
		mfCOMCall(m.engine, mfEngineShutdown)
		cleanup()
		return nil, fmt.Errorf("media: SetSource failed (%#x)", uint32(hr))
	}
	return m, nil
}

func createMFD3D11Device() (dev, ctx uintptr, err error) {
	levels := [...]uint32{d3dFeature11_0, d3dFeature10_1, d3dFeature10_0}
	var fl uint32
	flags := uintptr(d3dCreateBGRA | d3dCreateVideo)
	hr, _, _ := procMFD3D11CreateDevice.Call(
		0, d3dDriverHardware, 0, flags,
		uintptr(unsafe.Pointer(&levels[0])), uintptr(len(levels)),
		d3d11SDKVersion,
		uintptr(unsafe.Pointer(&dev)), uintptr(unsafe.Pointer(&fl)), uintptr(unsafe.Pointer(&ctx)),
	)
	if mfFailed(hr) || dev == 0 {
		// Retry without VIDEO_SUPPORT for older drivers / VMs / Wine.
		hr, _, _ = procMFD3D11CreateDevice.Call(
			0, d3dDriverHardware, 0, d3dCreateBGRA,
			uintptr(unsafe.Pointer(&levels[0])), uintptr(len(levels)),
			d3d11SDKVersion,
			uintptr(unsafe.Pointer(&dev)), uintptr(unsafe.Pointer(&fl)), uintptr(unsafe.Pointer(&ctx)),
		)
	}
	if mfFailed(hr) || dev == 0 || ctx == 0 {
		mfReleaseCOM(&ctx)
		mfReleaseCOM(&dev)
		return 0, 0, fmt.Errorf("media: D3D11CreateDevice failed (%#x)", uint32(hr))
	}
	var mt uintptr
	if !mfFailed(mfCOMCall(dev, comQueryInterface, uintptr(unsafe.Pointer(&iidID3D10Multithread)), uintptr(unsafe.Pointer(&mt)))) && mt != 0 {
		mfCOMCall(mt, d3d10MTSetProtected, 1)
		mfReleaseCOM(&mt)
	}
	return dev, ctx, nil
}

func (m *mfPlayer) onEvent(ev uint32) {
	switch ev {
	case mfEventLoadedMetadata, mfEventCanPlay:
		m.mu.Lock()
		m.needFrame = true
		m.mu.Unlock()
		m.kick()
	case mfEventSeeked:
		m.mu.Lock()
		m.seekPending = false
		m.needFrame = true
		m.mu.Unlock()
		m.kick()
	case mfEventEnded:
		m.mu.Lock()
		loop := m.loop
		if !loop {
			m.ended = true
			m.playing = false
		}
		m.mu.Unlock()
		if !loop {
			shirei.RequestNextFrame()
		}
		m.kick()
	case mfEventError:
		m.setError(fmt.Errorf("media: Media Foundation playback error"))
		m.kick()
	}
}

func (m *mfPlayer) Play() {
	m.start()
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	ended := m.ended
	m.playing = true
	m.ended = false
	m.needFrame = true
	m.mu.Unlock()

	if ended {
		m.Seek(0)
	}
	m.mfMu.Lock()
	if m.engine != 0 {
		mfCOMCall(m.engine, mfEnginePlay)
	}
	m.mfMu.Unlock()
	m.kick()
	shirei.RequestNextFrame()
}

func (m *mfPlayer) Pause() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.playing = false
	m.mu.Unlock()

	m.mfMu.Lock()
	if m.engine != 0 {
		mfCOMCall(m.engine, mfEnginePause)
	}
	m.mfMu.Unlock()
	shirei.RequestNextFrame()
}

func (m *mfPlayer) Seek(sec float64) {
	if sec < 0 {
		sec = 0
	}
	m.start()
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	if m.duration > 0 {
		max := m.duration.Seconds()
		if sec > max {
			sec = max
		}
	}
	m.position = seconds(sec)
	m.ended = false
	m.needFrame = true
	m.seekPending = true
	m.seekTarget = sec
	m.seekAt = time.Now()
	m.mu.Unlock()

	m.mfMu.Lock()
	if m.engine != 0 {
		m.fnSetCurrentTime(m.engine, sec)
	}
	m.mfMu.Unlock()
	m.kick()
	shirei.RequestNextFrame()
}

func (m *mfPlayer) SetLoop(loop bool) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.loop = loop
	m.mu.Unlock()

	v := uintptr(0)
	if loop {
		v = 1
	}
	m.mfMu.Lock()
	if m.engine != 0 {
		mfCOMCall(m.engine, mfEngineSetLoop, v)
	}
	m.mfMu.Unlock()
}

func (m *mfPlayer) SetMuted(muted bool) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.muted = muted
	m.mu.Unlock()

	v := uintptr(0)
	if muted {
		v = 1
	}
	m.mfMu.Lock()
	if m.engine != 0 {
		mfCOMCall(m.engine, mfEngineSetMuted, v)
	}
	m.mfMu.Unlock()
	shirei.RequestNextFrame()
}

func (m *mfPlayer) Muted() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.muted
}

func (m *mfPlayer) SetVolume(volume float32) {
	if volume < 0 {
		volume = 0
	} else if volume > 1 {
		volume = 1
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.volume = volume
	m.mu.Unlock()

	m.mfMu.Lock()
	if m.engine != 0 {
		m.fnSetVolume(m.engine, float64(volume))
	}
	m.mfMu.Unlock()
	shirei.RequestNextFrame()
}

func (m *mfPlayer) Volume() float32 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.volume
}

func (m *mfPlayer) Playing() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.playing
}

func (m *mfPlayer) Ended() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ended
}

func (m *mfPlayer) Position() time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.position
}

func (m *mfPlayer) Duration() time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.duration
}

func (m *mfPlayer) Size() (int, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.w, m.h
}

func (m *mfPlayer) ImageId() shirei.ImageId {
	m.start()
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.id
}

func (m *mfPlayer) Err() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.err
}

func (m *mfPlayer) View(maxSize shirei.Vec2) {
	m.start()
	m.mu.Lock()
	id, w, h := m.id, m.w, m.h
	m.mu.Unlock()
	drawVideoBox(id, w, h, maxSize)
}

func (m *mfPlayer) Close() {
	m.closeOnce.Do(func() {
		m.mu.Lock()
		m.closed = true
		m.playing = false
		m.id = 0
		m.mu.Unlock()

		close(m.stop)
		m.startOnce.Do(func() {})
		if m.started.Load() {
			<-m.done
		}

		shirei.WithFrameLock(func() {
			shirei.ReleaseImage(m.key)
		})

		m.mfMu.Lock()
		if m.engine != 0 {
			mfCOMCall(m.engine, mfEngineShutdown)
			mfReleaseCOM(&m.engine)
		}
		m.releaseTexturesLocked()
		mfReleaseCOM(&m.dxgiMgr)
		mfReleaseCOM(&m.d3dCtx)
		mfReleaseCOM(&m.d3dDev)
		m.mfMu.Unlock()

		if m.notify != nil {
			mfNotifyMap.Delete(m.notify.id)
			m.notifyPin.Unpin()
			m.notify = nil
		}
	})
}

func (m *mfPlayer) start() {
	m.mu.Lock()
	closed := m.closed
	m.mu.Unlock()
	if closed {
		return
	}
	m.startOnce.Do(func() {
		m.started.Store(true)
		go m.pump()
	})
}

func (m *mfPlayer) kick() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

func (m *mfPlayer) shouldPoll() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.err != nil {
		return false
	}
	return m.playing || m.needFrame || m.seekPending || m.duration == 0
}

func (m *mfPlayer) pump() {
	runtime.LockOSThread()
	procCoInitializeEx.Call(0, coinitMultithreaded)
	defer func() {
		procCoUninitialize.Call()
		runtime.UnlockOSThread()
		close(m.done)
	}()

	var ticker *time.Ticker
	var tickC <-chan time.Time
	defer func() {
		if ticker != nil {
			ticker.Stop()
		}
	}()

	updateTicker := func() {
		if m.shouldPoll() {
			if ticker == nil {
				ticker = time.NewTicker(16 * time.Millisecond)
				tickC = ticker.C
			}
		} else if ticker != nil {
			ticker.Stop()
			ticker = nil
			tickC = nil
		}
	}

	m.tick()
	updateTicker()

	for {
		select {
		case <-m.stop:
			return
		case <-m.wake:
			m.tick()
			updateTicker()
		case <-tickC:
			m.tick()
			updateTicker()
		}
	}
}

func (m *mfPlayer) tick() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()

	var (
		hasFrame     bool
		sharedHandle uintptr
		rgbaFrame    *image.RGBA
		w, h         int
		pos, dur     float64
	)

	m.mfMu.Lock()
	if m.engine == 0 {
		m.mfMu.Unlock()
		return
	}
	var pts int64
	hr := mfCOMCall(m.engine, mfEngineOnVideoStreamTick, uintptr(unsafe.Pointer(&pts)))
	if hr == 0 { // S_OK: a new video frame is ready
		var cw, ch uint32
		if !mfFailed(mfCOMCall(m.engine, mfEngineGetNativeVideoSize, uintptr(unsafe.Pointer(&cw)), uintptr(unsafe.Pointer(&ch)))) && cw > 0 && ch > 0 {
			w, h = int(cw), int(ch)
			if m.ensureTexturesLocked(w, h) {
				slot := &m.ring[m.ringIdx&1]
				m.ringIdx++
				src := mfNormRect{0, 0, 1, 1}
				dst := mfRect{0, 0, int32(w), int32(h)}
				border := mfARGB{0, 0, 0, 255}
				if !mfFailed(mfCOMCall(m.engine, mfEngineTransferVideoFrame,
					slot.tex,
					uintptr(unsafe.Pointer(&src)),
					uintptr(unsafe.Pointer(&dst)),
					uintptr(unsafe.Pointer(&border)))) {
					mfCOMCall(m.d3dCtx, ctxFlush)
					hasFrame = true
					if shirei.SupportsExternalKind(shirei.ExternalD3D11Texture) && slot.sharedHandle != 0 {
						sharedHandle = slot.sharedHandle
					} else {
						rgbaFrame = m.copyStagingRGBALocked(slot.tex, w, h)
					}
				}
			}
		}
	}
	pos = m.fnGetCurrentTime(m.engine)
	dur = m.fnGetDuration(m.engine)
	m.mfMu.Unlock()

	published := false
	if hasFrame {
		var id shirei.ImageId
		if sharedHandle != 0 {
			frame := shirei.ExternalFrame{
				Kind:   shirei.ExternalD3D11Texture,
				Handle: sharedHandle,
				Width:  w,
				Height: h,
			}
			shirei.WithFrameLock(func() {
				id = shirei.UseExternalImage(m.key, frame)
			})
		} else if rgbaFrame != nil {
			shirei.WithFrameLock(func() {
				id = shirei.UseImage(m.key, rgbaFrame)
			})
		}
		if id != 0 {
			m.mu.Lock()
			m.id = id
			m.w = w
			m.h = h
			m.mu.Unlock()
			shirei.RequestNextFrame()
			published = true
		}
	}

	if math.IsNaN(pos) || math.IsInf(pos, 0) || pos < 0 {
		pos = 0
	}
	if math.IsNaN(dur) || math.IsInf(dur, 0) || dur < 0 {
		dur = 0
	}

	m.mu.Lock()
	if dur > 0 {
		m.duration = seconds(dur)
	}
	if m.seekPending {
		if math.Abs(pos-m.seekTarget) < 0.25 || time.Since(m.seekAt) > 500*time.Millisecond {
			m.seekPending = false
			m.position = seconds(pos)
		}
	} else {
		m.position = seconds(pos)
	}
	if published && !m.seekPending {
		m.needFrame = false
	}
	m.mu.Unlock()
}

func (m *mfPlayer) ensureTexturesLocked(w, h int) bool {
	if m.ringW == w && m.ringH == h && m.ring[0].tex != 0 && m.ring[1].tex != 0 {
		return true
	}
	m.releaseTexturesLocked()
	desc := mfTex2DDesc{
		Width: uint32(w), Height: uint32(h), MipLevels: 1, ArraySize: 1,
		Format: dxgiFmtBGRA8Unorm, SampleCount: 1,
		Usage:     d3dUsageDefault,
		BindFlags: d3dBindRenderTarget | d3dBindSRV,
		Misc:      d3dMiscShared,
	}
	for i := range m.ring {
		var tex uintptr
		hr := mfCOMCall(m.d3dDev, devCreateTexture2D, uintptr(unsafe.Pointer(&desc)), 0, uintptr(unsafe.Pointer(&tex)))
		if mfFailed(hr) || tex == 0 {
			m.releaseTexturesLocked()
			return false
		}
		var res uintptr
		var sh uintptr
		if !mfFailed(mfCOMCall(tex, comQueryInterface, uintptr(unsafe.Pointer(&iidIDXGIResource)), uintptr(unsafe.Pointer(&res)))) && res != 0 {
			mfCOMCall(res, dxgiResGetShared, uintptr(unsafe.Pointer(&sh)))
			mfReleaseCOM(&res)
		}
		m.ring[i] = mfTexSlot{tex: tex, sharedHandle: sh}
	}
	m.ringW, m.ringH = w, h
	return true
}

func (m *mfPlayer) copyStagingRGBALocked(srcTex uintptr, w, h int) *image.RGBA {
	if m.staging == 0 {
		desc := mfTex2DDesc{
			Width: uint32(w), Height: uint32(h), MipLevels: 1, ArraySize: 1,
			Format: dxgiFmtBGRA8Unorm, SampleCount: 1,
			Usage: d3dUsageStaging, CPUAccess: d3dCPURead,
		}
		hr := mfCOMCall(m.d3dDev, devCreateTexture2D, uintptr(unsafe.Pointer(&desc)), 0, uintptr(unsafe.Pointer(&m.staging)))
		if mfFailed(hr) || m.staging == 0 {
			return nil
		}
	}
	mfCOMCall(m.d3dCtx, ctxCopyResource, m.staging, srcTex)
	var mapped mfMappedSub
	if mfFailed(mfCOMCall(m.d3dCtx, ctxMap, m.staging, 0, d3dMapRead, 0, uintptr(unsafe.Pointer(&mapped)))) || mapped.Data == 0 {
		return nil
	}
	defer mfCOMCall(m.d3dCtx, ctxUnmap, m.staging, 0)

	pitch := int(mapped.RowPitch)
	if pitch < w*4 {
		return nil
	}
	slot := m.rgbaNext & 1
	m.rgbaNext++
	rgba := m.rgbaBufs[slot]
	if rgba == nil || rgba.Rect.Dx() != w || rgba.Rect.Dy() != h {
		rgba = image.NewRGBA(image.Rect(0, 0, w, h))
		m.rgbaBufs[slot] = rgba
	}
	src := unsafe.Slice((*byte)(unsafe.Pointer(mapped.Data)), h*pitch)
	dst := rgba.Pix
	rowBytes := w * 4
	for y := 0; y < h; y++ {
		sRow := src[y*pitch : y*pitch+rowBytes]
		dRow := dst[y*rowBytes : y*rowBytes+rowBytes]
		for i := 0; i < rowBytes; i += 4 {
			dRow[i+0] = sRow[i+2]
			dRow[i+1] = sRow[i+1]
			dRow[i+2] = sRow[i+0]
			dRow[i+3] = sRow[i+3]
		}
	}
	return rgba
}

func (m *mfPlayer) releaseTexturesLocked() {
	mfReleaseCOM(&m.staging)
	for i := range m.ring {
		mfReleaseCOM(&m.ring[i].tex)
		m.ring[i].sharedHandle = 0
	}
	m.ringW, m.ringH = 0, 0
}

func (m *mfPlayer) setError(err error) {
	if err == nil {
		return
	}
	m.mu.Lock()
	if m.err != nil {
		m.mu.Unlock()
		return
	}
	m.err = err
	m.playing = false
	m.needFrame = false
	m.mu.Unlock()
	shirei.RequestNextFrame()
}

func mfCOMMethod(obj uintptr, slot int) uintptr {
	vt := *(*uintptr)(unsafe.Pointer(obj))
	return *(*uintptr)(unsafe.Pointer(vt + uintptr(slot)*unsafe.Sizeof(uintptr(0))))
}

func mfCOMCall(obj uintptr, slot int, args ...uintptr) uintptr {
	all := make([]uintptr, 1+len(args))
	all[0] = obj
	copy(all[1:], args)
	r, _, _ := syscall.SyscallN(mfCOMMethod(obj, slot), all...)
	return r
}

func mfReleaseCOM(p *uintptr) {
	if *p != 0 {
		mfCOMCall(*p, comRelease)
		*p = 0
	}
}

func mfFailed(hr uintptr) bool { return int32(hr) < 0 }

func allocBSTR(s string) uintptr {
	u := utf16.Encode([]rune(s))
	u = append(u, 0)
	r, _, _ := procSysAllocString.Call(uintptr(unsafe.Pointer(&u[0])))
	return r
}

func freeBSTR(b uintptr) {
	if b != 0 {
		procSysFreeString.Call(b)
	}
}
