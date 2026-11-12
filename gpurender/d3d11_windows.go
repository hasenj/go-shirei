//go:build windows

package gpurender

import (
	"fmt"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"go.hasen.dev/shirei"
)

// D3D11 submit into a GDI-compatible BGRA texture. The shell presents from
// PresentDC after Render, preserving alpha for layered windows.

const (
	d3d11SDKVersion     = 7
	d3dDriverUnknown    = 0
	d3dDriverHardware   = 1
	d3dCreateBGRA       = 0x20
	d3dFeature11_0      = 0xb000
	d3dFeature10_1      = 0xa100
	d3dFeature10_0      = 0xa000
	d3dFeature9_3       = 0x9300
	dxgiFmtRGBA8Unorm   = 28
	dxgiFmtR8Unorm      = 61
	dxgiFmtBGRA8Unorm   = 87
	dxgiFmtRGBA32Float  = 2
	d3dUsageDefault     = 0
	d3dUsageDynamic     = 2
	d3dBindVertexBuf    = 0x1
	d3dBindConstBuf     = 0x4
	d3dBindSRV          = 0x8
	d3dBindRenderTarget = 0x20
	d3dCPUWrite         = 0x10000
	d3dMiscGDICompat    = 0x200
	d3dMapWriteDiscard  = 4
	d3dInputPerInstance = 1
	d3dTopoTriList      = 4
	d3dBlendOne         = 2
	d3dBlendInvSrcAlpha = 6
	d3dBlendOpAdd       = 1
	d3dFillSolid        = 3
	d3dCullNone         = 1
	d3dFilterLinear     = 0x15
	d3dAddrClamp        = 3
	d3dCmpNever         = 1

	iunkRelease = 2
	blobPtr     = 3
	blobSize    = 4

	devCreateBuffer           = 3
	devCreateTexture2D        = 5
	devCreateSRV              = 7
	devCreateRenderTargetView = 9
	devCreateInputLayout      = 11
	devCreateVertexShader     = 12
	devCreatePixelShader      = 15
	devCreateBlendState       = 20
	devCreateRasterizerState  = 22
	devCreateSamplerState     = 23

	ctxVSSetConstantBuffers   = 7
	ctxPSSetShaderResources   = 8
	ctxPSSetShader            = 9
	ctxPSSetSamplers          = 10
	ctxVSSetShader            = 11
	ctxMap                    = 14
	ctxUnmap                  = 15
	ctxPSSetConstantBuffers   = 16
	ctxIASetInputLayout       = 17
	ctxIASetVertexBuffers     = 18
	ctxDrawInstanced          = 21
	ctxIASetPrimitiveTopology = 24
	ctxOMSetRenderTargets     = 33
	ctxOMSetBlendState        = 35
	ctxRSSetState             = 43
	ctxRSSetViewports         = 44
	ctxRSSetScissorRects      = 45
	ctxUpdateSubresource      = 48
	ctxClearRenderTargetView  = 50
	ctxFlush                  = 111

	dxgiDevGetAdapter = 7
	dxgiAdpGetDesc    = 8
	surf1GetDC        = 11
	surf1ReleaseDC    = 12
)

const d3dShaderSrc = `
cbuffer Uni : register(b0) {
    float2 viewport;
    uint mode;
    uint clipN;
    float4 clipRects[4];
    float4 clipRads[4];
};

struct VSIn {
    float4 dst : TEXCOORD0;
    float4 uv : TEXCOORD1;
    float4 color : TEXCOORD2;
    float4 color2 : TEXCOORD3;
};

struct VSOut {
    float4 pos : SV_Position;
    float4 color : COLOR0;
    float4 color2 : COLOR1;
    float2 uv : TEXCOORD0;
    float2 corner : TEXCOORD1;
};

VSOut vs_main(VSIn inn, uint vid : SV_VertexID) {
    float2 c = float2(
        (vid == 1 || vid == 3 || vid == 4) ? 1.0 : 0.0,
        (vid == 2 || vid == 4 || vid == 5) ? 1.0 : 0.0);
    float2 pos = inn.dst.xy + c * inn.dst.zw;
    VSOut o;
    o.pos = float4(pos.x / viewport.x * 2.0 - 1.0, 1.0 - pos.y / viewport.y * 2.0, 0.0, 1.0);
    o.color = inn.color;
    o.color2 = inn.color2;
    o.uv = inn.uv.xy + c * inn.uv.zw;
    o.corner = c;
    return o;
}

Texture2D tex : register(t0);
SamplerState samp : register(s0);

float sdRoundBox(float2 p, float2 b, float4 r) {
    r.xy = (p.x > 0.0) ? r.xy : r.zw;
    r.x  = (p.y > 0.0) ? r.x  : r.y;
    float2 q = abs(p) - b + r.x;
    return min(max(q.x, q.y), 0.0) + length(max(q, 0.0)) - r.x;
}

float clipCov(float2 pos) {
    float cov = 1.0;
    [loop] for (uint i = 0; i < clipN; i++) {
        float4 rc = clipRects[i];
        float2 halfv = rc.zw * 0.5;
        float2 p = pos - (rc.xy + halfv);
        p.y = -p.y;
        float4 rad = clipRads[i];
        float4 r = float4(rad.y, rad.z, rad.x, rad.w);
        cov *= saturate(0.5 - sdRoundBox(p, halfv, r));
    }
    return cov;
}

float4 ps_main(VSOut inn) : SV_Target {
    float4 outc;
    if (mode == 0) {
        float4 c = lerp(inn.color, inn.color2, inn.corner.y);
        outc = float4(c.rgb * c.a, c.a);
    } else if (mode == 1) {
        float cov = tex.Sample(samp, inn.uv).r;
        float4 c = lerp(inn.color, inn.color2, inn.corner.y);
        float a = c.a * cov;
        outc = float4(c.rgb * a, a);
    } else {
        float4 t = tex.Sample(samp, inn.uv);
        outc = t * inn.color.a;
    }
    return outc * clipCov(inn.pos.xy);
}
`

