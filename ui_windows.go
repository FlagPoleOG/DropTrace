//go:build windows

package main

import (
	"bytes"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

const windowClassName = "DropTraceMainWindow"

var (
	procRegisterClassExW       = user32.NewProc("RegisterClassExW")
	procCreateWindowExW        = user32.NewProc("CreateWindowExW")
	procDefWindowProcW         = user32.NewProc("DefWindowProcW")
	procCallWindowProcW        = user32.NewProc("CallWindowProcW")
	procSetWindowLongPtrW      = user32.NewProc("SetWindowLongPtrW")
	procDestroyWindow          = user32.NewProc("DestroyWindow")
	procGetMessageW            = user32.NewProc("GetMessageW")
	procTranslateMessage       = user32.NewProc("TranslateMessage")
	procDispatchMessageW       = user32.NewProc("DispatchMessageW")
	procIsDialogMessageW       = user32.NewProc("IsDialogMessageW")
	procPostMessageW           = user32.NewProc("PostMessageW")
	procSendMessageW           = user32.NewProc("SendMessageW")
	procPostQuitMessage        = user32.NewProc("PostQuitMessage")
	procRegisterWindowMessageW = user32.NewProc("RegisterWindowMessageW")
	procCreatePopupMenu        = user32.NewProc("CreatePopupMenu")
	procAppendMenuW            = user32.NewProc("AppendMenuW")
	procTrackPopupMenu         = user32.NewProc("TrackPopupMenu")
	procDestroyMenu            = user32.NewProc("DestroyMenu")
	procGetCursorPos           = user32.NewProc("GetCursorPos")
	procSetForegroundWindow    = user32.NewProc("SetForegroundWindow")
	procCreateIconIndirect     = user32.NewProc("CreateIconIndirect")
	procFindWindowW            = user32.NewProc("FindWindowW")
	procShowWindow             = user32.NewProc("ShowWindow")
	procIsIconic               = user32.NewProc("IsIconic")
	procMoveWindow             = user32.NewProc("MoveWindow")
	procSetWindowPos           = user32.NewProc("SetWindowPos")
	procGetClientRect          = user32.NewProc("GetClientRect")
	procInvalidateRect         = user32.NewProc("InvalidateRect")
	procBeginPaint             = user32.NewProc("BeginPaint")
	procEndPaint               = user32.NewProc("EndPaint")
	procFillRect               = user32.NewProc("FillRect")
	procDrawTextW              = user32.NewProc("DrawTextW")
	procSetTimer               = user32.NewProc("SetTimer")
	procKillTimer              = user32.NewProc("KillTimer")
	procGetDpiForWindow        = user32.NewProc("GetDpiForWindow")
	procGetDpiForSystem        = user32.NewProc("GetDpiForSystem")
	procLoadCursorW            = user32.NewProc("LoadCursorW")
	procTrackMouseEvent        = user32.NewProc("TrackMouseEvent")
	procGetWindowTextW         = user32.NewProc("GetWindowTextW")
	procGetWindowTextLengthW   = user32.NewProc("GetWindowTextLengthW")
	procSetWindowTextW         = user32.NewProc("SetWindowTextW")
	procSystemParametersInfoW  = user32.NewProc("SystemParametersInfoW")
	procSendMessageTimeoutW    = user32.NewProc("SendMessageTimeoutW")

	procExtractIconExW   = shell32.NewProc("ExtractIconExW")
	procShellNotifyIconW = shell32.NewProc("Shell_NotifyIconW")

	procCreateBitmap           = gdi32.NewProc("CreateBitmap")
	procCreateCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject           = gdi32.NewProc("SelectObject")
	procDeleteObject           = gdi32.NewProc("DeleteObject")
	procDeleteDC               = gdi32.NewProc("DeleteDC")
	procBitBlt                 = gdi32.NewProc("BitBlt")
	procCreateSolidBrush       = gdi32.NewProc("CreateSolidBrush")
	procCreateHatchBrush       = gdi32.NewProc("CreateHatchBrush")
	procSetTextColor           = gdi32.NewProc("SetTextColor")
	procSetBkColor             = gdi32.NewProc("SetBkColor")
	procSetBkMode              = gdi32.NewProc("SetBkMode")
	procCreateFontW            = gdi32.NewProc("CreateFontW")

	procGdiplusStartup      = gdiplus.NewProc("GdiplusStartup")
	procGdipCreateFromHDC   = gdiplus.NewProc("GdipCreateFromHDC")
	procGdipDeleteGraphics  = gdiplus.NewProc("GdipDeleteGraphics")
	procGdipSetSmoothing    = gdiplus.NewProc("GdipSetSmoothingMode")
	procGdipCreateSolidFill = gdiplus.NewProc("GdipCreateSolidFill")
	procGdipDeleteBrush     = gdiplus.NewProc("GdipDeleteBrush")
	procGdipCreatePen1      = gdiplus.NewProc("GdipCreatePen1")
	procGdipDeletePen       = gdiplus.NewProc("GdipDeletePen")
	procGdipSetPenDashStyle = gdiplus.NewProc("GdipSetPenDashStyle")
	procGdipFillEllipseI    = gdiplus.NewProc("GdipFillEllipseI")
	procGdipDrawEllipseI    = gdiplus.NewProc("GdipDrawEllipseI")
	procGdipDrawLineI       = gdiplus.NewProc("GdipDrawLineI")
	procGdipDrawLinesI      = gdiplus.NewProc("GdipDrawLinesI")
	procGdipFillRectangleI  = gdiplus.NewProc("GdipFillRectangleI")

	procInitCommonControlsEx  = comctl32.NewProc("InitCommonControlsEx")
	procGetSaveFileNameW      = comdlg32.NewProc("GetSaveFileNameW")
	procSetWindowTheme        = uxtheme.NewProc("SetWindowTheme")
	procDwmSetWindowAttribute = dwmapi.NewProc("DwmSetWindowAttribute")
	procDwmFlush              = dwmapi.NewProc("DwmFlush")
	procUpdateWindow          = user32.NewProc("UpdateWindow")
)

const (
	wmSize          = 0x0005
	wmDestroy       = 0x0002
	wmPaint         = 0x000F
	wmClose         = 0x0010
	wmEraseBkgnd    = 0x0014
	wmEndSession    = 0x0016
	wmSettingChange = 0x001A
	wmSetFont       = 0x0030
	wmGetMinMaxInfo = 0x0024
	wmDrawItem      = 0x002B
	wmNotify        = 0x004E
	wmCommand       = 0x0111
	wmTimer         = 0x0113
	wmCtlColorEdit  = 0x0133
	wmMouseMove     = 0x0200
	wmLButtonDown   = 0x0201
	wmLButtonUp     = 0x0202
	wmLButtonDbl    = 0x0203
	wmRButtonUp     = 0x0205
	wmMouseLeave    = 0x02A3
	wmDpiChanged    = 0x02E0
	wmNull          = 0x0000
	wmApp           = 0x8000
	wmTray          = wmApp + 1
	wmCheckDone     = wmApp + 2
	wmShowApp       = wmApp + 3
	wmAnimFrame     = wmApp + 4

	nimAdd, nimModify, nimDelete = 0, 1, 2
	nifMessage, nifIcon, nifTip  = 0x1, 0x2, 0x4
	nifInfo, niifInfo            = 0x10, 0x1

	wsChild, wsVisible, wsTabStop = 0x40000000, 0x10000000, 0x00010000
	wsBorder, wsClipChildren      = 0x00800000, 0x02000000
	wsOverlappedWindow            = 0x00CF0000
	wsExControlParent             = 0x00010000
	cwUseDefault                  = 0x80000000
	bsOwnerDraw                   = 0xB
	esAutoHScroll                 = 0x80
	swHide, swShow, swRestore     = 0, 5, 9
	sizeMinimized                 = 1

	dtLeft, dtCenter, dtRight = 0, 1, 2
	dtVCenter, dtSingleLine   = 4, 0x20
	dtWordBreak, dtCalcRect   = 0x10, 0x400
	dtNoPrefix, dtEndEllipsis = 0x800, 0x8000

	odsSelected, odsFocus = 0x1, 0x10
	odsNoFocusRect        = 0x200
	tmeLeave              = 0x2

	gwlpWndProc = ^uintptr(3) // -4

	lvsReport, lvsSingleSel, lvsNoSort = 0x1, 0x4, 0x8000
	lvmSetBkColor                      = 0x1001
	lvmDeleteAllItems                  = 0x1009
	lvmSetColumnWidth                  = 0x101E
	lvmGetHeader                       = 0x101F
	lvmSetTextColor                    = 0x1024
	lvmSetTextBkColor                  = 0x1026
	lvmSetExtStyle                     = 0x1036
	lvmInsertItemW                     = 0x104D
	lvmInsertColumnW                   = 0x1061
	lvmSetItemTextW                    = 0x1074

	timerAnim, timerDelete = 1, 2
)

const (
	viewLive = iota
	viewHistory
	viewReport
	viewSettings
)

const (
	kindNav = iota
	kindSeg
	kindPrimary
	kindSecondary
	kindDanger
	kindCheck
)

const (
	idNavBase = 100 // + view

	idHist7, idHist30, idHist90            = 201, 202, 203
	idRep7, idRep30, idRep90, idRep365     = 211, 212, 213, 214
	idRepAll, idSavePDF, idSaveCSV         = 220, 221, 222
	idDonateReport                         = 223
	idAuto, idNotify, idDelete, idExit     = 301, 302, 303, 304
	idDonateSettings                       = 305
	idStartupSettings                      = 306
	idEditName, idEditProvider, idEditAcct = 401, 402, 403
	idList                                 = 501
	menuOpen, menuExit                     = 1, 2
)

type winRect struct{ l, t, r, b int32 }

func (r winRect) w() int32 { return r.r - r.l }
func (r winRect) h() int32 { return r.b - r.t }
func (r winRect) has(x, y int32) bool {
	return x >= r.l && x < r.r && y >= r.t && y < r.b
}
func (r winRect) overlaps(o winRect) bool {
	return r.l < o.r && o.l < r.r && r.t < o.b && o.t < r.b
}

type point struct{ x, y int32 }

type paintStruct struct {
	hdc      uintptr
	fErase   int32
	rc       winRect
	fRest    int32
	fUpdate  int32
	reserved [32]byte
}

type notifyIconData struct {
	cbSize           uint32
	hWnd             uintptr
	uID              uint32
	uFlags           uint32
	uCallbackMessage uint32
	hIcon            uintptr
	szTip            [128]uint16
	dwState          uint32
	dwStateMask      uint32
	szInfo           [256]uint16
	uVersion         uint32
	szInfoTitle      [64]uint16
	dwInfoFlags      uint32
	guidItem         [16]byte
	hBalloonIcon     uintptr
}

type wndClassEx struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

type winMsg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      point
	private uint32
}

type iconInfo struct {
	fIcon    int32
	xHotspot uint32
	yHotspot uint32
	hbmMask  uintptr
	hbmColor uintptr
}

type drawItemStruct struct {
	ctlType, ctlID, itemID, itemAction, itemState uint32
	hwndItem, hDC                                 uintptr
	rc                                            winRect
	itemData                                      uintptr
}

type lvColumn struct {
	mask             uint32
	fmt              int32
	cx               int32
	pszText          *uint16
	cchTextMax       int32
	iSubItem         int32
	iImage, iOrder   int32
	cxMin, cxDefault int32
	cxIdeal          int32
}

type lvItem struct {
	mask             uint32
	iItem, iSubItem  int32
	state, stateMask uint32
	pszText          *uint16
	cchTextMax       int32
	iImage           int32
	lParam           uintptr
	iIndent          int32
	iGroupId         int32
	cColumns         uint32
	puColumns        uintptr
	piColFmt         uintptr
	iGroup           int32
}

