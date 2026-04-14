package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
)

const sysInfoTimeout = 10 * time.Second

// NewSysInfoTools returns the sys_info tool.
func NewSysInfoTools(logger *slog.Logger) []Tool {
	return []Tool{newSysInfoTool(logger)}
}

// sysInfoResult is the JSON payload returned by sys_info.
type sysInfoResult struct {
	Host   sysInfoHost   `json:"host"`
	CPU    sysInfoCPU    `json:"cpu"`
	Memory sysInfoMemory `json:"memory"`
	Disks  []sysInfoDisk `json:"disks"`
}

type sysInfoHost struct {
	Hostname             string `json:"hostname"`
	OS                   string `json:"os"`
	Platform             string `json:"platform"`
	PlatformVersion      string `json:"platform_version"`
	KernelVersion        string `json:"kernel_version"`
	KernelArch           string `json:"kernel_arch"`
	UptimeSeconds        uint64 `json:"uptime_seconds"`
	UptimeHuman          string `json:"uptime_human"`
	Procs                uint64 `json:"procs"`
	VirtualizationSystem string `json:"virtualization_system,omitempty"`
	VirtualizationRole   string `json:"virtualization_role,omitempty"`
}

type sysInfoCPU struct {
	LogicalCount  int      `json:"logical_count"`
	PhysicalCount int      `json:"physical_count"`
	ModelName     string   `json:"model_name"`
	Mhz           float64  `json:"mhz"`
	UsagePercent  float64  `json:"usage_percent"` // system-wide, averaged over 200 ms
}

type sysInfoMemory struct {
	TotalBytes     uint64  `json:"total_bytes"`
	TotalHuman     string  `json:"total_human"`
	UsedBytes      uint64  `json:"used_bytes"`
	UsedHuman      string  `json:"used_human"`
	AvailableBytes uint64  `json:"available_bytes"`
	AvailableHuman string  `json:"available_human"`
	UsedPercent    float64 `json:"used_percent"`
	SwapTotalBytes uint64  `json:"swap_total_bytes,omitempty"`
	SwapUsedBytes  uint64  `json:"swap_used_bytes,omitempty"`
	SwapUsedPercent float64 `json:"swap_used_percent,omitempty"`
}

type sysInfoDisk struct {
	Mountpoint  string  `json:"mountpoint"`
	Fstype      string  `json:"fstype"`
	TotalBytes  uint64  `json:"total_bytes"`
	TotalHuman  string  `json:"total_human"`
	UsedBytes   uint64  `json:"used_bytes"`
	UsedHuman   string  `json:"used_human"`
	FreeBytes   uint64  `json:"free_bytes"`
	FreeHuman   string  `json:"free_human"`
	UsedPercent float64 `json:"used_percent"`
}

func newSysInfoTool(logger *slog.Logger) Tool {
	return NewTool(
		"sys_info",
		"Return system information: hostname, OS, kernel, CPU model and current "+
			"utilisation, total/used/available RAM and swap, and per-mount-point "+
			"disk usage. All byte values are reported both as raw uint64 and as a "+
			"human-readable string (e.g. \"7.8 GiB\").",
		map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
		func(params map[string]any) ToolResult {
			ctx, cancel := context.WithTimeout(context.Background(), sysInfoTimeout)
			defer cancel()

			result, err := collectSysInfo(ctx)
			if err != nil {
				return NewToolResult(fmt.Sprintf("sys_info: %v", err), nil)
			}

			out, _ := json.MarshalIndent(result, "", "  ")
			return NewToolResult(string(out), nil)
		},
		WithPermissionLevel(PermLevelRead),
		WithLogger(logger),
	)
}

