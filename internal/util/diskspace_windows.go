//go:build windows

package util

import (
	"syscall"
	"unsafe"
)

type DiskSpaceInfo struct {
	AvailGB float64
	TotalGB float64
	UsedGB  float64
}

func GetDiskSpace(path string) (DiskSpaceInfo, error) {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getDiskFreeSpaceEx := kernel32.NewProc("GetDiskFreeSpaceExW")

	var freeBytesAvailable, totalBytes, totalFreeBytes uint64
	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return DiskSpaceInfo{}, err
	}

	ret, _, err := getDiskFreeSpaceEx.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		uintptr(unsafe.Pointer(&freeBytesAvailable)),
		uintptr(unsafe.Pointer(&totalBytes)),
		uintptr(unsafe.Pointer(&totalFreeBytes)),
	)
	if ret == 0 {
		return DiskSpaceInfo{}, err
	}

	availGB := float64(freeBytesAvailable) / (1024 * 1024 * 1024)
	totalGB := float64(totalBytes) / (1024 * 1024 * 1024)
	return DiskSpaceInfo{
		AvailGB: availGB,
		TotalGB: totalGB,
		UsedGB:  totalGB - availGB,
	}, nil
}