type openFileName struct {
	lStructSize       uint32
	hwndOwner         uintptr
	hInstance         uintptr
	lpstrFilter       *uint16
	lpstrCustomFilter *uint16
	nMaxCustFilter    uint32
	nFilterIndex      uint32
	lpstrFile         *uint16
	nMaxFile          uint32
	lpstrFileTitle    *uint16
	nMaxFileTitle     uint32
	lpstrInitialDir   *uint16
	lpstrTitle        *uint16
	Flags             uint32
	nFileOffset       uint16
	nFileExtension    uint16
	lpstrDefExt       *uint16
	lCustData         uintptr
	lpfnHook          uintptr
	lpTemplateName    *uint16
	pvReserved        uintptr
	dwReserved        uint32
	FlagsEx           uint32
}

type trackMouse struct {
	cbSize, dwFlags uint32
	hwnd            uintptr
	hover           uint32
}

type initCommon struct{ size, icc uint32 }

type gdipInput struct {
	version  uint32
	callback uintptr
	noThread int32
	noCodecs int32
}

// These fail to compile if a Win32 structure doesn't have its documented 64-bit size.
const (
	_ = uint(976 - unsafe.Sizeof(notifyIconData{}))
	_ = uint(unsafe.Sizeof(notifyIconData{}) - 976)
	_ = uint(80 - unsafe.Sizeof(wndClassEx{}))
	_ = uint(unsafe.Sizeof(wndClassEx{}) - 80)
	_ = uint(48 - unsafe.Sizeof(winMsg{}))
	_ = uint(unsafe.Sizeof(winMsg{}) - 48)
	_ = uint(32 - unsafe.Sizeof(iconInfo{}))
	_ = uint(unsafe.Sizeof(iconInfo{}) - 32)
	_ = uint(64 - unsafe.Sizeof(drawItemStruct{}))
	_ = uint(unsafe.Sizeof(drawItemStruct{}) - 64)
	_ = uint(56 - unsafe.Sizeof(lvColumn{}))
	_ = uint(unsafe.Sizeof(lvColumn{}) - 56)
	_ = uint(88 - unsafe.Sizeof(lvItem{}))
	_ = uint(unsafe.Sizeof(lvItem{}) - 88)
	_ = uint(152 - unsafe.Sizeof(openFileName{}))
	_ = uint(unsafe.Sizeof(openFileName{}) - 152)
	_ = uint(72 - unsafe.Sizeof(paintStruct{}))
	_ = uint(unsafe.Sizeof(paintStruct{}) - 72)
)

func ptr(u uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&u)) }
func wstr(s string) *uint16 {
	p, err := syscall.UTF16PtrFromString(strings.ReplaceAll(s, "\x00", ""))
	if err != nil {
		p, _ = syscall.UTF16PtrFromString("")
	}
	return p
}

// ---- Colors ----

type palette struct {
	bg, panel, ink, muted, line, track, accent, accentInk, up, down, unk uint32
	dark                                                                 bool
}

var lightTheme = palette{bg: 0xF0F3F5, panel: 0xFFFFFF, ink: 0x14222C, muted: 0x51616D, line: 0xC8D2D9, track: 0xDAE2E7,
	accent: 0x17506E, accentInk: 0xFFFFFF, up: 0x17744A, down: 0xC02E25, unk: 0x6B7A84}
var darkTheme = palette{bg: 0x172229, panel: 0x1F2D35, ink: 0xE9EFF2, muted: 0xA3B2BC, line: 0x3A4B56, track: 0x2E3E48,
	accent: 0x7CC0E3, accentInk: 0x0F222C, up: 0x54C991, down: 0xFF8378, unk: 0x9AA9B3, dark: true}

func colorRef(c uint32) uintptr {
	return uintptr((c>>16)&0xFF | c&0xFF00 | (c&0xFF)<<16)
}
func argb(c uint32, a uint32) uintptr { return uintptr(a<<24 | c&0xFFFFFF) }

func systemUsesDark() bool {
	var k syscall.Handle
	if syscall.RegOpenKeyEx(syscall.HKEY_CURRENT_USER, wstr(`Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`), 0, keyQuery, &k) != nil {
		return false
	}
	defer syscall.RegCloseKey(k)
	var v, size uint32 = 1, 4
	if syscall.RegQueryValueEx(k, wstr("AppsUseLightTheme"), nil, nil, (*byte)(unsafe.Pointer(&v)), &size) != nil {
		return false
	}
	return v == 0
}

// ---- Window state ----

type control struct {
	hwnd   uintptr
	id     int
	kind   int
	text   string
	view   int // -1 means every view
	on     bool
	hover  bool
	orig   uintptr
	hidden bool
}

type label struct {
	view  int
	text  string
	font  uintptr
	color *uint32
	r     winRect
	flags uintptr
}

type markRect struct {
	r    winRect
	text string
}

type winUI struct {
	store *Store
	mon   *Monitor

	startVisible bool
	visible      atomic.Bool
	hwnd         uintptr
	instance     uintptr
	dpi          int32
	width        int32
	height       int32
	view         int
	theme        palette
	motion       bool

	taskbarCreated uint32
	icons          map[string]uintptr
	ready          chan struct{}
	timerOn        bool
	tick           int
	closeTipShown  bool

	memDC, memBmp, memOld uintptr
	memW, memH            int32
	pathDC, pathBmp       uintptr
	pathOld               uintptr
	pathKey               string
	animUntil             atomic.Int64
	animRunning           atomic.Bool
	framePending          atomic.Bool
	frameCount            int
	frameWindow           time.Time
	fps                   int
	bgBrush, editBrush    uintptr

	fHero, fH2, fBody, fBold, fSmall, fFig, fTitle uintptr

	controls map[int]*control
	byHwnd   map[uintptr]*control
	labels   []label
	list     uintptr
	listOrig uintptr
	header   uintptr
	edits    map[int]uintptr

	rcHeader, rcHero, rcPath, rcGraph, rcFoot, rcContent, rcStrips winRect
	heroHead                                                       string

	// Set by the check goroutine, read on the window thread.
	mu         sync.Mutex
	pendStatus Status
	pendPoints []CheckPoint
	pendAt     time.Time
	pendTitle  string
	pendBody   string

	status    Status
	points    []CheckPoint
	lastCheck time.Time
	trayState string
	trayTip   string
	lastKey   string

	histDays int
	stats    Stats
	outages  []Outage
	weekOut  []Outage
	coverage map[string]float64
	marks    []markRect
	repDays  int
	repAll   bool
	repMsg   string
	repErr   bool
	setMsg   string
	armed    bool
	mouse    point
	mouseIn  bool
	tracking bool
}

var theUI *winUI

func newPlatformUI(store *Store, mon *Monitor, startVisible bool) PlatformUI {
	u := &winUI{store: store, mon: mon, startVisible: startVisible, icons: map[string]uintptr{}, ready: make(chan struct{}),
		controls: map[int]*control{}, byHwnd: map[uintptr]*control{}, edits: map[int]uintptr{},
		histDays: 30, repDays: 30, motion: true, view: viewLive}
	u.status = mon.Status()
	return u
}

func (u *winUI) S(v int32) int32 { return v * u.dpi / 96 }

func (u *winUI) WindowVisible() bool { return u.visible.Load() }

// Update hands the latest check to the window thread.
func (u *winUI) Update(st Status, pts []CheckPoint) {
	u.mu.Lock()
	u.pendStatus, u.pendAt = st, time.Now()
	u.pendPoints = append(u.pendPoints[:0], pts...)
	u.mu.Unlock()
	u.post()
}

// Notify shows a tray notification from any thread.
func (u *winUI) Notify(title, body string) {
	u.mu.Lock()
	u.pendTitle, u.pendBody = title, body
	u.mu.Unlock()
	u.post()
}

func (u *winUI) post() {
	<-u.ready
	_, _, _ = procPostMessageW.Call(u.hwnd, wmCheckDone, 0, 0)
}

// ---- Startup ----

func (u *winUI) Run() {
	runtime.LockOSThread()
	theUI = u

	var token uintptr
	in := gdipInput{version: 1}
	if r, _, _ := procGdiplusStartup.Call(uintptr(unsafe.Pointer(&token)), uintptr(unsafe.Pointer(&in)), 0); r != 0 {
		fatal(fmt.Sprintf("DropTrace couldn't start Windows graphics (GDI+ error %d).", r))
	}
	icc := initCommon{size: 8, icc: 0x1 | 0x4000} // list views and standard controls
	_, _, _ = procInitCommonControlsEx.Call(uintptr(unsafe.Pointer(&icc)))

	u.instance, _, _ = procGetModuleHandleW.Call(0)
	u.dpi = 96
	if procGetDpiForSystem.Find() == nil {
		if d, _, _ := procGetDpiForSystem.Call(); d != 0 {
			u.dpi = int32(d)
		}
	}
	u.loadTheme()

	var large, small uintptr
	if exe, err := os.Executable(); err == nil {
		_, _, _ = procExtractIconExW.Call(uintptr(unsafe.Pointer(wstr(exe))), 0,
			uintptr(unsafe.Pointer(&large)), uintptr(unsafe.Pointer(&small)), 1)
	}
	cursor, _, _ := procLoadCursorW.Call(0, 32512)
	wc := wndClassEx{style: 0x3, lpfnWndProc: syscall.NewCallback(windowProc), hInstance: u.instance,
		lpszClassName: wstr(windowClassName), hIcon: large, hIconSm: small, hCursor: cursor}
	wc.cbSize = uint32(unsafe.Sizeof(wc))
	if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		fatal("DropTrace couldn't create its window: " + err.Error())
	}
	tb, _, _ := procRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(wstr("TaskbarCreated"))))
	u.taskbarCreated = uint32(tb)

	hwnd, _, err := procCreateWindowExW.Call(wsExControlParent, uintptr(unsafe.Pointer(wstr(windowClassName))),
		uintptr(unsafe.Pointer(wstr("DropTrace"))), wsOverlappedWindow|wsClipChildren,
		cwUseDefault, cwUseDefault, uintptr(u.S(880)), uintptr(u.S(700)), 0, 0, u.instance, 0)
	if hwnd == 0 {
		fatal("DropTrace couldn't create its window: " + err.Error())
	}
	u.hwnd = hwnd
	if procGetDpiForWindow.Find() == nil {
		if d, _, _ := procGetDpiForWindow.Call(hwnd); d != 0 {
			u.dpi = int32(d)
		}
	}
	u.applyTitleBar()
	u.makeFonts()
	u.createControls()
	u.applyListTheme()
	u.icons["online"] = makeIcon(0x24, 0x9A, 0x5E)
	u.icons["checking"] = makeIcon(0xE0, 0x9A, 0x12)
	u.icons["starting"] = u.icons["checking"]
	u.icons["outage"] = makeIcon(0xD6, 0x3A, 0x30)
	u.trayState, u.trayTip = "starting", "DropTrace: Running the first check."
	u.addTrayIcon()
	u.syncSettings()
	u.setView(viewLive)
	u.layout()
	setShutdownHook(u.store)
	close(u.ready)
	if u.startVisible {
		u.showApp()
	}

	var msg winMsg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		if d, _, _ := procIsDialogMessageW.Call(u.hwnd, uintptr(unsafe.Pointer(&msg))); d != 0 {
			continue
		}
		_, _, _ = procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		_, _, _ = procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

var endSessionStore *Store

func setShutdownHook(s *Store) { endSessionStore = s }

func (u *winUI) loadTheme() {
	if systemUsesDark() {
		u.theme = darkTheme
	} else {
		u.theme = lightTheme
	}
	if u.bgBrush != 0 {
		_, _, _ = procDeleteObject.Call(u.bgBrush)
		_, _, _ = procDeleteObject.Call(u.editBrush)
	}
	u.bgBrush, _, _ = procCreateSolidBrush.Call(colorRef(u.theme.bg))
	u.editBrush, _, _ = procCreateSolidBrush.Call(colorRef(u.theme.panel))
	var on uint32 = 1
	u.motion = true
	if r, _, _ := procSystemParametersInfoW.Call(0x1042, 0, uintptr(unsafe.Pointer(&on)), 0); r != 0 {
		u.motion = on != 0 // Windows' "Animation effects" setting
	}
}