var (
	d3d11                    = syscall.NewLazyDLL("d3d11.dll")
	procD3D11CreateDevice    = d3d11.NewProc("D3D11CreateDevice")
	d3dcomp                  = syscall.NewLazyDLL("d3dcompiler_47.dll")
	procD3DCompile           = d3dcomp.NewProc("D3DCompile")
	dxgi                     = syscall.NewLazyDLL("dxgi.dll")
	procCreateDXGIFactory1   = dxgi.NewProc("CreateDXGIFactory1")
	k32                      = syscall.NewLazyDLL("kernel32.dll")
	procGetSystemPowerStatus = k32.NewProc("GetSystemPowerStatus")

	iidDXGISurface1 = guid{0x4ae63092, 0x6327, 0x4c1b, [8]byte{0x80, 0xae, 0xbf, 0xe1, 0x2e, 0xa3, 0x2b, 0x86}}
	iidDXGIDevice   = guid{0x54ec77fa, 0x1377, 0x44e6, [8]byte{0x8c, 0x32, 0x88, 0xfd, 0x5f, 0x44, 0xc8, 0x4c}}
	iidDXGIFactory1 = guid{0x770aae78, 0xf26f, 0x4dba, [8]byte{0xa8, 0x29, 0x25, 0x3c, 0x83, 0xd1, 0xb3, 0x87}}

	semTEXCOORD = [9]byte{'T', 'E', 'X', 'C', 'O', 'O', 'R', 'D', 0}
)

type guid struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

type tex2dDesc struct {
	Width, Height, MipLevels, ArraySize uint32
	Format                              uint32
	SampleCount, SampleQuality          uint32
	Usage, BindFlags, CPUAccess, Misc   uint32
}

type bufDesc struct {
	ByteWidth, Usage, BindFlags, CPUAccess, Misc, Stride uint32
}

type mappedSub struct {
	Data                 uintptr
	RowPitch, DepthPitch uint32
}

type d3dBox struct {
	Left, Top, Front, Right, Bottom, Back uint32
}

type inputElem struct {
	SemanticName         *byte
	SemanticIndex        uint32
	Format               uint32
	InputSlot            uint32
	AlignedByteOffset    uint32
	InputSlotClass       uint32
	InstanceDataStepRate uint32
}

type rtBlend struct {
	BlendEnable           uint32
	SrcBlend              uint32
	DestBlend             uint32
	BlendOp               uint32
	SrcBlendAlpha         uint32
	DestBlendAlpha        uint32
	BlendOpAlpha          uint32
	RenderTargetWriteMask uint8
	_                     [3]byte
}

type blendDesc struct {
	AlphaToCoverageEnable  uint32
	IndependentBlendEnable uint32
	RenderTarget           [8]rtBlend
}

type rasterDesc struct {
	FillMode, CullMode                       uint32
	FrontCounterClockwise                    uint32
	DepthBias                                int32
	DepthBiasClamp, SlopeScaledDepthBias     float32
	DepthClipEnable, ScissorEnable           uint32
	MultisampleEnable, AntialiasedLineEnable uint32
}

type samplerDesc struct {
	Filter                       uint32
	AddressU, AddressV, AddressW uint32
	MipLODBias                   float32
	MaxAnisotropy                uint32
	ComparisonFunc               uint32
	BorderColor                  [4]float32
	MinLOD, MaxLOD               float32
}

type viewport struct {
	TopLeftX, TopLeftY, Width, Height, MinDepth, MaxDepth float32
}

type d3dRect struct{ Left, Top, Right, Bottom int32 }

type d3dUni struct {
	ViewportX, ViewportY float32
	Mode, ClipN          uint32
	ClipRect             [4][4]float32
	ClipRad              [4][4]float32
}

