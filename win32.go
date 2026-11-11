package main

import (
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	wmDestroy          = 0x0002
	wmClose            = 0x0010
	wmQueryEndSession  = 0x0011
	wmEndSession       = 0x0016
	wmSetFont          = 0x0030
	wmCommand          = 0x0111
	wmContextMenu      = 0x007B
	wmNcDestroy        = 0x0082
	wmLeftButtonUp     = 0x0202
	wmRightButtonUp    = 0x0205
	wmApp              = 0x8000
	wmNull             = 0x0000
	wmDpiChanged       = 0x02E0
	wmAppTray          = wmApp + 1
	wmDroverCanClose   = wmApp + 501
	eventSystemAlert   = 0x0002
	eventObjectShow    = 0x8002
	eventObjectNameChg = 0x800C
	objIDClient        = 0xFFFFFFFC
	childIDSelf        = 0
)

const (
	wsPopup         = 0x80000000
	wsCaption       = 0x00C00000
	wsSysMenu       = 0x00080000
	wsChild         = 0x40000000
	wsVisible       = 0x10000000
	wsTabStop       = 0x00010000
	wsBorder        = 0x00800000
	wsClipChildren  = 0x02000000
	wsVScroll       = 0x00200000
	wsExDlgModalFrm = 0x00000001
	wsExAppWindow   = 0x00040000
	wsExCtrlParent  = 0x00010000
)

const (
	swHide   = 0
	swShow   = 5
	swNormal = 1
)

const (
	bsPushButton    = 0x00000000
	bsDefPushButton = 0x00000001
	ssLeft          = 0x00000000
	esAutoHScroll   = 0x00000080
	cbsDropDownList = 0x00000003
	cbAddString     = 0x0143
	cbGetCurSel     = 0x0147
	cbSetCurSel     = 0x014E
	bnClicked       = 0
	idOK            = 1
	idCancel        = 2
)

const (
	mfString     = 0x00000000
	mfGrayed     = 0x00000001
	mfDisabled   = 0x00000002
	mfChecked    = 0x00000008
	mfPopup      = 0x00000010
	mfSeparator  = 0x00000800
	tpmRightBtn  = 0x0002
	tpmReturnCmd = 0x0100
)

const (
	nimAdd        = 0
	nimModify     = 1
	nimDelete     = 2
	nimSetVersion = 4
	nifMessage    = 0x00000001
	nifIcon       = 0x00000002
	nifTip        = 0x00000004
	nifInfo       = 0x00000010
	nifShowTip    = 0x00000080
	niifError     = 0x00000003
	notifyVer4    = 4
	ninSelect     = 0x0400
	ninKeySelect  = 0x0401
)

const (
	idiApplication = 32512
	idcArrow       = 32512
)

const (
	spiGetWorkArea = 0x0030
	smCXSmIcon     = 49
	smCYSmIcon     = 50
	dpiCtxPerMonV2 = ^uintptr(3) // (HANDLE)-4
)

const (
	ofnFileMustExist = 0x00001000
	ofnPathMustExist = 0x00000800
	ofnExplorer      = 0x00080000
	ofnNoChangeDir   = 0x00000008
)

const (
	seeMaskNoAsync = 0x00000100
	msgfltAllow    = 1
)

const (
	swpNoZOrder   = 0x0004
	swpNoActivate = 0x0010
)

const (
	defaultCharset   = 1
	cleartypeQuality = 5
	fwNormal         = 400
	defaultPitch     = 0
)

const (
	icccStandardClasses = 0x4000
)

// --- Win32 structs -------------------------------------------------------

type winRect struct {
	Left, Top, Right, Bottom int32
}

type winPoint struct {
	X, Y int32
}

type winMsg struct {
	Hwnd     uintptr
	Message  uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	Pt       winPoint
	LPrivate uint32
}

type wndClassEx struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

type notifyIconData struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         windows.GUID
	HBalloonIcon     uintptr
}