func (u *winUI) applyTitleBar() {
	var dark uint32
	if u.theme.dark {
		dark = 1
	}
	if procDwmSetWindowAttribute.Find() == nil {
		_, _, _ = procDwmSetWindowAttribute.Call(u.hwnd, 20, uintptr(unsafe.Pointer(&dark)), 4)
	}
}

func (u *winUI) applyListTheme() {
	name := "Explorer"
	if u.theme.dark {
		name = "DarkMode_Explorer"
	}
	if procSetWindowTheme.Find() == nil && u.list != 0 {
		_, _, _ = procSetWindowTheme.Call(u.list, uintptr(unsafe.Pointer(wstr(name))), 0)
	}
	t := u.theme
	send(u.list, lvmSetBkColor, 0, colorRef(t.bg))
	send(u.list, lvmSetTextBkColor, 0, colorRef(t.bg))
	send(u.list, lvmSetTextColor, 0, colorRef(t.ink))
}

func send(h uintptr, msg uint32, w, l uintptr) uintptr {
	r, _, _ := procSendMessageW.Call(h, uintptr(msg), w, l)
	return r
}

func (u *winUI) makeFont(face string, size, weight int32) uintptr {
	h, _, _ := procCreateFontW.Call(uintptr(int64(-int64(u.S(size)))), 0, 0, 0, uintptr(weight), 0, 0, 0, 1, 0, 0, 5, 0,
		uintptr(unsafe.Pointer(wstr(face))))
	return h
}

func (u *winUI) makeFonts() {
	for _, f := range []uintptr{u.fHero, u.fH2, u.fBody, u.fBold, u.fSmall, u.fFig, u.fTitle} {
		if f != 0 {
			_, _, _ = procDeleteObject.Call(f)
		}
	}
	u.fHero = u.makeFont("Bahnschrift SemiBold", 36, 600)
	u.fH2 = u.makeFont("Bahnschrift SemiBold", 20, 600)
	u.fTitle = u.makeFont("Bahnschrift SemiBold", 18, 600)
	u.fFig = u.makeFont("Bahnschrift SemiBold", 26, 600)
	u.fBody = u.makeFont("Segoe UI", 15, 400)
	u.fBold = u.makeFont("Segoe UI Semibold", 15, 600)
	u.fSmall = u.makeFont("Segoe UI", 13, 400)
	for _, c := range u.controls {
		send(c.hwnd, wmSetFont, u.fBody, 1)
	}
	for _, e := range u.edits {
		send(e, wmSetFont, u.fBody, 1)
	}
	if u.list != 0 {
		send(u.list, wmSetFont, u.fBody, 1)
	}
}

// ---- Controls ----

var buttonProc = syscall.NewCallback(func(hwnd, msg, wParam, lParam uintptr) uintptr {
	u := theUI
	c := u.byHwnd[hwnd]
	if c == nil {
		r, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
		return r
	}
	switch uint32(msg) {
	case wmMouseMove:
		if !c.hover {
			c.hover = true
			tm := trackMouse{dwFlags: tmeLeave, hwnd: hwnd}
			tm.cbSize = uint32(unsafe.Sizeof(tm))
			_, _, _ = procTrackMouseEvent.Call(uintptr(unsafe.Pointer(&tm)))
			_, _, _ = procInvalidateRect.Call(hwnd, 0, 0)
		}
	case wmMouseLeave:
		c.hover = false
		_, _, _ = procInvalidateRect.Call(hwnd, 0, 0)
	case wmEraseBkgnd:
		return 1
	case wmLButtonDbl: // treat fast repeat clicks as clicks
		msg = wmLButtonDown
	}
	r, _, _ := procCallWindowProcW.Call(c.orig, hwnd, msg, wParam, lParam)
	return r
})

var listProc = syscall.NewCallback(func(hwnd, msg, wParam, lParam uintptr) uintptr {
	u := theUI
	if uint32(msg) == wmNotify {
		from := *(*uintptr)(ptr(lParam))
		code := *(*uint32)(ptr(lParam + 16))
		if from == u.header && code == 0xFFFFFFF4 { // NM_CUSTOMDRAW
			stage := *(*uint32)(ptr(lParam + 24))
			switch stage {
			case 1:
				return 0x20 // notify for each item
			case 0x10001:
				hdc := *(*uintptr)(ptr(lParam + 32))
				rc := *(*winRect)(ptr(lParam + 40))
				item := *(*uintptr)(ptr(lParam + 56))
				u.fill(hdc, rc, u.theme.bg)
				titles := []string{"Started", "Length", "Likely cause"}
				if int(item) < len(titles) {
					text := rc
					text.l += u.S(6)
					text.b--
					u.text(hdc, titles[item], u.fBold, u.theme.ink, text, dtLeft|dtVCenter|dtSingleLine)
				}
				line := rc
				line.t = rc.b - u.S(1)
				if line.t < rc.t {
					line.t = rc.t
				}
				u.fill(hdc, line, u.theme.ink)
				return 4 // skip the default drawing
			}
		}
	}
	r, _, _ := procCallWindowProcW.Call(u.listOrig, hwnd, msg, wParam, lParam)
	return r
})

func (u *winUI) addButton(id, kind, view int, text string) *control {
	h, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(wstr("BUTTON"))), uintptr(unsafe.Pointer(wstr(text))),
		wsChild|wsVisible|wsTabStop|bsOwnerDraw, 0, 0, 10, 10, u.hwnd, uintptr(id), u.instance, 0)
	c := &control{hwnd: h, id: id, kind: kind, text: text, view: view}
	u.controls[id] = c
	u.byHwnd[h] = c
	c.orig, _, _ = procSetWindowLongPtrW.Call(h, gwlpWndProc, buttonProc)
	send(h, wmSetFont, u.fBody, 1)
	return c
}

func (u *winUI) addEdit(id int) {
	h, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(wstr("EDIT"))), uintptr(unsafe.Pointer(wstr(""))),
		wsChild|wsTabStop|wsBorder|esAutoHScroll, 0, 0, 10, 10, u.hwnd, uintptr(id), u.instance, 0)
	send(h, wmSetFont, u.fBody, 1)
	u.edits[id] = h
}

func (u *winUI) createControls() {
	for i, name := range []string{"Live", "History", "Report", "Settings"} {
		u.addButton(idNavBase+i, kindNav, -1, name)
	}
	u.addButton(idHist7, kindSeg, viewHistory, "7 days")
	u.addButton(idHist30, kindSeg, viewHistory, "30 days").on = true
	u.addButton(idHist90, kindSeg, viewHistory, "90 days")
	u.addButton(idRep7, kindSeg, viewReport, "7 days")
	u.addButton(idRep30, kindSeg, viewReport, "30 days").on = true
	u.addButton(idRep90, kindSeg, viewReport, "90 days")
	u.addButton(idRep365, kindSeg, viewReport, "1 year")
	u.addButton(idRepAll, kindCheck, viewReport, "Include all outages, not only provider-side ones")
	u.addButton(idSavePDF, kindPrimary, viewReport, "Save PDF report")
	u.addButton(idSaveCSV, kindSecondary, viewReport, "Save CSV")
	if isPackaged() {
		u.addButton(idStartupSettings, kindSecondary, viewSettings, "Manage startup in Windows Settings")
	} else {
		u.addButton(idAuto, kindCheck, viewSettings, "Start DropTrace with Windows")
	}
	u.addButton(idNotify, kindCheck, viewSettings, "Notify me when an outage ends")
	u.addButton(idDelete, kindDanger, viewSettings, "Delete history")
	u.addButton(idExit, kindSecondary, viewSettings, "Exit DropTrace")
	if strings.HasPrefix(DonateURL, "https://") {
		u.addButton(idDonateReport, kindSecondary, viewReport, "Support on Ko-fi")
		u.addButton(idDonateSettings, kindSecondary, viewSettings, "Support on Ko-fi")
	}
	u.addEdit(idEditName)
	u.addEdit(idEditProvider)
	u.addEdit(idEditAcct)

	h, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(wstr("SysListView32"))), 0,
		wsChild|wsTabStop|lvsReport|lvsSingleSel|lvsNoSort, 0, 0, 10, 10, u.hwnd, idList, u.instance, 0)
	u.list = h
	send(h, lvmSetExtStyle, 0, 0x20|0x10000) // full-row select, double buffering
	send(h, wmSetFont, u.fBody, 1)
	for i, t := range []string{"Started", "Length", "Likely cause"} {
		col := lvColumn{mask: 0x2 | 0x4 | 0x8, cx: 200, pszText: wstr(t), iSubItem: int32(i)}
		send(h, lvmInsertColumnW, uintptr(i), uintptr(unsafe.Pointer(&col)))
	}
	u.header = send(h, lvmGetHeader, 0, 0)
	u.listOrig, _, _ = procSetWindowLongPtrW.Call(h, gwlpWndProc, listProc)
}

func (u *winUI) syncSettings() {
	if c := u.controls[idAuto]; c != nil {
		c.on = autostartEnabled()
	}
	u.controls[idNotify].on = u.store.NotifyEnabled()
}

func (u *winUI) setView(v int) {
	u.view = v
	for _, c := range u.controls {
		show := uintptr(swHide)
		if c.view == -1 || c.view == v {
			show = swShow
		}
		_, _, _ = procShowWindow.Call(c.hwnd, show)
		if c.kind == kindNav {
			c.on = c.id == idNavBase+v
			_, _, _ = procInvalidateRect.Call(c.hwnd, 0, 0)
		}
	}
	for _, e := range u.edits {
		show := uintptr(swHide)
		if v == viewReport {
			show = swShow
		}
		_, _, _ = procShowWindow.Call(e, show)
	}
	if v == viewHistory {
		u.refreshHistory()
	} else {
		_, _, _ = procShowWindow.Call(u.list, swHide)
	}
	u.layout()
	u.updateTimer()
	u.invalidate(winRect{0, 0, u.width, u.height})
}

func (u *winUI) move(id int, r winRect) {
	if c := u.controls[id]; c != nil {
		_, _, _ = procMoveWindow.Call(c.hwnd, uintptr(r.l), uintptr(r.t), uintptr(r.w()), uintptr(r.h()), 1)
	}
}