type adapterDesc struct {
	Description           [128]uint16
	VendorId              uint32
	DeviceId              uint32
	SubSysId              uint32
	Revision              uint32
	DedicatedVideoMemory  uintptr
	DedicatedSystemMemory uintptr
	SharedSystemMemory    uintptr
	AdapterLuid           uint64
}

type d3dImg struct {
	tex, srv uintptr
	w, h     int
}

var (
	d3dReady   bool
	d3dDevName string
	d3dDev     uintptr
	d3dCtx     uintptr

	d3dVS, d3dPS, d3dLayout   uintptr
	d3dBlend, d3dRaster       uintptr
	d3dSamp, d3dCBuf, d3dInst uintptr
	d3dInstBytes              int
	d3dWhiteTex, d3dWhiteSRV  uintptr
	d3dGlyphTex, d3dGlyphSRV  uintptr
	d3dColorTex, d3dColorSRV  uintptr
	d3dImages                 map[uint32]d3dImg
)

type d3dTarget struct {
	tex, rtv, surf, hdc uintptr
	w, h                int
}

var d3dTargets = map[unsafe.Pointer]*d3dTarget{}

func DeviceName() string { return d3dDevName }

func WaitIdle() {
	if d3dCtx != 0 {
		comCall(d3dCtx, ctxFlush)
	}
}

func Forget(dest unsafe.Pointer) {
	if target := d3dTargets[dest]; target != nil {
		target.release()
		delete(d3dTargets, dest)
	}
}

func gpuInit() error {
	if d3dReady {
		return nil
	}
	if err := procD3D11CreateDevice.Find(); err != nil {
		return fmt.Errorf("d3d11.dll: %w", err)
	}
	if err := procD3DCompile.Find(); err != nil {
		d3dcomp = syscall.NewLazyDLL("d3dcompiler_43.dll")
		procD3DCompile = d3dcomp.NewProc("D3DCompile")
		if err := procD3DCompile.Find(); err != nil {
			return fmt.Errorf("d3dcompiler: %w", err)
		}
	}
	adp, err := pickAdapter()
	if err != nil {
		return err
	}
	defer releaseCOM(&adp)
	driver := uintptr(d3dDriverHardware)
	if adp != 0 {
		driver = d3dDriverUnknown
	}
	levels := [...]uint32{d3dFeature11_0, d3dFeature10_1, d3dFeature10_0}
	var fl uint32
	hr, _, _ := procD3D11CreateDevice.Call(
		adp,
		driver,
		0,
		uintptr(d3dCreateBGRA),
		uintptr(unsafe.Pointer(&levels[0])),
		uintptr(len(levels)),
		d3d11SDKVersion,
		uintptr(unsafe.Pointer(&d3dDev)),
		uintptr(unsafe.Pointer(&fl)),
		uintptr(unsafe.Pointer(&d3dCtx)),
	)
	if failed(hr) || d3dDev == 0 || d3dCtx == 0 {
		releaseCOM(&d3dDev)
		releaseCOM(&d3dCtx)
		return fmt.Errorf("D3D11CreateDevice hr=%s", hrStr(hr))
	}
	if err := createPipeline(); err != nil {
		Release()
		return err
	}
	d3dDevName = adapterName(d3dDev)
	if d3dDevName == "" {
		d3dDevName = fmt.Sprintf("D3D11 fl=%#x", fl)
	} else {
		d3dDevName = fmt.Sprintf("%s fl=%#x", d3dDevName, fl)
	}
	d3dImages = make(map[uint32]d3dImg)
	d3dReady = true
	return nil
}