type browseInfo struct {
	HwndOwner      uintptr
	PidlRoot       uintptr
	PszDisplayName *uint16
	LpszTitle      *uint16
	UlFlags        uint32
	Lpfn           uintptr
	LParam         uintptr
	IImage         int32
}

type openFileName struct {
	LStructSize       uint32
	HwndOwner         uintptr
	HInstance         uintptr
	LpstrFilter       *uint16
	LpstrCustomFilter *uint16
	NMaxCustFilter    uint32
	NFilterIndex      uint32
	LpstrFile         *uint16
	NMaxFile          uint32
	LpstrFileTitle    *uint16
	NMaxFileTitle     uint32
	LpstrInitialDir   *uint16
	LpstrTitle        *uint16
	Flags             uint32
	NFileOffset       uint16
	NFileExtension    uint16
	LpstrDefExt       *uint16
	LCustData         uintptr
	LpfnHook          uintptr
	LpTemplateName    *uint16
	PvReserved        uintptr
	DwReserved        uint32
	FlagsEx           uint32
}

type shellExecuteInfo struct {
	CbSize       uint32
	FMask        uint32
	Hwnd         uintptr
	LpVerb       *uint16
	LpFile       *uint16
	LpParameters *uint16
	LpDirectory  *uint16
	NShow        int32
	HInstApp     uintptr
	LpIDList     uintptr
	LpClass      *uint16
	HkeyClass    uintptr
	DwHotKey     uint32
	HIcon        uintptr
	HProcess     uintptr
}

type internetPerConnOption struct {
	DwOption uint32
	Value    uintptr
}

type internetPerConnOptionList struct {
	DwSize        uint32
	PszConnection *uint16
	DwOptionCount uint32
	DwOptionError uint32
	POptions      *internetPerConnOption
}

type spDevInfoData struct {
	CbSize    uint32
	ClassGUID windows.GUID
	DevInst   uint32
	Reserved  uintptr
}

type spClassInstallHeader struct {
	CbSize          uint32
	InstallFunction uint32
}

type spRemoveDeviceParams struct {
	ClassInstallHeader spClassInstallHeader
	Scope              uint32
	HwProfile          uint32
}

type initCommonControlsEx struct {
	Size uint32
	ICC  uint32
}

