//go:build linux

package service

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"sync"
)

type clusterProcessMetrics struct {
	CPUPercent       float64
	RSSBytes         int64
	MemoryLimitBytes int64
	FDOpen           int64
	FDLimit          int64
}

type clusterProcessMetricsSampler struct {
	mu             sync.Mutex
	lastProcessCPU uint64
	lastSystemCPU  uint64
}

func (s *clusterProcessMetricsSampler) Sample() clusterProcessMetrics {
	metrics := clusterProcessMetrics{
		RSSBytes:         linuxProcessRSS(),
		MemoryLimitBytes: linuxMemoryTotal(),
		FDOpen:           linuxFDOpen(),
		FDLimit:          linuxFDLimit(),
	}
	processCPU, processOK := linuxProcessCPU()
	systemCPU, systemOK := linuxSystemCPU()
	s.mu.Lock()
	if processOK && systemOK && s.lastProcessCPU > 0 && systemCPU > s.lastSystemCPU && processCPU >= s.lastProcessCPU {
		processDelta := processCPU - s.lastProcessCPU
		systemDelta := systemCPU - s.lastSystemCPU
		metrics.CPUPercent = float64(processDelta) / float64(systemDelta) * float64(runtimeCPUCount()) * 100
	}
	if processOK {
		s.lastProcessCPU = processCPU
	}
	if systemOK {
		s.lastSystemCPU = systemCPU
	}
	s.mu.Unlock()
	return metrics
}

func linuxProcessRSS() int64 {
	content, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(content))
	if len(fields) < 2 {
		return 0
	}
	pages, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil || pages < 0 {
		return 0
	}
	return pages * int64(os.Getpagesize())
}

func linuxMemoryTotal() int64 {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer func() { _ = file.Close() }()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && fields[0] == "MemTotal:" {
			kib, parseErr := strconv.ParseInt(fields[1], 10, 64)
			if parseErr == nil && kib >= 0 {
				return kib * 1024
			}
			return 0
		}
	}
	return 0
}

func linuxFDOpen() int64 {
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return 0
	}
	return int64(len(entries))
}

func linuxFDLimit() int64 {
	file, err := os.Open("/proc/self/limits")
	if err != nil {
		return 0
	}
	defer func() { _ = file.Close() }()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "Max open files") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			return 0
		}
		value, parseErr := strconv.ParseInt(fields[3], 10, 64)
		if parseErr == nil && value >= 0 {
			return value
		}
		return 0
	}
	return 0
}

func linuxProcessCPU() (uint64, bool) {
	content, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return 0, false
	}
	endName := strings.LastIndexByte(string(content), ')')
	if endName < 0 || endName+2 >= len(content) {
		return 0, false
	}
	fields := strings.Fields(string(content[endName+2:]))
	// Fields after comm start at the process state (field 3). utime/stime are
	// therefore indexes 11 and 12 in this slice.
	if len(fields) <= 12 {
		return 0, false
	}
	user, err := strconv.ParseUint(fields[11], 10, 64)
	if err != nil {
		return 0, false
	}
	system, err := strconv.ParseUint(fields[12], 10, 64)
	if err != nil {
		return 0, false
	}
	return user + system, true
}

func linuxSystemCPU() (uint64, bool) {
	file, err := os.Open("/proc/stat")
	if err != nil {
		return 0, false
	}
	defer func() { _ = file.Close() }()
	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		return 0, false
	}
	fields := strings.Fields(scanner.Text())
	if len(fields) < 2 || fields[0] != "cpu" {
		return 0, false
	}
	var total uint64
	for _, field := range fields[1:] {
		value, parseErr := strconv.ParseUint(field, 10, 64)
		if parseErr != nil {
			return 0, false
		}
		total += value
	}
	return total, true
}

func runtimeCPUCount() int {
	content, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 1
	}
	count := 0
	for _, line := range strings.Split(string(content), "\n") {
		if len(line) > 3 && strings.HasPrefix(line, "cpu") {
			if _, err := strconv.Atoi(strings.Fields(line)[0][3:]); err == nil {
				count++
			}
		}
	}
	if count < 1 {
		return 1
	}
	return count
}