func (u *winUI) layout() {
	var c winRect
	_, _, _ = procGetClientRect.Call(u.hwnd, uintptr(unsafe.Pointer(&c)))
	u.width, u.height = c.r, c.b
	if u.width <= 0 || u.height <= 0 {
		return
	}
	m := u.S(28)
	right := u.width - m
	x := m + u.S(96)
	for i := 0; i < 4; i++ {
		w := u.S(78)
		u.move(idNavBase+i, winRect{x, u.S(10), x + w, u.S(46)})
		x += w
	}
	u.rcHeader = winRect{0, 0, u.width, u.S(52)}
	u.rcContent = winRect{0, u.rcHeader.b, u.width, u.height}
	u.labels = u.labels[:0]

	// Live view: the headline can wrap, so measure it.
	var hr winRect = winRect{m, 0, right, 1000}
	head := u.status.Headline
	if head == "" {
		head = "Running the first check."
	}
	u.measure(head, u.fHero, &hr, dtWordBreak|dtNoPrefix)
	det := winRect{m, 0, right, 1000}
	u.measure(u.status.Detail, u.fBody, &det, dtWordBreak|dtNoPrefix)
	heroH := u.S(14) + hr.h() + u.S(6) + det.h() + u.S(26) + u.S(10)
	u.rcHero = winRect{0, u.rcHeader.b, u.width, u.rcHeader.b + heroH}
	u.rcPath = winRect{0, u.rcHero.b, u.width, u.rcHero.b + u.S(132)}
	u.rcFoot = winRect{0, u.height - u.S(34), u.width, u.height}
	u.rcGraph = winRect{0, u.rcPath.b, u.width, u.rcFoot.t}
	u.heroHead = head

	lb := func(view int, text string, font uintptr, color *uint32, r winRect, flags uintptr) {
		u.labels = append(u.labels, label{view, text, font, color, r, flags})
	}

	// History view.
	y := u.rcHeader.b + u.S(12)
	sx := right - 3*u.S(78)
	for i, id := range []int{idHist7, idHist30, idHist90} {
		u.move(id, winRect{sx + int32(i)*u.S(78), y, sx + int32(i+1)*u.S(78), y + u.S(32)})
	}
	u.rcStrips = winRect{0, y + u.S(46), u.width, y + u.S(46) + u.S(76) + u.S(7*22) + u.S(30)}
	lt := u.rcStrips.b + u.S(4)
	lh := u.height - lt - u.S(16)
	if lh < u.S(60) {
		lh = u.S(60)
	}
	_, _, _ = procMoveWindow.Call(u.list, uintptr(m), uintptr(lt), uintptr(right-m), uintptr(lh), 1)
	cw := (right - m - u.S(24))
	send(u.list, lvmSetColumnWidth, 0, uintptr(cw*42/100))
	send(u.list, lvmSetColumnWidth, 1, uintptr(cw*14/100))
	send(u.list, lvmSetColumnWidth, 2, ^uintptr(1)) // fill the remaining width

	// Report view.
	y = u.rcHeader.b + u.S(14)
	lb(viewReport, "Report for your provider", u.fH2, &u.theme.ink, winRect{m, y, right, y + u.S(30)}, dtSingleLine|dtVCenter)
	y += u.S(32)
	lb(viewReport, "Save a PDF of the outages where your router was working but the internet wasn't.", u.fBody, &u.theme.muted, winRect{m, y, right, y + u.S(44)}, dtWordBreak)
	y += u.S(50)
	fw := (right - m - 2*u.S(12)) / 3
	if fw > u.S(240) {
		fw = u.S(240)
	}
	for i, it := range []struct {
		id   int
		text string
	}{{idEditName, "Your name"}, {idEditProvider, "Internet provider"}, {idEditAcct, "Account number (optional)"}} {
		fx := m + int32(i)*(fw+u.S(12))
		lb(viewReport, it.text, u.fBold, &u.theme.ink, winRect{fx, y, fx + fw, y + u.S(22)}, dtSingleLine|dtVCenter|dtEndEllipsis)
		_, _, _ = procMoveWindow.Call(u.edits[it.id], uintptr(fx), uintptr(y+u.S(24)), uintptr(fw), uintptr(u.S(30)), 1)
	}
	y += u.S(70)
	lb(viewReport, "Period", u.fBold, &u.theme.ink, winRect{m, y, right, y + u.S(22)}, dtSingleLine|dtVCenter)
	y += u.S(24)
	for i, id := range []int{idRep7, idRep30, idRep90, idRep365} {
		u.move(id, winRect{m + int32(i)*u.S(78), y, m + int32(i+1)*u.S(78), y + u.S(32)})
	}
	y += u.S(44)
	u.move(idRepAll, winRect{m, y, right, y + u.S(28)})
	y += u.S(42)
	u.move(idSavePDF, winRect{m, y, m + u.S(146), y + u.S(36)})
	u.move(idSaveCSV, winRect{m + u.S(156), y, m + u.S(256), y + u.S(36)})
	u.move(idDonateReport, winRect{m + u.S(266), y, m + u.S(416), y + u.S(36)})
	y += u.S(48)
	msgColor := &u.theme.muted
	if u.repErr {
		msgColor = &u.theme.down
	}
	lb(viewReport, u.repMsg, u.fBody, msgColor, winRect{m, y, right, y + u.S(70)}, dtWordBreak|dtNoPrefix)

	// Settings view. These controls never move or hide.
	y = u.rcHeader.b + u.S(14)
	lb(viewSettings, "Settings", u.fH2, &u.theme.ink, winRect{m, y, right, y + u.S(30)}, dtSingleLine|dtVCenter)
	y += u.S(40)
	if isPackaged() {
		u.move(idStartupSettings, winRect{m, y, m + u.S(290), y + u.S(36)})
		y += u.S(42)
		lb(viewSettings, "Turn on DropTrace in Windows' Startup apps so it doesn't miss outages after a restart.", u.fSmall, &u.theme.muted, winRect{m, y, right, y + u.S(36)}, dtWordBreak)
	} else {
		u.move(idAuto, winRect{m, y, right, y + u.S(28)})
		y += u.S(28)
		lb(viewSettings, "DropTrace starts quietly in the tray when you sign in, so it doesn't miss outages after a restart.", u.fSmall, &u.theme.muted, winRect{m + u.S(30), y, right, y + u.S(36)}, dtWordBreak)
	}
	y += u.S(42)
	u.move(idNotify, winRect{m, y, right, y + u.S(28)})
	y += u.S(28)
	lb(viewSettings, "Shows how long the outage lasted and its likely cause.", u.fSmall, &u.theme.muted, winRect{m + u.S(30), y, right, y + u.S(20)}, dtWordBreak)
	y += u.S(40)
	u.move(idDelete, winRect{m, y, m + u.S(250), y + u.S(36)})
	u.move(idExit, winRect{m + u.S(260), y, m + u.S(390), y + u.S(36)})
	u.move(idDonateSettings, winRect{m + u.S(400), y, m + u.S(550), y + u.S(36)})
	y += u.S(44)
	if u.setMsg != "" {
		lb(viewSettings, u.setMsg, u.fBody, &u.theme.down, winRect{m, y, right, y + u.S(44)}, dtWordBreak|dtNoPrefix)
		y += u.S(48)
	}
	y += u.S(6)
	lb(viewSettings, "Closing this window keeps DropTrace logging in the tray. Choose Exit DropTrace to stop it.\n\nYour history stays on this PC. DropTrace checks your router, 1.1.1.1, 8.8.8.8 and 9.9.9.9, and looks up three website names, but sends none of your data anywhere.\n\n"+historyLine()+"Version "+Version,
		u.fSmall, &u.theme.muted, winRect{m, y, right, u.height - u.S(10)}, dtWordBreak|dtNoPrefix)
}

func (u *winUI) measure(s string, font uintptr, r *winRect, flags uintptr) {
	if u.memDC == 0 {
		u.ensureBuffer()
	}
	if u.memDC == 0 {
		return
	}
	old, _, _ := procSelectObject.Call(u.memDC, font)
	_, _, _ = procDrawTextW.Call(u.memDC, uintptr(unsafe.Pointer(wstr(s))), ^uintptr(0), uintptr(unsafe.Pointer(r)), flags|dtCalcRect)
	_, _, _ = procSelectObject.Call(u.memDC, old)
}

// ---- Data ----

func (u *winUI) refreshHistory() {
	now := time.Now()
	u.stats = u.store.StatsFor(u.histDays, now)
	u.outages = u.store.Outages(now.Add(-time.Duration(u.histDays) * 24 * time.Hour))
	u.weekOut = u.store.Outages(now.AddDate(0, 0, -7))
	u.coverage = u.store.DayCoverage(now.AddDate(0, 0, -7))
	send(u.list, lvmDeleteAllItems, 0, 0)
	for i, o := range u.outages {
		length := "Still down"
		if o.End != nil {
			length = formatDuration(o.Duration(now))
		}
		cause := CauseInfoFor(o.Cause).Label
		if o.Note != "" {
			cause += " (end time approximate)"
		}
		start := wstr(o.Start.Local().Format("Mon, Jan 2, 3:04:05 PM"))
		it := lvItem{mask: 1, iItem: int32(i), pszText: start}
		send(u.list, lvmInsertItemW, 0, uintptr(unsafe.Pointer(&it)))
		for col, text := range []string{length, cause} {
			sub := lvItem{mask: 1, iSubItem: int32(col + 1), pszText: wstr(text)}
			send(u.list, lvmSetItemTextW, uintptr(i), uintptr(unsafe.Pointer(&sub)))
		}
		runtime.KeepAlive(start)
	}
	show := uintptr(swHide)
	if u.view == viewHistory && len(u.outages) > 0 {
		show = swShow
	}
	_, _, _ = procShowWindow.Call(u.list, show)
}

func (u *winUI) takeUpdate() {
	u.mu.Lock()
	had := !u.pendAt.IsZero()
	if had {
		u.status, u.lastCheck = u.pendStatus, u.pendAt
		u.points = append(u.points[:0], u.pendPoints...)
		u.pendAt = time.Time{}
	}
	title, body := u.pendTitle, u.pendBody
	u.pendTitle, u.pendBody = "", ""
	u.mu.Unlock()

	if title != "" {
		u.balloon(title, body)
	}
	if !had {
		return
	}
	if u.status.Headline != u.heroHead {
		u.layout()
		u.invalidate(u.rcContent)
	}
	state := u.status.State
	tip := "DropTrace: " + u.status.Headline
	if len(tip) > 120 {
		tip = tip[:120]
	}
	if state != u.trayState || tip != u.trayTip {
		u.trayState, u.trayTip = state, tip
		u.notifyIcon(nimModify, nifIcon|nifTip, func(n *notifyIconData) {
			n.hIcon = u.iconFor(state)
			copyUTF16(n.szTip[:], tip)
		})
	}
	id := int64(0)
	if u.status.Outage != nil {
		id = u.status.Outage.ID
	}
	key := fmt.Sprintf("%s/%d", state, id)
	if key != u.lastKey {
		u.lastKey = key
		if u.view == viewHistory {
			u.refreshHistory()
			u.invalidate(u.rcContent)
		}
	}
	if u.WindowVisible() {
		if u.view == viewLive {
			u.invalidate(u.rcHero)
			u.invalidate(u.rcPath)
			u.invalidate(u.rcGraph)
			u.invalidate(u.rcFoot)
			if u.motion {
				u.animUntil.Store(time.Now().Add(time.Duration(animSeconds * float64(time.Second))).UnixNano())
				u.runAnimation()
			}
		}
		u.invalidate(u.rcHeader)
	}
}

func (u *winUI) invalidate(r winRect) {
	_, _, _ = procInvalidateRect.Call(u.hwnd, uintptr(unsafe.Pointer(&r)), 0)
}

func (u *winUI) updateTimer() {
	want := u.WindowVisible() && u.view == viewLive && u.motion
	if want && !u.timerOn {
		_, _, _ = procSetTimer.Call(u.hwnd, timerAnim, 100, 0)
	} else if !want && u.timerOn {
		_, _, _ = procKillTimer.Call(u.hwnd, timerAnim)
	}
	u.timerOn = want
}

func (u *winUI) onTick() {
	u.tick++
	u.invalidate(u.rcGraph) // the graph scrolls about 10 times a second
	if u.tick%10 == 0 && u.status.Outage != nil {
		u.invalidate(u.rcHero)
	}
}

// runAnimation draws the dot in step with the display, and only while it's moving.
func (u *winUI) runAnimation() {
	if !u.animRunning.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer u.animRunning.Store(false)
		for time.Now().UnixNano() < u.animUntil.Load() && u.WindowVisible() {
			start := time.Now()
			flushed := false
			if procDwmFlush.Find() == nil {
				r, _, _ := procDwmFlush.Call()
				flushed = r == 0
			}
			if elapsed := time.Since(start); !flushed || elapsed < 6*time.Millisecond {
				wait := 6*time.Millisecond - elapsed
				if !flushed {
					wait = 16*time.Millisecond - elapsed
				}
				if wait > 0 {
					time.Sleep(wait)
				}
			}
			if u.framePending.CompareAndSwap(false, true) {
				_, _, _ = procPostMessageW.Call(u.hwnd, wmAnimFrame, 0, 0)
			}
		}
	}()
}

// ---- Drawing helpers ----

