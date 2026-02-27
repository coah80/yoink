//go:build !windows

package util

import (
	"syscall"
)

type DiskSpaceInfo struct {
	AvailGB float64
	TotalGB float64
	UsedGB  float64
}

func GetDiskSpace(path string) (DiskSpaceInfo, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return DiskSpaceInfo{}, err
	}
	availGB := float64(stat.Bavail*uint64(stat.Bsize)) / (1024 * 1024 * 1024)
	totalGB := float64(stat.Blocks*uint64(stat.Bsize)) / (1024 * 1024 * 1024)
	return DiskSpaceInfo{
		AvailGB: availGB,
		TotalGB: totalGB,
		UsedGB:  totalGB - availGB,
	}, nil
}