func createPipeline() error {
	vsBlob, err := compileHLSL("vs_main", "vs_4_0")
	if err != nil {
		return err
	}
	defer releaseCOM(&vsBlob)
	psBlob, err := compileHLSL("ps_main", "ps_4_0")
	if err != nil {
		return err
	}
	defer releaseCOM(&psBlob)

	vsPtr, vsLen := comCall(vsBlob, blobPtr), comCall(vsBlob, blobSize)
	psPtr, psLen := comCall(psBlob, blobPtr), comCall(psBlob, blobSize)
	hr := comCall(d3dDev, devCreateVertexShader, vsPtr, vsLen, 0, uintptr(unsafe.Pointer(&d3dVS)))
	if failed(hr) || d3dVS == 0 {
		return fmt.Errorf("CreateVertexShader hr=%s", hrStr(hr))
	}
	hr = comCall(d3dDev, devCreatePixelShader, psPtr, psLen, 0, uintptr(unsafe.Pointer(&d3dPS)))
	if failed(hr) || d3dPS == 0 {
		return fmt.Errorf("CreatePixelShader hr=%s", hrStr(hr))
	}

	// HLSL "TEXCOORD0" is semantic TEXCOORD index 0, not the name "TEXCOORD0".
	elems := [4]inputElem{
		{&semTEXCOORD[0], 0, dxgiFmtRGBA32Float, 0, 0, d3dInputPerInstance, 1},
		{&semTEXCOORD[0], 1, dxgiFmtRGBA32Float, 0, 16, d3dInputPerInstance, 1},
		{&semTEXCOORD[0], 2, dxgiFmtRGBA32Float, 0, 32, d3dInputPerInstance, 1},
		{&semTEXCOORD[0], 3, dxgiFmtRGBA32Float, 0, 48, d3dInputPerInstance, 1},
	}
	hr = comCall(d3dDev, devCreateInputLayout,
		uintptr(unsafe.Pointer(&elems[0])), 4, vsPtr, vsLen, uintptr(unsafe.Pointer(&d3dLayout)))
	if failed(hr) || d3dLayout == 0 {
		return fmt.Errorf("CreateInputLayout hr=%s", hrStr(hr))
	}

	var bd blendDesc
	rt := &bd.RenderTarget[0]
	rt.BlendEnable = 1
	rt.SrcBlend, rt.DestBlend, rt.BlendOp = d3dBlendOne, d3dBlendInvSrcAlpha, d3dBlendOpAdd
	rt.SrcBlendAlpha, rt.DestBlendAlpha, rt.BlendOpAlpha = d3dBlendOne, d3dBlendInvSrcAlpha, d3dBlendOpAdd
	rt.RenderTargetWriteMask = 0x0f
	hr = comCall(d3dDev, devCreateBlendState, uintptr(unsafe.Pointer(&bd)), uintptr(unsafe.Pointer(&d3dBlend)))
	if failed(hr) || d3dBlend == 0 {
		return fmt.Errorf("CreateBlendState hr=%s", hrStr(hr))
	}

	rd := rasterDesc{FillMode: d3dFillSolid, CullMode: d3dCullNone, DepthClipEnable: 1, ScissorEnable: 1}
	hr = comCall(d3dDev, devCreateRasterizerState, uintptr(unsafe.Pointer(&rd)), uintptr(unsafe.Pointer(&d3dRaster)))
	if failed(hr) || d3dRaster == 0 {
		return fmt.Errorf("CreateRasterizerState hr=%s", hrStr(hr))
	}

	sd := samplerDesc{
		Filter: d3dFilterLinear, AddressU: d3dAddrClamp, AddressV: d3dAddrClamp, AddressW: d3dAddrClamp,
		MaxAnisotropy: 1, ComparisonFunc: d3dCmpNever, MaxLOD: 3.402823466e+38,
	}
	hr = comCall(d3dDev, devCreateSamplerState, uintptr(unsafe.Pointer(&sd)), uintptr(unsafe.Pointer(&d3dSamp)))
	if failed(hr) || d3dSamp == 0 {
		return fmt.Errorf("CreateSamplerState hr=%s", hrStr(hr))
	}

	cbd := bufDesc{ByteWidth: uint32((unsafe.Sizeof(d3dUni{}) + 15) &^ 15), Usage: d3dUsageDefault, BindFlags: d3dBindConstBuf}
	hr = comCall(d3dDev, devCreateBuffer, uintptr(unsafe.Pointer(&cbd)), 0, uintptr(unsafe.Pointer(&d3dCBuf)))
	if failed(hr) || d3dCBuf == 0 {
		return fmt.Errorf("CreateBuffer cbuf hr=%s", hrStr(hr))
	}

	white := [4]byte{255, 255, 255, 255}
	d3dWhiteTex, d3dWhiteSRV, err = createTex(1, 1, dxgiFmtRGBA8Unorm, d3dBindSRV, 0, white[:], 4)
	if err != nil {
		return err
	}
	d3dGlyphTex, d3dGlyphSRV, err = createTex(glyphAtlasW, glyphAtlasH, dxgiFmtR8Unorm, d3dBindSRV, 0, nil, 0)
	if err != nil {
		return err
	}
	d3dColorTex, d3dColorSRV, err = createTex(colorAtlasW, colorAtlasH, dxgiFmtRGBA8Unorm, d3dBindSRV, 0, nil, 0)
	if err != nil {
		return err
	}
	zeroTex(d3dGlyphTex, glyphAtlasW, glyphAtlasH, 1)
	zeroTex(d3dColorTex, colorAtlasW, colorAtlasH, 4)

	comCall(d3dCtx, ctxIASetPrimitiveTopology, d3dTopoTriList)
	comCall(d3dCtx, ctxIASetInputLayout, d3dLayout)
	comCall(d3dCtx, ctxVSSetShader, d3dVS, 0, 0)
	comCall(d3dCtx, ctxPSSetShader, d3dPS, 0, 0)
	comCall(d3dCtx, ctxRSSetState, d3dRaster)
	factor := [4]float32{}
	comCall(d3dCtx, ctxOMSetBlendState, d3dBlend, uintptr(unsafe.Pointer(&factor[0])), 0xffffffff)
	samp := d3dSamp
	comCall(d3dCtx, ctxPSSetSamplers, 0, 1, uintptr(unsafe.Pointer(&samp)))
	cbuf := d3dCBuf
	comCall(d3dCtx, ctxVSSetConstantBuffers, 0, 1, uintptr(unsafe.Pointer(&cbuf)))
	comCall(d3dCtx, ctxPSSetConstantBuffers, 0, 1, uintptr(unsafe.Pointer(&cbuf)))
	return nil
}