func (u *winUI) fill(dc uintptr, r winRect, c uint32) {
	b, _, _ := procCreateSolidBrush.Call(colorRef(c))
	_, _, _ = procFillRect.Call(dc, uintptr(unsafe.Pointer(&r)), b)
	_, _, _ = procDeleteObject.Call(b)
}

func (u *winUI) text(dc uintptr, s string, font uintptr, color uint32, r winRect, flags uintptr) {
	old, _, _ := procSelectObject.Call(dc, font)
	_, _, _ = procSetTextColor.Call(dc, colorRef(color))
	_, _, _ = procSetBkMode.Call(dc, 1)
	_, _, _ = procDrawTextW.Call(dc, uintptr(unsafe.Pointer(wstr(s))), ^uintptr(0), uintptr(unsafe.Pointer(&r)), flags|dtNoPrefix)
	_, _, _ = procSelectObject.Call(dc, old)
}

type gfx uintptr

func newGfx(dc uintptr) gfx {
	var g uintptr
	_, _, _ = procGdipCreateFromHDC.Call(dc, uintptr(unsafe.Pointer(&g)))
	_, _, _ = procGdipSetSmoothing.Call(g, 4)
	return gfx(g)
}
func (g gfx) close() { _, _, _ = procGdipDeleteGraphics.Call(uintptr(g)) }

func (g gfx) circle(c uintptr, cx, cy, r int32) {
	var b uintptr
	_, _, _ = procGdipCreateSolidFill.Call(c, uintptr(unsafe.Pointer(&b)))
	_, _, _ = procGdipFillEllipseI.Call(uintptr(g), b, uintptr(cx-r), uintptr(cy-r), uintptr(2*r), uintptr(2*r))
	_, _, _ = procGdipDeleteBrush.Call(b)
}

func (g gfx) rect(c uintptr, x, y, w, h int32) {
	var b uintptr
	_, _, _ = procGdipCreateSolidFill.Call(c, uintptr(unsafe.Pointer(&b)))
	_, _, _ = procGdipFillRectangleI.Call(uintptr(g), b, uintptr(x), uintptr(y), uintptr(w), uintptr(h))
	_, _, _ = procGdipDeleteBrush.Call(b)
}

func (g gfx) pen(c uintptr, w float32, dash bool) uintptr {
	var p uintptr
	_, _, _ = procGdipCreatePen1.Call(c, uintptr(math.Float32bits(w)), 2, uintptr(unsafe.Pointer(&p)))
	if dash {
		_, _, _ = procGdipSetPenDashStyle.Call(p, 1)
	}
	return p
}

func (g gfx) ring(c uintptr, w float32, cx, cy, r int32) {
	p := g.pen(c, w, false)
	_, _, _ = procGdipDrawEllipseI.Call(uintptr(g), p, uintptr(cx-r), uintptr(cy-r), uintptr(2*r), uintptr(2*r))
	_, _, _ = procGdipDeletePen.Call(p)
}

func (g gfx) line(c uintptr, w float32, dash bool, x1, y1, x2, y2 int32) {
	p := g.pen(c, w, dash)
	_, _, _ = procGdipDrawLineI.Call(uintptr(g), p, uintptr(x1), uintptr(y1), uintptr(x2), uintptr(y2))
	_, _, _ = procGdipDeletePen.Call(p)
}

func (g gfx) poly(c uintptr, w float32, pts []point) {
	if len(pts) < 2 {
		return
	}
	p := g.pen(c, w, false)
	_, _, _ = procGdipDrawLinesI.Call(uintptr(g), p, uintptr(unsafe.Pointer(&pts[0])), uintptr(len(pts)))
	_, _, _ = procGdipDeletePen.Call(p)
}

func (u *winUI) ensureBuffer() {
	if u.hwnd == 0 {
		return
	}
	var c winRect
	_, _, _ = procGetClientRect.Call(u.hwnd, uintptr(unsafe.Pointer(&c)))
	if c.r < 1 {
		c.r = 1
	}
	if c.b < 1 {
		c.b = 1
	}
	if u.memDC != 0 && u.memW == c.r && u.memH == c.b {
		return
	}
	if u.memDC != 0 {
		_, _, _ = procSelectObject.Call(u.memDC, u.memOld)
		_, _, _ = procDeleteObject.Call(u.memBmp)
		_, _, _ = procDeleteDC.Call(u.memDC)
	}
	var ps paintStruct
	_ = ps
	screen, _, _ := procGetDC.Call(u.hwnd)
	u.memDC, _, _ = procCreateCompatibleDC.Call(screen)
	u.memBmp, _, _ = procCreateCompatibleBitmap.Call(screen, uintptr(c.r), uintptr(c.b))
	u.memOld, _, _ = procSelectObject.Call(u.memDC, u.memBmp)
	_, _, _ = procReleaseDC.Call(u.hwnd, screen)
	u.memW, u.memH = c.r, c.b
}

var (
	procGetDC     = user32.NewProc("GetDC")
	procReleaseDC = user32.NewProc("ReleaseDC")
)

// ---- Painting ----

func (u *winUI) paint() {
	var ps paintStruct
	hdc, _, _ := procBeginPaint.Call(u.hwnd, uintptr(unsafe.Pointer(&ps)))
	u.ensureBuffer()
	d := ps.rc
	dc := u.memDC
	if u.rcHeader.overlaps(d) {
		u.paintHeader(dc)
	}
	switch u.view {
	case viewLive:
		if u.rcHero.overlaps(d) {
			u.paintHero(dc)
		}
		if u.rcPath.overlaps(d) {
			u.paintPath(dc)
		}
		if u.rcGraph.overlaps(d) {
			u.paintGraph(dc)
		}
		if u.rcFoot.overlaps(d) {
			u.paintFoot(dc)
		}
	case viewHistory:
		if u.rcContent.overlaps(d) {
			u.fill(dc, u.rcContent, u.theme.bg)
			u.paintHistory(dc)
		}
	default:
		if u.rcContent.overlaps(d) {
			u.fill(dc, u.rcContent, u.theme.bg)
			for _, l := range u.labels {
				if l.view == u.view && l.text != "" {
					u.text(dc, l.text, l.font, *l.color, l.r, l.flags)
				}
			}
		}
	}
	_, _, _ = procBitBlt.Call(hdc, uintptr(d.l), uintptr(d.t), uintptr(d.w()), uintptr(d.h()), dc, uintptr(d.l), uintptr(d.t), 0x00CC0020)
	_, _, _ = procEndPaint.Call(u.hwnd, uintptr(unsafe.Pointer(&ps)))
}

func (u *winUI) stateColor() uint32 {
	switch u.status.State {
	case "online":
		return u.theme.up
	case "outage":
		return u.theme.down
	}
	return 0xC98A12
}

func (u *winUI) paintHeader(dc uintptr) {
	m := u.S(28)
	u.fill(dc, u.rcHeader, u.theme.bg)
	u.text(dc, "DropTrace", u.fTitle, u.theme.ink, winRect{m, 0, m + u.S(92), u.rcHeader.b}, dtSingleLine|dtVCenter)
	line := u.rcHeader
	line.t = line.b - u.S(1)
	u.fill(dc, line, u.theme.line)

	// A pulse fires on every finished check, so you can see that it's running.
	right := u.width - m
	if right-u.S(190) < m+u.S(96)+4*u.S(78) {
		return
	}
	txt := "Waiting for the first check"
	if !u.lastCheck.IsZero() {
		txt = "Checking every second"
		if !u.WindowVisible() {
			txt = "Checking in the tray"
		}
		if showFPS {
			txt = fmt.Sprintf("%d frames/s in 2s window", u.fps)
		}
	}
	u.text(dc, txt, u.fSmall, u.theme.muted, winRect{right - u.S(170), 0, right, u.rcHeader.b}, dtRight|dtSingleLine|dtVCenter)
	g := newGfx(dc)
	defer g.close()
	cx, cy := right-u.S(180), u.rcHeader.b/2
	since := time.Since(u.lastCheck).Seconds()
	col := u.stateColor()
	g.circle(argb(col, 255), cx, cy, u.S(5))
	if u.motion && since >= 0 && since < 0.7 && !u.lastCheck.IsZero() {
		p := since / 0.7
		g.ring(argb(col, uint32(200*(1-p))), float32(u.S(2)), cx, cy, u.S(5)+int32(p*float64(u.S(9))))
	}
}

func (u *winUI) paintHero(dc uintptr) {
	m := u.S(28)
	right := u.width - m
	u.fill(dc, u.rcHero, u.theme.bg)
	y := u.rcHero.t + u.S(14)
	head := u.heroHead
	hr := winRect{m, y, right, u.rcHero.b}
	calc := hr
	u.measure(head, u.fHero, &calc, dtWordBreak|dtNoPrefix)
	u.text(dc, head, u.fHero, u.theme.ink, hr, dtWordBreak)
	y = calc.b + u.S(6)
	dr := winRect{m, y, right, u.rcHero.b}
	calc = dr
	u.measure(u.status.Detail, u.fBody, &calc, dtWordBreak|dtNoPrefix)
	u.text(dc, u.status.Detail, u.fBody, u.theme.muted, dr, dtWordBreak)
	y = calc.b + u.S(4)
	if o := u.status.Outage; o != nil {
		s := fmt.Sprintf("Down for %s, since %s", formatDuration(time.Since(o.Start)), o.Start.Local().Format("3:04:05 PM"))
		u.text(dc, s, u.fBold, u.theme.ink, winRect{m, y, right, y + u.S(24)}, dtSingleLine)
	}
}

const (
	animSeconds = 0.9  // how long frames are drawn after each check
	dotTravel   = 0.7  // how long the dot takes to move along the path
	rippleTime  = 0.35 // how long a stop's ring lasts
)

// dotProgress eases the dot in and out, which reads as smoother than a constant speed.
func dotProgress(t float64) float64 { return (1 - math.Cos(math.Pi*t)) / 2 }

// dotTimeAt returns when the dot reaches a fraction of its trip.
func dotTimeAt(frac float64) float64 { return math.Acos(1-2*frac) / math.Pi }

func (u *winUI) pathX(i int) int32 {
	m := u.S(28)
	return m + u.S(15) + int32(i)*((u.width-2*m)/4)
}

func (u *winUI) hopColor(state string) uint32 {
	switch state {
	case "up":
		return u.theme.up
	case "down":
		return u.theme.down
	}
	return u.theme.unk
}