// --- DLL procs -----------------------------------------------------------

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")
	comdlg32 = windows.NewLazySystemDLL("comdlg32.dll")
	setupapi = windows.NewLazySystemDLL("setupapi.dll")
	cfgmgr32 = windows.NewLazySystemDLL("cfgmgr32.dll")
	comctl32 = windows.NewLazySystemDLL("comctl32.dll")

	pRegisterClassExW      = user32.NewProc("RegisterClassExW")
	pCreateWindowExW       = user32.NewProc("CreateWindowExW")
	pDestroyWindow         = user32.NewProc("DestroyWindow")
	pDefWindowProcW        = user32.NewProc("DefWindowProcW")
	pShowWindow            = user32.NewProc("ShowWindow")
	pIsWindow              = user32.NewProc("IsWindow")
	pSetFocus              = user32.NewProc("SetFocus")
	pGetFocus              = user32.NewProc("GetFocus")
	pSetForegroundWindow   = user32.NewProc("SetForegroundWindow")
	pBringWindowToTop      = user32.NewProc("BringWindowToTop")
	pSetWindowPos          = user32.NewProc("SetWindowPos")
	pGetWindowTextW        = user32.NewProc("GetWindowTextW")
	pGetWindowTextLengthW  = user32.NewProc("GetWindowTextLengthW")
	pSetWindowTextW        = user32.NewProc("SetWindowTextW")
	pSendMessageW          = user32.NewProc("SendMessageW")
	pPostMessageW          = user32.NewProc("PostMessageW")
	pGetMessageW           = user32.NewProc("GetMessageW")
	pTranslateMessage      = user32.NewProc("TranslateMessage")
	pDispatchMessageW      = user32.NewProc("DispatchMessageW")
	pPostQuitMessage       = user32.NewProc("PostQuitMessage")
	pIsDialogMessageW      = user32.NewProc("IsDialogMessageW")
	pEndMenu               = user32.NewProc("EndMenu")
	pGetCursorPos          = user32.NewProc("GetCursorPos")
	pTrackPopupMenuEx      = user32.NewProc("TrackPopupMenuEx")
	pCreatePopupMenu       = user32.NewProc("CreatePopupMenu")
	pAppendMenuW           = user32.NewProc("AppendMenuW")
	pDestroyMenu           = user32.NewProc("DestroyMenu")
	pChangeMessageFilter   = user32.NewProc("ChangeWindowMessageFilterEx")
	pRegisterWindowMsg     = user32.NewProc("RegisterWindowMessageW")
	pNotifyWinEvent        = user32.NewProc("NotifyWinEvent")
	pAdjustWindowRectEx    = user32.NewProc("AdjustWindowRectEx")
	pSystemParametersInfoW = user32.NewProc("SystemParametersInfoW")
	pGetSystemMetrics      = user32.NewProc("GetSystemMetrics")
	pLoadIconW             = user32.NewProc("LoadIconW")
	pLoadCursorW           = user32.NewProc("LoadCursorW")
	pDestroyIcon           = user32.NewProc("DestroyIcon")
	pCreateIconFromResEx   = user32.NewProc("CreateIconFromResourceEx")
	pSetProcessDPIAware    = user32.NewProc("SetProcessDPIAware")
	pSetProcessDpiAwareCtx = user32.NewProc("SetProcessDpiAwarenessContext")
	pGetDpiForSystem       = user32.NewProc("GetDpiForSystem")

	pCreateFontW  = gdi32.NewProc("CreateFontW")
	pDeleteObject = gdi32.NewProc("DeleteObject")

	pGetModuleHandleW      = kernel32.NewProc("GetModuleHandleW")
	pAttachConsole         = kernel32.NewProc("AttachConsole")
	pFreeConsole           = kernel32.NewProc("FreeConsole")
	pSetConsoleCtrlHandler = kernel32.NewProc("SetConsoleCtrlHandler")

	pShellNotifyIconW = shell32.NewProc("Shell_NotifyIconW")
	pShellExecuteExW  = shell32.NewProc("ShellExecuteExW")

	pGetOpenFileNameW = comdlg32.NewProc("GetOpenFileNameW")

	pSetupDiGetClassDevsW          = setupapi.NewProc("SetupDiGetClassDevsW")
	pSetupDiEnumDeviceInfo         = setupapi.NewProc("SetupDiEnumDeviceInfo")
	pSetupDiDestroyDeviceInfoList  = setupapi.NewProc("SetupDiDestroyDeviceInfoList")
	pSetupDiGetDeviceInstanceIdW   = setupapi.NewProc("SetupDiGetDeviceInstanceIdW")
	pSetupDiSetClassInstallParamsW = setupapi.NewProc("SetupDiSetClassInstallParamsW")
	pSetupDiCallClassInstaller     = setupapi.NewProc("SetupDiCallClassInstaller")

	pCMGetDevNodeStatus = cfgmgr32.NewProc("CM_Get_DevNode_Status")

	pInitCommonControlsEx = comctl32.NewProc("InitCommonControlsEx")
)

var propIDAccName = windows.GUID{
	Data1: 0x608D3DF8,
	Data2: 0x8128,
	Data3: 0x4AA7,
	Data4: [8]byte{0xA4, 0x28, 0xF5, 0x5E, 0x49, 0x26, 0x72, 0x91},
}

// --- Small helpers -------------------------------------------------------

func utf16Ptr(s string) *uint16 {
	p, err := windows.UTF16PtrFromString(s)
	if err != nil {
		return nil
	}
	return p
}

func copyToUTF16Array(dst []uint16, s string) {
	n := len(dst) - 1
	u := windows.StringToUTF16(s)
	if len(u) < n {
		n = len(u)
	}
	copy(dst[:n], u[:n])
	dst[n] = 0
}