func compileHLSL(entry, target string) (uintptr, error) {
	src := append([]byte(d3dShaderSrc), 0)
	ep, _ := syscall.BytePtrFromString(entry)
	tg, _ := syscall.BytePtrFromString(target)
	name, _ := syscall.BytePtrFromString("shirei")
	var code, errmsg uintptr
	hr, _, _ := procD3DCompile.Call(
		uintptr(unsafe.Pointer(&src[0])), uintptr(len(d3dShaderSrc)),
		uintptr(unsafe.Pointer(name)), 0, 0,
		uintptr(unsafe.Pointer(ep)), uintptr(unsafe.Pointer(tg)),
		0, 0,
		uintptr(unsafe.Pointer(&code)), uintptr(unsafe.Pointer(&errmsg)),
	)
	if failed(hr) || code == 0 {
		msg := blobString(errmsg)
		if msg != "" {
			msg = string(append([]byte(nil), msg...))
		}
		releaseCOM(&errmsg)
		return 0, fmt.Errorf("D3DCompile %s: %s hr=%s", entry, msg, hrStr(hr))
	}
	releaseCOM(&errmsg)
	return code, nil
}

func blobString(b uintptr) string {
	if b == 0 {
		return ""
	}
	p, n := comCall(b, blobPtr), comCall(b, blobSize)
	if p == 0 || n == 0 {
		return ""
	}
	return unsafe.String((*byte)(unsafe.Pointer(p)), int(n))
}

func gpuSubmit(dest unsafe.Pointer, w, h int, quads []Quad, batches []Batch, uploads []gpuUpload, wait bool) (encodeNs, waitNs int64, err error) {
	_ = wait
	if !d3dReady {
		return 0, 0, fmt.Errorf("d3d11 not initialized")
	}
	if w <= 0 || h <= 0 {
		return 0, 0, fmt.Errorf("bad target")
	}
	t0 := time.Now()
	target := d3dTargets[dest]
	if target == nil {
		target = &d3dTarget{}
		d3dTargets[dest] = target
	}
	if err := target.ensure(w, h); err != nil {
		return 0, 0, err
	}
	applyUploads(uploads)
	if err := uploadQuads(quads); err != nil {
		return 0, 0, err
	}

	rtv := target.rtv
	comCall(d3dCtx, ctxOMSetRenderTargets, 1, uintptr(unsafe.Pointer(&rtv)), 0)
	vp := viewport{Width: float32(w), Height: float32(h), MaxDepth: 1}
	comCall(d3dCtx, ctxRSSetViewports, 1, uintptr(unsafe.Pointer(&vp)))
	if !defaultB.skipClear {
		color := [4]float32{1, 1, 1, 1}
		if shirei.GetHost().TransparentBackground {
			color = [4]float32{}
		}
		comCall(d3dCtx, ctxClearRenderTargetView, target.rtv, uintptr(unsafe.Pointer(&color[0])))
	}

	stride := uint32(unsafe.Sizeof(Quad{}))
	off := uint32(0)
	inst := d3dInst
	comCall(d3dCtx, ctxIASetVertexBuffers, 0, 1, uintptr(unsafe.Pointer(&inst)), uintptr(unsafe.Pointer(&stride)), uintptr(unsafe.Pointer(&off)))

	for i := range batches {
		b := &batches[i]
		if b.Count <= 0 || b.ClipW <= 0 || b.ClipH <= 0 {
			continue
		}
		x, y, cw, ch := int(b.ClipX), int(b.ClipY), int(b.ClipW), int(b.ClipH)
		if x < 0 {
			cw += x
			x = 0
		}
		if y < 0 {
			ch += y
			y = 0
		}
		if x+cw > w {
			cw = w - x
		}
		if y+ch > h {
			ch = h - y
		}
		if cw <= 0 || ch <= 0 {
			continue
		}
		sc := d3dRect{int32(x), int32(y), int32(x + cw), int32(y + ch)}
		comCall(d3dCtx, ctxRSSetScissorRects, 1, uintptr(unsafe.Pointer(&sc)))

		srv := d3dWhiteSRV
		mode := uint32(0)
		switch b.TexKind {
		case texGlyph:
			srv = d3dGlyphSRV
			mode = 1
		case texColorGlyph:
			srv = d3dColorSRV
			mode = 2
		case texImage:
			if im, ok := d3dImages[uint32(b.TexKey)]; ok {
				srv = im.srv
			}
			mode = 2
		}
		comCall(d3dCtx, ctxPSSetShaderResources, 0, 1, uintptr(unsafe.Pointer(&srv)))

		uni := d3dUni{
			ViewportX: float32(w), ViewportY: float32(h),
			Mode: mode, ClipN: uint32(b.ClipN),
			ClipRect: b.ClipRect, ClipRad: b.ClipRad,
		}
		if uni.ClipN > 4 {
			uni.ClipN = 4
		}
		comCall(d3dCtx, ctxUpdateSubresource, d3dCBuf, 0, 0, uintptr(unsafe.Pointer(&uni)), 0, 0)
		comCall(d3dCtx, ctxDrawInstanced, 6, uintptr(b.Count), 0, uintptr(b.First))
	}

	comCall(d3dCtx, ctxOMSetRenderTargets, 0, 0, 0)
	comCall(d3dCtx, ctxFlush)
	return time.Since(t0).Nanoseconds(), 0, nil
}