// drawPathLayer draws the parts that only change when a check result changes.
func (u *winUI) drawPathLayer() {
	var key strings.Builder
	fmt.Fprintf(&key, "%d %d %d %v|", u.width, u.height, u.dpi, u.theme.dark)
	for _, h := range u.status.Hops {
		key.WriteString(h.State + h.Detail + "|")
	}
	fmt.Fprintf(&key, "%d", u.rcPath.t)
	if key.String() == u.pathKey && u.pathDC != 0 {
		return
	}
	u.pathKey = key.String()
	if u.pathDC != 0 {
		_, _, _ = procSelectObject.Call(u.pathDC, u.pathOld)
		_, _, _ = procDeleteObject.Call(u.pathBmp)
		_, _, _ = procDeleteDC.Call(u.pathDC)
	}
	screen, _, _ := procGetDC.Call(u.hwnd)
	u.pathDC, _, _ = procCreateCompatibleDC.Call(screen)
	u.pathBmp, _, _ = procCreateCompatibleBitmap.Call(screen, uintptr(u.width), uintptr(u.rcPath.b))
	u.pathOld, _, _ = procSelectObject.Call(u.pathDC, u.pathBmp)
	_, _, _ = procReleaseDC.Call(u.hwnd, screen)

	dc := u.pathDC
	u.fill(dc, u.rcPath, u.theme.bg)
	hops := u.status.Hops
	if len(hops) != 4 {
		return
	}
	m := u.S(28)
	span := (u.width - 2*m) / 4
	cy := u.rcPath.t + u.S(24)
	r := u.S(15)
	g := newGfx(dc)
	defer g.close()
	t := u.theme
	for i := 0; i < 3; i++ {
		a, b := hops[i], hops[i+1]
		switch {
		case b.State == "up" && a.State == "up":
			g.line(argb(t.up, 255), float32(u.S(4)), false, u.pathX(i), cy, u.pathX(i+1), cy)
		case a.State == "down" || b.State == "down":
			g.line(argb(t.down, 255), float32(u.S(3)), true, u.pathX(i), cy, u.pathX(i+1), cy)
		default:
			g.line(argb(t.line, 255), float32(u.S(4)), false, u.pathX(i), cy, u.pathX(i+1), cy)
		}
	}
	for i, h := range hops {
		x := u.pathX(i)
		c := u.hopColor(h.State)
		g.circle(argb(t.bg, 255), x, cy, r)
		if h.State == "down" {
			g.circle(argb(c, 255), x, cy, r)
		} else {
			g.ring(argb(c, 255), float32(u.S(4)), x, cy, r-u.S(2))
		}
		mark := argb(c, 255)
		s := u.S(5)
		switch h.State {
		case "up":
			g.line(mark, float32(u.S(2)), false, x-s, cy, x-u.S(1), cy+s-u.S(1))
			g.line(mark, float32(u.S(2)), false, x-u.S(1), cy+s-u.S(1), x+s+u.S(1), cy-s+u.S(1))
		case "down":
			white := argb(t.bg, 255)
			g.line(white, float32(u.S(2)), false, x-s, cy-s, x+s, cy+s)
			g.line(white, float32(u.S(2)), false, x-s, cy+s, x+s, cy-s)
		default:
			u.text(dc, "?", u.fBold, c, winRect{x - r, cy - r, x + r, cy + r}, dtCenter|dtVCenter|dtSingleLine)
		}
		tx := m + int32(i)*span
		tr := winRect{tx, cy + u.S(24), tx + span - u.S(12), cy + u.S(50)}
		u.text(dc, h.Name, u.fBold, t.ink, tr, dtSingleLine|dtEndEllipsis)
		dr := winRect{tx, tr.b, tx + span - u.S(12), u.rcPath.b}
		u.text(dc, h.Detail, u.fSmall, t.muted, dr, dtWordBreak)
	}
}

func (u *winUI) paintPath(dc uintptr) {
	u.drawPathLayer()
	r0 := u.rcPath
	_, _, _ = procBitBlt.Call(dc, uintptr(r0.l), uintptr(r0.t), uintptr(r0.w()), uintptr(r0.h()), u.pathDC, uintptr(r0.l), uintptr(r0.t), 0x00CC0020)
	hops := u.status.Hops
	if len(hops) != 4 || !u.motion || u.lastCheck.IsZero() {
		return
	}
	since := time.Since(u.lastCheck).Seconds()
	if since < 0 || since >= animSeconds {
		return
	}

	// How far the latest check got along the path.
	reach := 0
	for i := 1; i < 4; i++ {
		if hops[i].State == "down" {
			reach = i
			break
		}
		if hops[i].State == "up" || (i == 1 && hops[2].State == "up") {
			reach = i
		}
	}
	t := u.theme
	cy := u.rcPath.t + u.S(24)
	start, end := u.pathX(0), u.pathX(reach)
	g := newGfx(dc)
	defer g.close()

	for i := 0; i <= reach; i++ {
		arrive := 0.0
		if end > start {
			arrive = dotTravel * dotTimeAt(float64(u.pathX(i)-start)/float64(end-start))
		}
		age := since - arrive
		if age >= 0 && age < rippleTime {
			q := age / rippleTime
			g.ring(argb(u.hopColor(hops[i].State), uint32(170*(1-q))), float32(u.S(2)), u.pathX(i), cy, u.S(15)+int32(q*float64(u.S(10))))
		}
	}
	if since >= dotTravel || end <= start {
		return
	}
	at := func(s float64) int32 {
		if s < 0 {
			s = 0
		}
		return start + int32(float64(end-start)*dotProgress(s/dotTravel))
	}
	// A short fading trail keeps the fast middle of the trip from looking like jumps.
	for i := 4; i >= 1; i-- {
		g.circle(argb(t.accent, uint32(110-i*22)), at(since-float64(i)*0.012), cy, u.S(6)-int32(i)*u.S(6)/8)
	}
	g.circle(argb(t.accent, 255), at(since), cy, u.S(6))
}

func max32(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}

func (u *winUI) paintGraph(dc uintptr) {
	t := u.theme
	u.fill(dc, u.rcGraph, t.bg)
	m := u.S(28)
	right := u.width - m
	top := u.rcGraph.t + u.S(4)
	u.text(dc, "Internet response time, last 2 minutes", u.fBold, t.ink, winRect{m, top, right - u.S(120), top + u.S(24)}, dtSingleLine|dtEndEllipsis)

	now := time.Now()
	const window = 120.0
	gut := u.S(64)
	pl, pr := m+gut, right
	pt, pb := top+u.S(32), u.rcGraph.b-u.S(22)
	if pb-pt < u.S(20) || pr-pl < u.S(50) {
		return
	}
	xOf := func(tm time.Time) int32 {
		return pr - int32(now.Sub(tm).Seconds()/window*float64(pr-pl))
	}
	lastMs := -1
	lastOK := true
	for _, p := range u.points {
		if p.Ms >= 0 || !p.OK {
			lastMs, lastOK = p.Ms, p.OK
		}
	}
	scale := GraphScale(u.points, now)
	yOf := func(ms int) int32 { return pb - int32(float64(ms)/float64(scale)*float64(pb-pt)) }

	now2 := "Waiting for checks"
	if !lastOK {
		now2 = "No response"
	} else if lastMs >= 0 {
		now2 = msText(lastMs) + " now"
	}
	u.text(dc, now2, u.fBold, t.ink, winRect{right - u.S(160), top, right, top + u.S(24)}, dtRight|dtSingleLine)

	g := newGfx(dc)
	defer g.close()
	for _, ms := range []int{0, scale / 2, scale} {
		y := yOf(ms)
		col := t.line
		g.line(argb(col, 255), 1, false, pl, y, pr, y)
		u.text(dc, fmt.Sprintf("%d ms", ms), u.fSmall, t.muted, winRect{m, y - u.S(10), pl - u.S(8), y + u.S(10)}, dtRight|dtSingleLine|dtVCenter)
	}
	u.text(dc, "2 min ago", u.fSmall, t.muted, winRect{pl, pb + u.S(3), pl + u.S(120), pb + u.S(21)}, dtSingleLine)
	u.text(dc, "1 min ago", u.fSmall, t.muted, winRect{(pl+pr)/2 - u.S(60), pb + u.S(3), (pl+pr)/2 + u.S(60), pb + u.S(21)}, dtSingleLine|dtCenter)
	u.text(dc, "Now", u.fSmall, t.muted, winRect{pr - u.S(60), pb + u.S(3), pr, pb + u.S(21)}, dtRight|dtSingleLine)

	var seg []point
	var head *point
	var prev time.Time
	flush := func() {
		g.poly(argb(t.accent, 255), float32(u.S(2)), seg)
		seg = seg[:0]
	}
	for i, p := range u.points {
		x := xOf(p.T)
		if x < pl {
			prev = p.T
			continue
		}
		if !p.OK {
			next := now
			if i+1 < len(u.points) {
				next = u.points[i+1].T
			}
			nx := xOf(next)
			if nx > pr {
				nx = pr
			}
			w := nx - x
			if w < u.S(2) {
				w = u.S(2)
			}
			g.rect(argb(t.down, 70), x, pt, w, pb-pt)
			g.rect(argb(t.down, 255), x, pt, w, u.S(3))
			flush()
			prev = p.T
			continue
		}
		if p.Ms < 0 {
			continue
		}
		if !prev.IsZero() && p.T.Sub(prev) > 12*time.Second {
			flush()
		}
		ms := p.Ms
		if ms > scale {
			ms = scale
		}
		pnt := point{x, yOf(ms)}
		seg = append(seg, pnt)
		head = &pnt
		prev = p.T
	}
	flush()
	if head != nil && lastOK {
		g.circle(argb(t.accent, 255), head.x, head.y, u.S(4))
	}
	if len(u.points) == 0 {
		u.text(dc, "The first checks will appear here.", u.fBody, t.muted, winRect{pl, pt, pr, pb}, dtCenter|dtVCenter|dtSingleLine)
	}

	// Hover shows the exact value at that moment.
	if u.mouseIn && u.mouse.x >= pl && u.mouse.x <= pr && u.mouse.y >= pt && u.mouse.y <= pb {
		bestD := int32(1 << 30)
		var best *CheckPoint
		for i := range u.points {
			d := xOf(u.points[i].T) - u.mouse.x
			if d < 0 {
				d = -d
			}
			if d < bestD {
				bestD, best = d, &u.points[i]
			}
		}
		if best != nil && bestD < u.S(24) {
			bx := xOf(best.T)
			g.line(argb(t.ink, 120), 1, false, bx, pt, bx, pb)
			val := "No response"
			if best.OK {
				val = "Reachable"
				if best.Ms >= 0 {
					val = msText(best.Ms)
				}
			}
			u.tip(dc, best.T.Format("3:04:05 PM")+": "+val, bx, pt+u.S(6))
		}
	}
}

// tip draws a small label that stays inside the window.
func (u *winUI) tip(dc uintptr, s string, x, y int32) {
	r := winRect{0, 0, u.S(340), 200}
	u.measure(s, u.fSmall, &r, dtWordBreak|dtNoPrefix)
	w, h := r.w()+u.S(16), r.h()+u.S(10)
	l := x + u.S(8)
	if l+w > u.width-u.S(8) {
		l = x - w - u.S(8)
	}
	if l < u.S(8) {
		l = u.S(8)
	}
	box := winRect{l, y, l + w, y + h}
	u.fill(dc, box, u.theme.ink)
	box.l += u.S(8)
	box.t += u.S(5)
	u.text(dc, s, u.fSmall, u.theme.bg, box, dtWordBreak)
}