// utf16FilterString builds a double-NUL-terminated filter list for the common
// dialogs; windows.StringToUTF16 panics on embedded NULs.
func utf16FilterString(s string) []uint16 {
	parts := strings.Split(s, "\x00")
	out := make([]uint16, 0, len(s)+1)
	for _, part := range parts {
		out = append(out, windows.StringToUTF16(part)...)
	}
	return out
}

func loword(v uintptr) uint32 { return uint32(v & 0xFFFF) }
func hiword(v uintptr) uint32 { return uint32((v >> 16) & 0xFFFF) }

func initCommonControls() {
	var icc initCommonControlsEx
	icc.Size = uint32(unsafe.Sizeof(icc))
	icc.ICC = icccStandardClasses
	pInitCommonControlsEx.Call(uintptr(unsafe.Pointer(&icc)))
}

func getModuleHandle() uintptr {
	h, _, _ := pGetModuleHandleW.Call(0)
	return h
}

func postMessage(hwnd uintptr, msg uint32, wparam, lparam uintptr) bool {
	r, _, _ := pPostMessageW.Call(hwnd, uintptr(msg), wparam, lparam)
	return r != 0
}

func sendMessage(hwnd uintptr, msg uint32, wparam, lparam uintptr) uintptr {
	r, _, _ := pSendMessageW.Call(hwnd, uintptr(msg), wparam, lparam)
	return r
}

func setWindowText(hwnd uintptr, text string) {
	pSetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(utf16Ptr(text))))
}

func getWindowText(hwnd uintptr) string {
	n, _, _ := pGetWindowTextLengthW.Call(hwnd)
	buf := make([]uint16, int(n)+2)
	if len(buf) < 2 {
		buf = make([]uint16, 2)
	}
	pGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return windows.UTF16ToString(buf)
}

func showWindow(hwnd uintptr, cmd int) {
	pShowWindow.Call(hwnd, uintptr(cmd))
}

func messageBox(text, caption string, flags uint32) int32 {
	r, _ := windows.MessageBox(0, utf16Ptr(text), utf16Ptr(caption), flags)
	return r
}

func messageBoxFor(owner uintptr, text, caption string, flags uint32) int32 {
	r, _ := windows.MessageBox(windows.HWND(owner), utf16Ptr(text), utf16Ptr(caption), flags)
	return r
}

// --- Window/control helpers ---------------------------------------------

const (
	dmGetDefID        = 0x0400
	dcHasDefID        = 0x534B
	mbOK              = 0x00000000
	mbIconError       = 0x00000010
	swpNoMove         = 0x0002
	smCXIcon          = 11
	smCYIcon          = 12
	colorBtnFace      = 15
	colorWindowText   = 8
	wmCtlColorStatic  = 0x0138
	bkmodeTransparent = 1
)

var (
	ole32 = windows.NewLazySystemDLL("ole32.dll")

	pGetDlgItem       = user32.NewProc("GetDlgItem")
	pIsChild          = user32.NewProc("IsChild")
	pGetSysColor      = user32.NewProc("GetSysColor")
	pGetSysColorBrush = user32.NewProc("GetSysColorBrush")
	pGetTickCount     = kernel32.NewProc("GetTickCount")
	pOleInitialize    = ole32.NewProc("OleInitialize")
	pOleUninitialize  = ole32.NewProc("OleUninitialize")
	pCoCreateInstance = ole32.NewProc("CoCreateInstance")
	pCoTaskMemFree    = ole32.NewProc("CoTaskMemFree")
	pSetBkMode        = gdi32.NewProc("SetBkMode")
	pSetTextColor     = gdi32.NewProc("SetTextColor")
)

func createWindowEx(exStyle uintptr, className, title string, style uintptr,
	x, y, w, h int32, parent, menu, instance, param uintptr) uintptr {
	r, _, _ := pCreateWindowExW.Call(
		exStyle,
		uintptr(unsafe.Pointer(utf16Ptr(className))),
		uintptr(unsafe.Pointer(utf16Ptr(title))),
		style,
		uintptr(x), uintptr(y), uintptr(w), uintptr(h),
		parent, menu, instance, param)
	return r
}