// collectSysInfo gathers all system metrics. Partial failures are tolerated:
// each subsection is collected independently and an error is only returned when
// every subsection fails.
func collectSysInfo(ctx context.Context) (sysInfoResult, error) {
	var res sysInfoResult
	var errs []string

	// ── host ─────────────────────────────────────────────────────────────────
	if info, err := host.InfoWithContext(ctx); err != nil {
		errs = append(errs, "host: "+err.Error())
	} else {
		res.Host = sysInfoHost{
			Hostname:             info.Hostname,
			OS:                   info.OS,
			Platform:             info.Platform,
			PlatformVersion:      info.PlatformVersion,
			KernelVersion:        info.KernelVersion,
			KernelArch:           info.KernelArch,
			UptimeSeconds:        info.Uptime,
			UptimeHuman:          formatUptime(info.Uptime),
			Procs:                info.Procs,
			VirtualizationSystem: info.VirtualizationSystem,
			VirtualizationRole:   info.VirtualizationRole,
		}
	}

	// ── CPU ───────────────────────────────────────────────────────────────────
	logical, _ := cpu.CountsWithContext(ctx, true)
	physical, _ := cpu.CountsWithContext(ctx, false)
	cpuInfo, err := cpu.InfoWithContext(ctx)
	modelName := ""
	mhz := 0.0
	if err == nil && len(cpuInfo) > 0 {
		modelName = cpuInfo[0].ModelName
		mhz = cpuInfo[0].Mhz
	}
	// Sample CPU usage over 200 ms — short enough to be responsive but long
	// enough for a meaningful sample. percpu=false returns a single value.
	usageCtx, usageCancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer usageCancel()
	usages, usageErr := cpu.PercentWithContext(usageCtx, 200*time.Millisecond, false)
	usage := 0.0
	if usageErr == nil && len(usages) > 0 {
		usage = usages[0]
	}
	if logical == 0 && physical == 0 {
		errs = append(errs, "cpu: no data")
	} else {
		res.CPU = sysInfoCPU{
			LogicalCount:  logical,
			PhysicalCount: physical,
			ModelName:     modelName,
			Mhz:           mhz,
			UsagePercent:  roundFloat(usage, 2),
		}
	}

	// ── memory ────────────────────────────────────────────────────────────────
	vm, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		errs = append(errs, "memory: "+err.Error())
	} else {
		res.Memory = sysInfoMemory{
			TotalBytes:     vm.Total,
			TotalHuman:     formatBytes(vm.Total),
			UsedBytes:      vm.Used,
			UsedHuman:      formatBytes(vm.Used),
			AvailableBytes: vm.Available,
			AvailableHuman: formatBytes(vm.Available),
			UsedPercent:    roundFloat(vm.UsedPercent, 2),
		}
		if sw, err := mem.SwapMemoryWithContext(ctx); err == nil {
			res.Memory.SwapTotalBytes = sw.Total
			res.Memory.SwapUsedBytes = sw.Used
			res.Memory.SwapUsedPercent = roundFloat(sw.UsedPercent, 2)
		}
	}

	// ── disks ─────────────────────────────────────────────────────────────────
	partitions, err := disk.PartitionsWithContext(ctx, false) // false = physical only
	if err != nil {
		errs = append(errs, "disk: "+err.Error())
	} else {
		for _, p := range partitions {
			usage, err := disk.UsageWithContext(ctx, p.Mountpoint)
			if err != nil {
				continue // skip unreadable mount points (e.g. /proc, /sys)
			}
			res.Disks = append(res.Disks, sysInfoDisk{
				Mountpoint:  p.Mountpoint,
				Fstype:      p.Fstype,
				TotalBytes:  usage.Total,
				TotalHuman:  formatBytes(usage.Total),
				UsedBytes:   usage.Used,
				UsedHuman:   formatBytes(usage.Used),
				FreeBytes:   usage.Free,
				FreeHuman:   formatBytes(usage.Free),
				UsedPercent: roundFloat(usage.UsedPercent, 2),
			})
		}
	}

	if len(errs) > 0 && res.Host.Hostname == "" && res.CPU.LogicalCount == 0 && res.Memory.TotalBytes == 0 {
		return sysInfoResult{}, errors.New(strings.Join(errs, "; "))
	}
	return res, nil
}

// ── formatting helpers ────────────────────────────────────────────────────────

// formatBytes converts a byte count to a human-readable IEC string.
func formatBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}

// formatUptime converts seconds into a compact "Xd Yh Zm" string.
func formatUptime(secs uint64) string {
	d := secs / 86400
	h := (secs % 86400) / 3600
	m := (secs % 3600) / 60
	switch {
	case d > 0:
		return fmt.Sprintf("%dd %dh %dm", d, h, m)
	case h > 0:
		return fmt.Sprintf("%dh %dm", h, m)
	default:
		return fmt.Sprintf("%dm", m)
	}
}

// roundFloat rounds f to prec decimal places.
func roundFloat(f float64, prec int) float64 {
	p := 1.0
	for range prec {
		p *= 10
	}
	return float64(int(f*p+0.5)) / p
}