func (u *winUI) paintHistory(dc uintptr) {
	t := u.theme
	m := u.S(28)
	right := u.width - m
	y := u.rcHeader.b + u.S(12)
	u.text(dc, "History", u.fH2, t.ink, winRect{m, y, right, y + u.S(32)}, dtSingleLine|dtVCenter)

	s := u.stats
	up := "Not enough data"
	if s.UptimePercent != nil {
		up = fmt.Sprintf("%.2f%%", *s.UptimePercent)
	}
	longest := "None"
	if s.Count > 0 {
		longest = formatDuration(time.Duration(s.LongestSeconds) * time.Second)
	}
	word := "outages"
	if s.Count == 1 {
		word = "outage"
	}
	figs := [][2]string{{itoa(s.Count), word}, {formatDuration(time.Duration(s.DowntimeSeconds) * time.Second), "total downtime"},
		{longest, "longest outage"}, {up, "available while monitored"}, {formatDuration(time.Duration(s.MonitoredSeconds) * time.Second), "monitored"}}
	fy := u.rcStrips.t
	fw := (right - m) / 5
	for i, f := range figs {
		x := m + int32(i)*fw
		u.text(dc, f[0], u.fFig, t.ink, winRect{x, fy, x + fw - u.S(6), fy + u.S(36)}, dtSingleLine|dtVCenter|dtEndEllipsis)
		u.text(dc, f[1], u.fSmall, t.muted, winRect{x, fy + u.S(36), x + fw - u.S(6), fy + u.S(56)}, dtSingleLine|dtEndEllipsis)
	}

	// One row per day, with outages marked by time of day.
	u.marks = u.marks[:0]
	now := time.Now()
	labelW := u.S(96)
	tl, tr := m+labelW, right
	ty := fy + u.S(66)
	g := newGfx(dc)
	defer g.close()
	for i := 0; i < 7; i++ {
		d := time.Date(now.Year(), now.Month(), now.Day()-i, 0, 0, 0, 0, now.Location())
		next := d.AddDate(0, 0, 1)
		length := next.Sub(d).Seconds()
		ry := ty + int32(i)*u.S(22)
		u.text(dc, d.Format("Mon, Jan 2"), u.fSmall, t.muted, winRect{m, ry, tl - u.S(6), ry + u.S(20)}, dtSingleLine|dtVCenter)
		bar := winRect{tl, ry + u.S(4), tr, ry + u.S(17)}
		has := u.coverage[d.Format("2006-01-02")] > 0
		for _, o := range u.weekOut {
			end := now
			if o.End != nil {
				end = *o.End
			}
			if end.After(d) && o.Start.Before(next) {
				has = true
			}
		}
		if has {
			fillR := bar
			if i == 0 {
				fillR.r = tl + int32(now.Sub(d).Seconds()/length*float64(tr-tl))
			}
			u.fill(dc, fillR, t.track)
		} else {
			hb, _, _ := procCreateHatchBrush.Call(3, colorRef(t.track))
			_, _, _ = procSetBkColor.Call(dc, colorRef(t.bg))
			_, _, _ = procSetBkMode.Call(dc, 2)
			_, _, _ = procFillRect.Call(dc, uintptr(unsafe.Pointer(&bar)), hb)
			_, _, _ = procDeleteObject.Call(hb)
		}
		for _, o := range u.weekOut {
			end := now
			if o.End != nil {
				end = *o.End
			}
			if !end.After(d) || !o.Start.Before(next) {
				continue
			}
			a, b := o.Start, end
			if a.Before(d) {
				a = d
			}
			if b.After(next) {
				b = next
			}
			x1 := tl + int32(a.Sub(d).Seconds()/length*float64(tr-tl))
			x2 := tl + int32(b.Sub(d).Seconds()/length*float64(tr-tl))
			if x2-x1 < u.S(3) {
				x2 = x1 + u.S(3)
			}
			mr := winRect{x1, bar.t, x2, bar.b}
			u.fill(dc, mr, t.down)
			hit := mr
			hit.l -= u.S(3)
			hit.r += u.S(3)
			u.marks = append(u.marks, markRect{hit, fmt.Sprintf("%s. Down %s. %s.", o.Start.Local().Format("Mon, Jan 2, 3:04 PM"), formatDuration(o.Duration(now)), CauseInfoFor(o.Cause).Label)})
		}
	}
	ay := ty + 7*u.S(22)
	for i, name := range []string{"12 AM", "6 AM", "12 PM", "6 PM", "12 AM"} {
		x := tl + int32(i)*(tr-tl)/4
		flags := uintptr(dtCenter)
		r := winRect{x - u.S(40), ay, x + u.S(40), ay + u.S(20)}
		if i == 0 {
			r.l, flags = x, dtLeft
		}
		if i == 4 {
			r.r, flags = x, dtRight
		}
		u.text(dc, name, u.fSmall, t.muted, r, flags|dtSingleLine)
	}
	_ = g
	if len(u.outages) == 0 {
		lt := u.rcStrips.b + u.S(4)
		u.text(dc, "No outages logged in this period. DropTrace adds them here when your connection drops.", u.fBody, t.muted, winRect{m, lt + u.S(8), right, lt + u.S(50)}, dtWordBreak)
	}
	if u.mouseIn {
		for _, mk := range u.marks {
			if mk.r.has(u.mouse.x, u.mouse.y) {
				u.tip(dc, mk.text, u.mouse.x, mk.r.t-u.S(30))
				break
			}
		}
	}
}

func (u *winUI) paintFoot(dc uintptr) {
	u.fill(dc, u.rcFoot, u.theme.bg)
	m := u.S(28)
	u.text(dc, "Checks run every second while this window is open and every 3 seconds in the tray.", u.fSmall, u.theme.muted,
		winRect{m, u.rcFoot.t, u.width - m, u.rcFoot.b}, dtSingleLine|dtVCenter|dtEndEllipsis)
}

func (u *winUI) drawButton(di *drawItemStruct) {
	c := u.byHwnd[di.hwndItem]
	if c == nil {
		return
	}
	t := u.theme
	dc := di.hDC
	r := di.rc
	pressed := di.itemState&odsSelected != 0
	focus := di.itemState&odsFocus != 0 && di.itemState&odsNoFocusRect == 0 // keyboard use only
	font := u.fBody
	switch c.kind {
	case kindNav:
		u.fill(dc, r, t.bg)
		col := t.muted
		if c.on || c.hover {
			col = t.ink
		}
		if c.on {
			font = u.fBold
			bar := winRect{r.l + u.S(10), r.b - u.S(3), r.r - u.S(10), r.b}
			u.fill(dc, bar, t.accent)
		}
		u.text(dc, c.text, font, col, r, dtCenter|dtVCenter|dtSingleLine)
	case kindSeg:
		bg := t.bg
		ink := t.ink
		if c.on {
			bg, ink, font = t.accent, t.accentInk, u.fBold
		} else if c.hover {
			bg = t.track
		}
		u.fill(dc, r, t.line)
		inner := winRect{r.l + 1, r.t + 1, r.r - 1, r.b - 1}
		u.fill(dc, inner, bg)
		u.text(dc, c.text, font, ink, r, dtCenter|dtVCenter|dtSingleLine)
	case kindPrimary, kindSecondary, kindDanger:
		bg, ink, edge := t.accent, t.accentInk, t.accent
		if c.kind == kindSecondary {
			bg, ink = t.bg, t.accent
		}
		if c.kind == kindDanger {
			bg, ink, edge = t.bg, t.down, t.down
		}
		if c.kind != kindPrimary && c.hover {
			bg = t.track
		}
		if pressed {
			bg = t.line
			if c.kind == kindPrimary {
				bg, ink = t.ink, t.bg
			}
		}
		u.fill(dc, r, edge)
		u.fill(dc, winRect{r.l + 1, r.t + 1, r.r - 1, r.b - 1}, bg)
		u.text(dc, c.text, u.fBold, ink, r, dtCenter|dtVCenter|dtSingleLine)
	case kindCheck:
		u.fill(dc, r, t.bg)
		size := u.S(18)
		bx := winRect{r.l + u.S(2), r.t + (r.h()-size)/2, r.l + u.S(2) + size, r.t + (r.h()-size)/2 + size}
		edge := t.muted
		if c.hover {
			edge = t.ink
		}
		fillC := t.panel
		if c.on {
			edge, fillC = t.accent, t.accent
		}
		u.fill(dc, bx, edge)
		u.fill(dc, winRect{bx.l + 1, bx.t + 1, bx.r - 1, bx.b - 1}, fillC)
		if c.on {
			g := newGfx(dc)
			w := argb(t.accentInk, 255)
			g.line(w, float32(u.S(2)), false, bx.l+u.S(4), bx.t+u.S(9), bx.l+u.S(8), bx.t+u.S(13))
			g.line(w, float32(u.S(2)), false, bx.l+u.S(8), bx.t+u.S(13), bx.l+u.S(14), bx.t+u.S(5))
			g.close()
		}
		tr := r
		tr.l = bx.r + u.S(10)
		u.text(dc, c.text, u.fBody, t.ink, tr, dtLeft|dtVCenter|dtSingleLine|dtEndEllipsis)
	}
	if focus {
		ring := []point{{r.l, r.t}, {r.r - 1, r.t}, {r.r - 1, r.b - 1}, {r.l, r.b - 1}, {r.l, r.t}}
		g := newGfx(dc)
		g.poly(argb(t.accent, 255), float32(u.S(2)), ring)
		g.close()
	}
}

// ---- Actions ----

func (u *winUI) selectOne(ids []int, chosen int) {
	for _, id := range ids {
		c := u.controls[id]
		c.on = id == chosen
		_, _, _ = procInvalidateRect.Call(c.hwnd, 0, 0)
	}
}

func (u *winUI) setCheck(id int, on bool) {
	c := u.controls[id]
	c.on = on
	_, _, _ = procInvalidateRect.Call(c.hwnd, 0, 0)
}

func (u *winUI) showSettingsMsg(s string) {
	u.setMsg = s
	u.layout()
	u.invalidate(u.rcContent)
}

func (u *winUI) showReportMsg(s string, isErr bool) {
	u.repMsg, u.repErr = s, isErr
	u.layout()
	u.invalidate(u.rcContent)
}

func (u *winUI) editText(id int) string {
	h := u.edits[id]
	n, _, _ := procGetWindowTextLengthW.Call(h)
	buf := make([]uint16, n+1)
	_, _, _ = procGetWindowTextW.Call(h, uintptr(unsafe.Pointer(&buf[0])), n+1)
	return syscall.UTF16ToString(buf)
}

func (u *winUI) saveDialog(title, filter, ext, name string) (string, bool) {
	buf := make([]uint16, 1024)
	copyUTF16(buf, name)
	f, _ := syscall.UTF16FromString(strings.ReplaceAll(filter, "|", "\x01"))
	for i := range f {
		if f[i] == 1 {
			f[i] = 0
		}
	}
	f = append(f, 0)
	ofn := openFileName{hwndOwner: u.hwnd, lpstrFilter: &f[0], nFilterIndex: 1, lpstrFile: &buf[0], nMaxFile: uint32(len(buf)),
		lpstrTitle: wstr(title), lpstrDefExt: wstr(ext), Flags: 0x2 | 0x8 | 0x800}
	ofn.lStructSize = uint32(unsafe.Sizeof(ofn))
	r, _, _ := procGetSaveFileNameW.Call(uintptr(unsafe.Pointer(&ofn)))
	return syscall.UTF16ToString(buf), r != 0
}

func (u *winUI) savePDF() {
	path, ok := u.saveDialog("Save outage report", "PDF report (*.pdf)|*.pdf|", "pdf", "Internet outage report.pdf")
	if !ok {
		return
	}
	d := BuildReport(u.store, ReportOptions{Name: u.editText(idEditName), Provider: u.editText(idEditProvider),
		Account: u.editText(idEditAcct), Days: u.repDays, ProviderOnly: !u.repAll}, time.Now())
	if err := os.WriteFile(path, RenderPDF(d), 0o644); err != nil {
		u.showReportMsg("DropTrace couldn't save the report. Choose another folder and try again. "+err.Error(), true)
		return
	}
	u.showReportMsg("Saved "+filepath.Base(path)+" and opened it.", false)
	shellOpen(path)
}

func (u *winUI) saveCSV() {
	path, ok := u.saveDialog("Save outage history", "CSV file (*.csv)|*.csv|", "csv", "DropTrace outages.csv")
	if !ok {
		return
	}
	var b bytes.Buffer
	err := WriteCSV(&b, u.store, u.repDays, time.Now())
	if err == nil {
		err = os.WriteFile(path, b.Bytes(), 0o644)
	}
	if err != nil {
		u.showReportMsg("DropTrace couldn't save the CSV. Choose another folder and try again. "+err.Error(), true)
		return
	}
	u.showReportMsg("Saved "+filepath.Base(path)+".", false)
}