func uploadQuads(quads []Quad) error {
	n := len(quads)
	if n == 0 {
		return nil
	}
	bytes := n * int(unsafe.Sizeof(Quad{}))
	if d3dInst == 0 || d3dInstBytes < bytes {
		releaseCOM(&d3dInst)
		cap := bytes * 2
		if cap < 4096 {
			cap = 4096
		}
		bd := bufDesc{ByteWidth: uint32(cap), Usage: d3dUsageDynamic, BindFlags: d3dBindVertexBuf, CPUAccess: d3dCPUWrite}
		hr := comCall(d3dDev, devCreateBuffer, uintptr(unsafe.Pointer(&bd)), 0, uintptr(unsafe.Pointer(&d3dInst)))
		if failed(hr) || d3dInst == 0 {
			return fmt.Errorf("CreateBuffer inst hr=%s", hrStr(hr))
		}
		d3dInstBytes = cap
	}
	var mapped mappedSub
	hr := comCall(d3dCtx, ctxMap, d3dInst, 0, d3dMapWriteDiscard, 0, uintptr(unsafe.Pointer(&mapped)))
	if failed(hr) || mapped.Data == 0 {
		return fmt.Errorf("Map inst hr=%s", hrStr(hr))
	}
	copy(unsafe.Slice((*byte)(unsafe.Pointer(mapped.Data)), bytes), unsafe.Slice((*byte)(unsafe.Pointer(&quads[0])), bytes))
	comCall(d3dCtx, ctxUnmap, d3dInst, 0)
	return nil
}

func applyUploads(uploads []gpuUpload) {
	for i := range uploads {
		u := &uploads[i]
		if len(u.pix) == 0 || u.w <= 0 || u.h <= 0 {
			continue
		}
		var tex uintptr
		var bpp int
		switch u.kind {
		case 1:
			tex, bpp = d3dGlyphTex, 1
		case 2:
			tex, bpp = d3dColorTex, 4
		case 3:
			if im, ok := d3dImages[u.imageID]; ok {
				tex = im.tex
			}
			bpp = 4
		}
		if tex == 0 {
			continue
		}
		box := d3dBox{uint32(u.x), uint32(u.y), 0, uint32(u.x + u.w), uint32(u.y + u.h), 1}
		comCall(d3dCtx, ctxUpdateSubresource, tex, 0, uintptr(unsafe.Pointer(&box)),
			uintptr(unsafe.Pointer(&u.pix[0])), uintptr(u.stride), 0)
		_ = bpp
	}
}

func gpuResetAtlases() {
	if !d3dReady {
		return
	}
	zeroTex(d3dGlyphTex, glyphAtlasW, glyphAtlasH, 1)
	zeroTex(d3dColorTex, colorAtlasW, colorAtlasH, 4)
}

func gpuImageEnsure(id uint32, w, h int) bool {
	if !d3dReady || w <= 0 || h <= 0 {
		return false
	}
	if m, ok := d3dImages[id]; ok && m.w == w && m.h == h {
		return true
	}
	if m, ok := d3dImages[id]; ok {
		releaseCOM(&m.srv)
		releaseCOM(&m.tex)
		delete(d3dImages, id)
	}
	tex, srv, err := createTex(w, h, dxgiFmtRGBA8Unorm, d3dBindSRV, 0, nil, 0)
	if err != nil {
		return false
	}
	d3dImages[id] = d3dImg{tex: tex, srv: srv, w: w, h: h}
	return true
}

func gpuImageForget(id uint32) {
	m, ok := d3dImages[id]
	if !ok {
		return
	}
	releaseCOM(&m.srv)
	releaseCOM(&m.tex)
	delete(d3dImages, id)
}

