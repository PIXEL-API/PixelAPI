//go:build !linux

package service

import (
	"runtime"
)

type clusterProcessMetrics struct {
	CPUPercent       float64
	RSSBytes         int64
	MemoryLimitBytes int64
	FDOpen           int64
	FDLimit          int64
}

type clusterProcessMetricsSampler struct{}

func (clusterProcessMetricsSampler) Sample() clusterProcessMetrics {
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	return clusterProcessMetrics{
		RSSBytes: int64(memory.Sys),
	}
}