func (u *winUI) onCommand(id int) {
	if id >= idNavBase && id < idNavBase+4 {
		u.setView(id - idNavBase)
		return
	}
	days := map[int]int{idHist7: 7, idHist30: 30, idHist90: 90}
	if d, ok := days[id]; ok {
		u.histDays = d
		u.selectOne([]int{idHist7, idHist30, idHist90}, id)
		u.refreshHistory()
		u.invalidate(u.rcContent)
		return
	}
	days = map[int]int{idRep7: 7, idRep30: 30, idRep90: 90, idRep365: 365}
	if d, ok := days[id]; ok {
		u.repDays = d
		u.selectOne([]int{idRep7, idRep30, idRep90, idRep365}, id)
		return
	}
	switch id {
	case idRepAll:
		u.repAll = !u.repAll
		u.setCheck(id, u.repAll)
	case idSavePDF:
		u.savePDF()
	case idSaveCSV:
		u.saveCSV()
	case idDonateReport, idDonateSettings:
		shellOpen(DonateURL)
	case idStartupSettings:
		shellOpen("ms-settings:startupapps")
	case idAuto:
		want := !u.controls[idAuto].on
		if err := setAutostart(want); err != nil {
			u.showSettingsMsg("Windows didn't allow the startup change. " + err.Error())
			return
		}
		u.setCheck(idAuto, want)
		u.showSettingsMsg("")
	case idNotify:
		want := !u.controls[idNotify].on
		if err := u.store.SetNotify(want); err != nil {
			u.showSettingsMsg("DropTrace couldn't save that setting. " + err.Error())
			return
		}
		u.setCheck(idNotify, want)
		u.showSettingsMsg("")
	case idDelete:
		c := u.controls[idDelete]
		if !u.armed {
			u.armed = true
			c.text = "Click again to delete all history"
			_, _, _ = procSetWindowTextW.Call(c.hwnd, uintptr(unsafe.Pointer(wstr(c.text))))
			_, _, _ = procInvalidateRect.Call(c.hwnd, 0, 0)
			_, _, _ = procSetTimer.Call(u.hwnd, timerDelete, 5000, 0)
			return
		}
		u.disarmDelete()
		if err := u.store.Clear(); err != nil {
			u.showSettingsMsg("DropTrace couldn't delete the history. " + err.Error())
			return
		}
		u.showSettingsMsg("History deleted.")
	case idExit:
		_, _, _ = procDestroyWindow.Call(u.hwnd)
	}
}

func (u *winUI) disarmDelete() {
	u.armed = false
	_, _, _ = procKillTimer.Call(u.hwnd, timerDelete)
	c := u.controls[idDelete]
	c.text = "Delete history"
	_, _, _ = procSetWindowTextW.Call(c.hwnd, uintptr(unsafe.Pointer(wstr(c.text))))
	_, _, _ = procInvalidateRect.Call(c.hwnd, 0, 0)
}

func (u *winUI) showApp() {
	iconic, _, _ := procIsIconic.Call(u.hwnd)
	if iconic != 0 {
		_, _, _ = procShowWindow.Call(u.hwnd, swRestore)
	} else {
		_, _, _ = procShowWindow.Call(u.hwnd, swShow)
	}
	_, _, _ = procSetForegroundWindow.Call(u.hwnd)
	u.visible.Store(true)
	u.updateTimer()
	if u.view == viewHistory {
		u.refreshHistory()
	}
	u.invalidate(winRect{0, 0, u.width, u.height})
}

// ---- Tray ----

func copyUTF16(dst []uint16, s string) {
	v, err := syscall.UTF16FromString(s)
	if err != nil {
		return
	}
	if len(v) > len(dst) {
		v = v[:len(dst)]
		v[len(v)-1] = 0
	}
	copy(dst, v)
}

// makeIcon draws a filled status dot, so no image files are needed.
func makeIcon(r, g, b byte) uintptr {
	const size = 32
	pixels := make([]byte, size*size*4)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx, dy := float64(x)-15.5, float64(y)-15.5
			dist2 := dx*dx + dy*dy
			alpha := 0.0
			switch {
			case dist2 <= 13.5*13.5:
				alpha = 1
			case dist2 <= 14.5*14.5:
				alpha = 0.5
			}
			i := (y*size + x) * 4
			// Icon bitmaps are BGRA with straight (not premultiplied) alpha.
			pixels[i], pixels[i+1], pixels[i+2] = b, g, r
			pixels[i+3] = byte(255 * alpha)
		}
	}
	mask := make([]byte, size*size/8)
	color, _, _ := procCreateBitmap.Call(size, size, 1, 32, uintptr(unsafe.Pointer(&pixels[0])))
	maskBmp, _, _ := procCreateBitmap.Call(size, size, 1, 1, uintptr(unsafe.Pointer(&mask[0])))
	info := iconInfo{fIcon: 1, hbmMask: maskBmp, hbmColor: color}
	h, _, _ := procCreateIconIndirect.Call(uintptr(unsafe.Pointer(&info)))
	return h
}

func (u *winUI) iconFor(state string) uintptr {
	if h := u.icons[state]; h != 0 {
		return h
	}
	return u.icons["checking"]
}

func (u *winUI) notifyIcon(op uintptr, flags uint32, fill func(*notifyIconData)) bool {
	nid := notifyIconData{hWnd: u.hwnd, uID: 1, uFlags: flags, uCallbackMessage: wmTray}
	nid.cbSize = uint32(unsafe.Sizeof(nid))
	if fill != nil {
		fill(&nid)
	}
	r, _, _ := procShellNotifyIconW.Call(op, uintptr(unsafe.Pointer(&nid)))
	return r != 0
}

func (u *winUI) addTrayIcon() {
	state, tip := u.trayState, u.trayTip
	ok := u.notifyIcon(nimAdd, nifMessage|nifIcon|nifTip, func(n *notifyIconData) {
		n.hIcon = u.iconFor(state)
		copyUTF16(n.szTip[:], tip)
	})
	if !ok {
		log.Print("tray icon not added; will retry when the taskbar is ready")
	}
}

func (u *winUI) balloon(title, body string) {
	u.notifyIcon(nimModify, nifInfo, func(n *notifyIconData) {
		copyUTF16(n.szInfoTitle[:], title)
		copyUTF16(n.szInfo[:], body)
		n.dwInfoFlags = niifInfo
	})
}

func (u *winUI) showMenu() {
	menu, _, _ := procCreatePopupMenu.Call()
	defer procDestroyMenu.Call(menu)
	_, _, _ = procAppendMenuW.Call(menu, 0, menuOpen, uintptr(unsafe.Pointer(wstr("Open DropTrace"))))
	_, _, _ = procAppendMenuW.Call(menu, 0, menuExit, uintptr(unsafe.Pointer(wstr("Exit"))))
	var pt point
	_, _, _ = procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	// Without this, the menu won't close when someone clicks elsewhere.
	_, _, _ = procSetForegroundWindow.Call(u.hwnd)
	cmd, _, _ := procTrackPopupMenu.Call(menu, 0x0100|0x0002, uintptr(pt.x), uintptr(pt.y), 0, u.hwnd, 0)
	_, _, _ = procPostMessageW.Call(u.hwnd, wmNull, 0, 0)
	switch cmd {
	case menuOpen:
		u.showApp()
	case menuExit:
		_, _, _ = procDestroyWindow.Call(u.hwnd)
	}
}

// ---- Messages ----

func windowProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	u := theUI
	if u == nil || u.hwnd == 0 {
		switch uint32(msg) {
		case wmGetMinMaxInfo, wmSize, wmPaint:
		}
		r, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
		return r
	}
	switch uint32(msg) {
	case wmPaint:
		u.paint()
		return 0
	case wmEraseBkgnd:
		return 1
	case wmSize:
		if wParam == sizeMinimized {
			u.visible.Store(false)
		} else if wParam == 0 || wParam == 2 {
			if !u.visible.Load() {
				if v, _, _ := procIsWindowVisible.Call(hwnd); v != 0 {
					u.visible.Store(true)
				}
			}
			u.layout()
			u.invalidate(winRect{0, 0, u.width, u.height})
		}
		u.updateTimer()
		return 0
	case wmGetMinMaxInfo:
		info := (*[10]int32)(ptr(lParam))
		info[6], info[7] = u.S(720), u.S(600)
		return 0
	case wmTimer:
		switch wParam {
		case timerAnim:
			u.onTick()
		case timerDelete:
			u.disarmDelete()
		}
		return 0
	case wmDrawItem:
		u.drawButton((*drawItemStruct)(ptr(lParam)))
		return 1
	case wmCommand:
		if (wParam>>16)&0xFFFF == 0 {
			u.onCommand(int(wParam & 0xFFFF))
		}
		return 0
	case wmCtlColorEdit:
		_, _, _ = procSetTextColor.Call(wParam, colorRef(u.theme.ink))
		_, _, _ = procSetBkColor.Call(wParam, colorRef(u.theme.panel))
		return u.editBrush
	case wmMouseMove:
		u.mouse = point{int32(int16(lParam & 0xFFFF)), int32(int16((lParam >> 16) & 0xFFFF))}
		u.mouseIn = true
		if !u.tracking {
			u.tracking = true
			tm := trackMouse{dwFlags: tmeLeave, hwnd: hwnd}
			tm.cbSize = uint32(unsafe.Sizeof(tm))
			_, _, _ = procTrackMouseEvent.Call(uintptr(unsafe.Pointer(&tm)))
		}
		if u.view == viewLive {
			u.invalidate(u.rcGraph)
		} else if u.view == viewHistory {
			u.invalidate(u.rcStrips)
		}
		return 0
	case wmMouseLeave:
		u.mouseIn, u.tracking = false, false
		if u.view == viewLive {
			u.invalidate(u.rcGraph)
		} else if u.view == viewHistory {
			u.invalidate(u.rcStrips)
		}
		return 0
	case wmDpiChanged:
		u.dpi = int32(wParam & 0xFFFF)
		r := (*winRect)(ptr(lParam))
		_, _, _ = procSetWindowPos.Call(hwnd, 0, uintptr(r.l), uintptr(r.t), uintptr(r.w()), uintptr(r.h()), 0x4|0x10)
		u.makeFonts()
		u.layout()
		u.invalidate(winRect{0, 0, u.width, u.height})
		return 0
	case wmSettingChange:
		if lParam != 0 {
			name := syscall.UTF16ToString((*[64]uint16)(ptr(lParam))[:])
			if name == "ImmersiveColorSet" {
				u.loadTheme()
				u.applyTitleBar()
				u.applyListTheme()
				for _, c := range u.controls {
					_, _, _ = procInvalidateRect.Call(c.hwnd, 0, 0)
				}
				u.invalidate(winRect{0, 0, u.width, u.height})
			}
		}
	case wmCheckDone:
		u.takeUpdate()
		return 0
	case wmAnimFrame:
		u.framePending.Store(false)
		if u.view == viewLive && u.WindowVisible() {
			u.invalidate(u.rcPath)
			u.invalidate(u.rcHeader)
			_, _, _ = procUpdateWindow.Call(hwnd)
			u.countFrame()
		}
		return 0
	case wmShowApp:
		u.showApp()
		return 0
	case wmTray:
		switch uint32(lParam) {
		case wmLButtonUp:
			u.showApp()
		case wmRButtonUp:
			u.showMenu()
		}
		return 0
	case wmClose:
		_, _, _ = procShowWindow.Call(hwnd, swHide)
		u.visible.Store(false)
		u.updateTimer()
		if !u.closeTipShown {
			u.closeTipShown = true
			u.balloon("DropTrace is still logging", "It keeps running in the tray. Right-click the tray icon and choose Exit to stop it.")
		}
		return 0
	case wmEndSession:
		if wParam != 0 && endSessionStore != nil {
			_ = endSessionStore.Save()
		}
		return 0
	case wmDestroy:
		u.notifyIcon(nimDelete, 0, nil)
		_, _, _ = procPostQuitMessage.Call(0)
		return 0
	}
	if u.taskbarCreated != 0 && uint32(msg) == u.taskbarCreated {
		u.addTrayIcon() // Explorer restarted or the taskbar just became ready
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return r
}

var procIsWindowVisible = user32.NewProc("IsWindowVisible")

func (u *winUI) countFrame() {
	u.frameCount++
	if time.Since(u.frameWindow) < 2*time.Second {
		return
	}
	u.fps = u.frameCount * 1000 / max(int(time.Since(u.frameWindow).Milliseconds()), 1)
	u.frameCount, u.frameWindow = 0, time.Now()
}

func historyLine() string {
	if isPackaged() {
		return "" // the note above already says history stays on this PC
	}
	return "History is stored in " + dataDir() + "\n"
}