func PresentDC(dest unsafe.Pointer) (uintptr, error) {
	target := d3dTargets[dest]
	if target == nil {
		return 0, fmt.Errorf("no presentation target")
	}
	if target.hdc != 0 {
		return target.hdc, nil
	}
	if target.surf == 0 {
		return 0, fmt.Errorf("no GDI surface")
	}
	var hdc uintptr
	hr := comCall(target.surf, surf1GetDC, 0, uintptr(unsafe.Pointer(&hdc)))
	if failed(hr) || hdc == 0 {
		return 0, fmt.Errorf("IDXGISurface1.GetDC hr=%s", hrStr(hr))
	}
	target.hdc = hdc
	return hdc, nil
}

func ReleasePresentDC(dest unsafe.Pointer) {
	if target := d3dTargets[dest]; target != nil {
		target.releaseDC()
	}
}

func (target *d3dTarget) releaseDC() {
	if target.hdc == 0 || target.surf == 0 {
		target.hdc = 0
		return
	}
	comCall(target.surf, surf1ReleaseDC, 0)
	target.hdc = 0
}

func Release() {
	for dest := range d3dTargets {
		Forget(dest)
	}
	for id := range d3dImages {
		gpuImageForget(id)
	}
	releaseCOM(&d3dInst)
	d3dInstBytes = 0
	releaseCOM(&d3dWhiteSRV)
	releaseCOM(&d3dWhiteTex)
	releaseCOM(&d3dGlyphSRV)
	releaseCOM(&d3dGlyphTex)
	releaseCOM(&d3dColorSRV)
	releaseCOM(&d3dColorTex)
	releaseCOM(&d3dCBuf)
	releaseCOM(&d3dSamp)
	releaseCOM(&d3dRaster)
	releaseCOM(&d3dBlend)
	releaseCOM(&d3dLayout)
	releaseCOM(&d3dPS)
	releaseCOM(&d3dVS)
	releaseCOM(&d3dCtx)
	releaseCOM(&d3dDev)
	d3dReady = false
	d3dDevName = ""
}

func (target *d3dTarget) ensure(w, h int) error {
	if target.tex != 0 && target.w == w && target.h == h {
		return nil
	}
	target.release()

	tex, _, err := createTex(w, h, dxgiFmtBGRA8Unorm, d3dBindRenderTarget, d3dMiscGDICompat, nil, 0)
	if err != nil {
		return fmt.Errorf("GDI_COMPATIBLE: %w", err)
	}
	var rtv uintptr
	hr := comCall(d3dDev, devCreateRenderTargetView, tex, 0, uintptr(unsafe.Pointer(&rtv)))
	if failed(hr) || rtv == 0 {
		releaseCOM(&tex)
		return fmt.Errorf("CreateRenderTargetView hr=%s", hrStr(hr))
	}
	var surf uintptr
	hr = comQI(tex, &iidDXGISurface1, &surf)
	if failed(hr) || surf == 0 {
		releaseCOM(&rtv)
		releaseCOM(&tex)
		return fmt.Errorf("QueryInterface IDXGISurface1 hr=%s", hrStr(hr))
	}
	target.tex, target.rtv, target.surf = tex, rtv, surf
	target.w, target.h = w, h
	return nil
}

func (target *d3dTarget) release() {
	target.releaseDC()
	releaseCOM(&target.surf)
	releaseCOM(&target.rtv)
	releaseCOM(&target.tex)
	target.w, target.h = 0, 0
}

func createTex(w, h int, format, bind, misc uint32, pix []byte, stride int) (tex, srv uintptr, err error) {
	desc := tex2dDesc{
		Width: uint32(w), Height: uint32(h), MipLevels: 1, ArraySize: 1,
		Format: format, SampleCount: 1, Usage: d3dUsageDefault, BindFlags: bind, Misc: misc,
	}
	var init *struct {
		SysMem                   uintptr
		SysMemPitch, SysMemSlice uint32
	}
	var initBuf struct {
		SysMem                   uintptr
		SysMemPitch, SysMemSlice uint32
	}
	if len(pix) > 0 {
		initBuf.SysMem = uintptr(unsafe.Pointer(&pix[0]))
		initBuf.SysMemPitch = uint32(stride)
		init = &initBuf
	}
	var initArg uintptr
	if init != nil {
		initArg = uintptr(unsafe.Pointer(init))
	}
	hr := comCall(d3dDev, devCreateTexture2D, uintptr(unsafe.Pointer(&desc)), initArg, uintptr(unsafe.Pointer(&tex)))
	if failed(hr) || tex == 0 {
		return 0, 0, fmt.Errorf("CreateTexture2D %dx%d fmt=%d hr=%s", w, h, format, hrStr(hr))
	}
	if bind&d3dBindSRV != 0 {
		hr = comCall(d3dDev, devCreateSRV, tex, 0, uintptr(unsafe.Pointer(&srv)))
		if failed(hr) || srv == 0 {
			releaseCOM(&tex)
			return 0, 0, fmt.Errorf("CreateShaderResourceView hr=%s", hrStr(hr))
		}
	}
	return tex, srv, nil
}

