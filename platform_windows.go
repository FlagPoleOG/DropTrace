//go:build windows

package main

import (
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

// Load system DLLs by full path so a planted DLL beside the .exe can't be used.
func systemDLL(name string) *syscall.LazyDLL {
	buf := make([]uint16, 260)
	k32 := syscall.NewLazyDLL("kernel32.dll")
	n, _, _ := k32.NewProc("GetSystemDirectoryW").Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.NewLazyDLL(syscall.UTF16ToString(buf[:n]) + `\` + name)
}

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	iphlpapi = systemDLL("iphlpapi.dll")
	dnsapi   = systemDLL("dnsapi.dll")
	user32   = systemDLL("user32.dll")
	shell32  = systemDLL("shell32.dll")
	gdi32    = systemDLL("gdi32.dll")
	gdiplus  = systemDLL("gdiplus.dll")
	advapi32 = systemDLL("advapi32.dll")
	comctl32 = systemDLL("comctl32.dll")
	comdlg32 = systemDLL("comdlg32.dll")
	uxtheme  = systemDLL("uxtheme.dll")
	dwmapi   = systemDLL("dwmapi.dll")

	procCreateMutexW     = kernel32.NewProc("CreateMutexW")
	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")

	procGetBestRoute    = iphlpapi.NewProc("GetBestRoute")
	procGetIfEntry2     = iphlpapi.NewProc("GetIfEntry2")
	procIcmpCreateFile  = iphlpapi.NewProc("IcmpCreateFile")
	procIcmpCloseHandle = iphlpapi.NewProc("IcmpCloseHandle")
	procIcmpSendEcho    = iphlpapi.NewProc("IcmpSendEcho")

	procDnsQueryW         = dnsapi.NewProc("DnsQuery_W")
	procDnsRecordListFree = dnsapi.NewProc("DnsRecordListFree")

	procRegSetValueExW  = advapi32.NewProc("RegSetValueExW")
	procRegDeleteValueW = advapi32.NewProc("RegDeleteValueW")
	procMessageBoxW     = user32.NewProc("MessageBoxW")
	procShellExecuteW   = shell32.NewProc("ShellExecuteW")
)

// oldDataDir is where versions named Dropline kept their history.
func oldDataDir() string {
	if os.Getenv("DROPTRACE_DATA") != "" {
		return ""
	}
	if d := os.Getenv("LOCALAPPDATA"); d != "" {
		return filepath.Join(d, "Dropline")
	}
	return ""
}

// olderVersionRunning looks for a copy still running under the old name, so two
// copies can't log to the same history while it's being moved.
func olderVersionRunning() bool {
	for _, class := range []string{"DroplineMainWindow", "DroplineTrayWindow"} {
		name, _ := syscall.UTF16PtrFromString(class)
		if h, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(name)), 0); h != 0 {
			return true
		}
	}
	return false
}

var (
	procGetCurrentPackageFullName = kernel32.NewProc("GetCurrentPackageFullName")
	packagedOnce                  sync.Once
	packaged                      bool
)

// isPackaged reports whether this copy was installed from an MSIX package, such as
// through the Microsoft Store.
func isPackaged() bool {
	packagedOnce.Do(func() {
		if packagedTestMode {
			packaged = true
			return
		}
		var length uint32
		r, _, _ := procGetCurrentPackageFullName.Call(uintptr(unsafe.Pointer(&length)), 0)
		packaged = r != 15700 // APPMODEL_ERROR_NO_PACKAGE
	})
	return packaged
}

// startedBySignIn reports whether Windows launched the package's startup copy, which
// exists because a package's startup task can't pass --background.
func startedBySignIn() bool {
	exe, err := os.Executable()
	return err == nil && strings.EqualFold(filepath.Base(exe), startupExeName)
}

const startupExeName = "DropTraceStartup.exe"

func dataDir() string {
	if d := os.Getenv("DROPTRACE_DATA"); d != "" {
		return d
	}
	if d := os.Getenv("LOCALAPPDATA"); d != "" {
		return filepath.Join(d, "DropTrace")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "DropTrace")
}

var instanceMutex uintptr // held for the life of the process

func acquireSingleInstance() bool {
	name, _ := syscall.UTF16PtrFromString(`Local\DropTraceSingleInstance`)
	h, _, err := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(name)))
	instanceMutex = h
	return !errors.Is(err, syscall.ERROR_ALREADY_EXISTS)
}

func fatal(msg string) {
	text, _ := syscall.UTF16PtrFromString(msg)
	title, _ := syscall.UTF16PtrFromString("DropTrace")
	_, _, _ = procMessageBoxW.Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), 0x10)
	os.Exit(1)
}

// showExistingInstance brings up the window of the copy that's already running.
func showExistingInstance() {
	class, _ := syscall.UTF16PtrFromString(windowClassName)
	hwnd, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(class)), 0)
	if hwnd == 0 {
		fatal("An older version of DropTrace is still running. Right-click its tray icon, choose Exit, and then open this version again.")
	}
	_, _, _ = procPostMessageW.Call(hwnd, wmShowApp, 0, 0)
}

func shellOpen(url string) {
	verb, _ := syscall.UTF16PtrFromString("open")
	target, _ := syscall.UTF16PtrFromString(url)
	_, _, _ = procShellExecuteW.Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(target)), 0, 0, 1)
}

// ---- Start with Windows ----

const (
	runKeyPath  = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValue    = "DropTrace"
	keySetValue = 0x0002
	keyQuery    = 0x0001
	regSZ       = 1
)

func openRunKey(access uint32) (syscall.Handle, error) {
	var k syscall.Handle
	path, _ := syscall.UTF16PtrFromString(runKeyPath)
	err := syscall.RegOpenKeyEx(syscall.HKEY_CURRENT_USER, path, 0, access, &k)
	return k, err
}

func autostartEnabled() bool {
	if isPackaged() {
		return false
	}
	k, err := openRunKey(keyQuery)
	if err != nil {
		return false
	}
	defer syscall.RegCloseKey(k)
	name, _ := syscall.UTF16PtrFromString(runValue)
	var size uint32
	return syscall.RegQueryValueEx(k, name, nil, nil, nil, &size) == nil
}

func setAutostart(on bool) error {
	k, err := openRunKey(keySetValue)
	if err != nil {
		return err
	}
	defer syscall.RegCloseKey(k)
	name, _ := syscall.UTF16PtrFromString(runValue)
	if !on {
		r, _, _ := procRegDeleteValueW.Call(uintptr(k), uintptr(unsafe.Pointer(name)))
		if r != 0 && syscall.Errno(r) != syscall.ERROR_FILE_NOT_FOUND {
			return syscall.Errno(r)
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	value, _ := syscall.UTF16FromString(`"` + exe + `" --background`)
	r, _, _ := procRegSetValueExW.Call(uintptr(k), uintptr(unsafe.Pointer(name)), 0, regSZ,
		uintptr(unsafe.Pointer(&value[0])), uintptr(len(value)*2))
	if r != 0 {
		return syscall.Errno(r)
	}
	return nil
}

// refreshAutostartPath keeps startup working if the .exe was moved.
func refreshAutostartPath() {
	if isPackaged() { // Windows manages a package's startup task
		return
	}
	// Keep the setting from the old name, then remove the old startup entry.
	if k, err := openRunKey(keyQuery | keySetValue); err == nil {
		old, _ := syscall.UTF16PtrFromString("Dropline")
		var size uint32
		hadOld := syscall.RegQueryValueEx(k, old, nil, nil, nil, &size) == nil
		if hadOld {
			_, _, _ = procRegDeleteValueW.Call(uintptr(k), uintptr(unsafe.Pointer(old)))
		}
		_ = syscall.RegCloseKey(k)
		if hadOld {
			if err := setAutostart(true); err != nil {
				log.Printf("couldn't move startup entry: %v", err)
			}
			return
		}
	}
	if autostartEnabled() {
		if err := setAutostart(true); err != nil {
			log.Printf("couldn't refresh startup entry: %v", err)
		}
	}
}