func getDlgItem(hwnd uintptr, id int) uintptr {
	r, _, _ := pGetDlgItem.Call(hwnd, uintptr(id))
	return r
}

func isChild(parent, child uintptr) bool {
	r, _, _ := pIsChild.Call(parent, child)
	return r != 0
}

func getTickCount() uint32 {
	r, _, _ := pGetTickCount.Call()
	return uint32(r)
}

func setWindowFont(hwnd, font uintptr) {
	sendMessage(hwnd, wmSetFont, font, 1)
}

func setFocus(hwnd uintptr) {
	pSetFocus.Call(hwnd)
}

func getFocus() uintptr {
	r, _, _ := pGetFocus.Call()
	return r
}

func adjustWindowRectEx(rect *winRect, style, exStyle uintptr, menu bool) {
	v := uintptr(0)
	if menu {
		v = 1
	}
	pAdjustWindowRectEx.Call(uintptr(unsafe.Pointer(rect)), style, v, exStyle)
}

func setWindowPos(hwnd, after uintptr, x, y, cx, cy int32, flags uintptr) {
	pSetWindowPos.Call(hwnd, after, uintptr(x), uintptr(y), uintptr(cx), uintptr(cy), flags)
}

func appendMenu(hmenu uintptr, flags uintptr, id uintptr, text string) {
	pAppendMenuW.Call(hmenu, flags, id, uintptr(unsafe.Pointer(utf16Ptr(text))))
}

func appendSubMenu(hmenu, submenu uintptr, text string) {
	pAppendMenuW.Call(hmenu, mfPopup|mfString, submenu, uintptr(unsafe.Pointer(utf16Ptr(text))))
}

func getCursorPos() (winPoint, bool) {
	var pt winPoint
	r, _, _ := pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	return pt, r != 0
}

const (
	clsctxInprocServer = 0x1
	vtblSetHwndPropStr = 7
)

var (
	clsidAccPropServices = windows.GUID{
		Data1: 0xB5F8350B, Data2: 0x0548, Data3: 0x48B1,
		Data4: [8]byte{0xA6, 0xEE, 0x88, 0xBD, 0x00, 0xB4, 0xA5, 0xE7},
	}
	iidIAccPropServices = windows.GUID{
		Data1: 0x6E26E776, Data2: 0x04F0, Data3: 0x495D,
		Data4: [8]byte{0x80, 0xE4, 0x33, 0x30, 0x35, 0x2E, 0x31, 0x69},
	}

	accPropMu      sync.Mutex
	accPropCreated bool
	accPropSvc     unsafe.Pointer
)

func accPropServices() unsafe.Pointer {
	accPropMu.Lock()
	defer accPropMu.Unlock()
	if accPropCreated {
		return accPropSvc
	}
	accPropCreated = true

	var p unsafe.Pointer
	hr, _, _ := pCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidAccPropServices)),
		0,
		clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidIAccPropServices)),
		uintptr(unsafe.Pointer(&p)))
	if int32(hr) < 0 {
		return nil
	}
	accPropSvc = p
	return p
}

func setAccessibleName(hwnd uintptr, name string) {
	if hwnd == 0 || name == "" {
		return
	}
	svc := accPropServices()
	if svc == nil {
		return
	}
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return
	}

	obj := (*[1]unsafe.Pointer)(svc)
	vtbl := (*[16]uintptr)(obj[0])
	fn := vtbl[vtblSetHwndPropStr]
	if fn == 0 {
		return
	}
	syscall.SyscallN(fn, uintptr(svc), hwnd, uintptr(objIDClient), uintptr(childIDSelf),
		uintptr(unsafe.Pointer(&propIDAccName)), uintptr(unsafe.Pointer(p)))
	pNotifyWinEvent.Call(eventObjectNameChg, hwnd, objIDClient, childIDSelf)
}

func notifyWinEvent(event uint32, hwnd uintptr) {
	pNotifyWinEvent.Call(uintptr(event), hwnd, objIDClient, childIDSelf)
}