func zeroTex(tex uintptr, w, h, bpp int) {
	if tex == 0 {
		return
	}
	z := make([]byte, w*h*bpp)
	comCall(d3dCtx, ctxUpdateSubresource, tex, 0, 0, uintptr(unsafe.Pointer(&z[0])), uintptr(w*bpp), 0)
}

func pickAdapter() (uintptr, error) {
	if procCreateDXGIFactory1.Find() != nil {
		return 0, nil
	}
	var fac uintptr
	hr, _, _ := procCreateDXGIFactory1.Call(uintptr(unsafe.Pointer(&iidDXGIFactory1)), uintptr(unsafe.Pointer(&fac)))
	if failed(hr) || fac == 0 {
		return 0, nil
	}
	defer releaseCOM(&fac)

	type cand struct {
		ptr    uintptr
		vendor uint32
	}
	var hw []cand
	for i := uintptr(0); ; i++ {
		var adp uintptr
		hr := comCall(fac, 7, i, uintptr(unsafe.Pointer(&adp)))
		if failed(hr) || adp == 0 {
			break
		}
		var desc adapterDesc
		if failed(comCall(adp, dxgiAdpGetDesc, uintptr(unsafe.Pointer(&desc)))) {
			releaseCOM(&adp)
			continue
		}
		if desc.VendorId == 0x1414 { // Microsoft Basic Render / WARP
			releaseCOM(&adp)
			continue
		}
		hw = append(hw, cand{adp, desc.VendorId})
	}
	if len(hw) == 0 {
		return 0, nil
	}
	take := func(id uint32) uintptr {
		for i := range hw {
			if hw[i].vendor == id && hw[i].ptr != 0 {
				p := hw[i].ptr
				hw[i].ptr = 0
				return p
			}
		}
		return 0
	}
	drop := func() {
		for i := range hw {
			releaseCOM(&hw[i].ptr)
		}
	}
	if p := take(0x8086); p != 0 { // Intel
		drop()
		return p, nil
	}
	if p := take(0x106B); p != 0 { // Apple (Wine)
		drop()
		return p, nil
	}
	allNV := true
	for i := range hw {
		if hw[i].vendor != 0x10DE {
			allNV = false
			break
		}
	}
	if allNV && hasBattery() {
		drop()
		return 0, fmt.Errorf("laptop dGPU only")
	}
	if p := take(0x1002); p != 0 { // AMD
		drop()
		return p, nil
	}
	p := hw[0].ptr
	hw[0].ptr = 0
	drop()
	return p, nil
}

func hasBattery() bool {
	if procGetSystemPowerStatus.Find() != nil {
		return false
	}
	var st struct {
		ACLineStatus        byte
		BatteryFlag         byte
		BatteryLifePercent  byte
		SystemStatusFlag    byte
		BatteryLifeTime     uint32
		BatteryFullLifeTime uint32
	}
	r, _, _ := procGetSystemPowerStatus.Call(uintptr(unsafe.Pointer(&st)))
	if r == 0 {
		return false
	}
	return st.BatteryFlag != 128
}

func adapterName(dev uintptr) string {
	var dxgiDev uintptr
	if failed(comQI(dev, &iidDXGIDevice, &dxgiDev)) || dxgiDev == 0 {
		return ""
	}
	defer releaseCOM(&dxgiDev)
	var adp uintptr
	hr := comCall(dxgiDev, dxgiDevGetAdapter, uintptr(unsafe.Pointer(&adp)))
	if failed(hr) || adp == 0 {
		return ""
	}
	defer releaseCOM(&adp)
	var desc adapterDesc
	hr = comCall(adp, dxgiAdpGetDesc, uintptr(unsafe.Pointer(&desc)))
	if failed(hr) {
		return ""
	}
	return utf16z(desc.Description[:])
}

func utf16z(s []uint16) string {
	n := 0
	for n < len(s) && s[n] != 0 {
		n++
	}
	if n == 0 {
		return ""
	}
	return string(utf16.Decode(s[:n]))
}

func comCall(obj uintptr, idx int, args ...uintptr) uintptr {
	if obj == 0 {
		return 0x80004003
	}
	vt := *(**[256]uintptr)(unsafe.Pointer(obj))
	all := make([]uintptr, 1+len(args))
	all[0] = obj
	copy(all[1:], args)
	r, _, _ := syscall.SyscallN(vt[idx], all...)
	return r
}

func comQI(obj uintptr, iid *guid, out *uintptr) uintptr {
	*out = 0
	return comCall(obj, 0, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(out)))
}

func releaseCOM(p *uintptr) {
	if p == nil || *p == 0 {
		return
	}
	comCall(*p, iunkRelease)
	*p = 0
}

func failed(hr uintptr) bool { return int32(hr) < 0 }

func hrStr(hr uintptr) string { return fmt.Sprintf("0x%08X", uint32(hr)) }
