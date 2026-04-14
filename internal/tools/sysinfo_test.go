package tools

import (
	"encoding/json"
	"strings"
	"testing"
)

// ── tool smoke test ───────────────────────────────────────────────────────────

func TestSysInfoTool_ReturnsValidJSON(t *testing.T) {
	tool := newSysInfoTool(nil)
	res := tool.Run(map[string]any{})

	if res.GetError() != nil {
		t.Fatalf("unexpected error: %v", res.GetError())
	}

	raw, ok := res.GetContent().(string)
	if !ok || raw == "" {
		t.Fatal("expected non-empty string result")
	}

	var out sysInfoResult
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("result is not valid JSON: %v\ncontent: %s", err, raw)
	}
}

func TestSysInfoTool_HostPopulated(t *testing.T) {
	tool := newSysInfoTool(nil)
	res := tool.Run(map[string]any{})

	raw, _ := res.GetContent().(string)
	var out sysInfoResult
	_ = json.Unmarshal([]byte(raw), &out)

	if out.Host.Hostname == "" {
		t.Error("host.hostname should not be empty")
	}
	if out.Host.OS == "" {
		t.Error("host.os should not be empty")
	}
}

func TestSysInfoTool_CPUPopulated(t *testing.T) {
	tool := newSysInfoTool(nil)
	res := tool.Run(map[string]any{})

	raw, _ := res.GetContent().(string)
	var out sysInfoResult
	_ = json.Unmarshal([]byte(raw), &out)

	if out.CPU.LogicalCount <= 0 {
		t.Errorf("cpu.logical_count should be > 0, got %d", out.CPU.LogicalCount)
	}
}

func TestSysInfoTool_MemoryPopulated(t *testing.T) {
	tool := newSysInfoTool(nil)
	res := tool.Run(map[string]any{})

	raw, _ := res.GetContent().(string)
	var out sysInfoResult
	_ = json.Unmarshal([]byte(raw), &out)

	if out.Memory.TotalBytes == 0 {
		t.Error("memory.total_bytes should be > 0")
	}
	if out.Memory.TotalHuman == "" {
		t.Error("memory.total_human should not be empty")
	}
	if out.Memory.UsedPercent < 0 || out.Memory.UsedPercent > 100 {
		t.Errorf("memory.used_percent out of range: %f", out.Memory.UsedPercent)
	}
}

func TestSysInfoTool_DisksPopulated(t *testing.T) {
	tool := newSysInfoTool(nil)
	res := tool.Run(map[string]any{})

	raw, _ := res.GetContent().(string)
	var out sysInfoResult
	_ = json.Unmarshal([]byte(raw), &out)

	if len(out.Disks) == 0 {
		t.Error("disks should contain at least one mount point")
	}
	for _, d := range out.Disks {
		if d.Mountpoint == "" {
			t.Error("disk entry has empty mountpoint")
		}
		if d.TotalHuman == "" {
			t.Errorf("disk %s: total_human should not be empty", d.Mountpoint)
		}
		if d.UsedPercent < 0 || d.UsedPercent > 100 {
			t.Errorf("disk %s: used_percent out of range: %f", d.Mountpoint, d.UsedPercent)
		}
	}
}

func TestSysInfoTool_PermissionLevel(t *testing.T) {
	tool := newSysInfoTool(nil)
	c, ok := tool.(Classified)
	if !ok {
		t.Fatal("sys_info tool does not implement Classified")
	}
	if c.PermissionLevel() != PermLevelRead {
		t.Errorf("want PermLevelRead (%d), got %d", PermLevelRead, c.PermissionLevel())
	}
}

// ── formatBytes ───────────────────────────────────────────────────────────────

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		input uint64
		want  string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1.0 KiB"},
		{1536, "1.5 KiB"},
		{1024 * 1024, "1.0 MiB"},
		{1024 * 1024 * 1024, "1.0 GiB"},
		{uint64(8.5 * 1024 * 1024 * 1024), "8.5 GiB"},
		{1024 * 1024 * 1024 * 1024, "1.0 TiB"},
	}
	for _, tc := range tests {
		got := formatBytes(tc.input)
		if got != tc.want {
			t.Errorf("formatBytes(%d): want %q, got %q", tc.input, tc.want, got)
		}
	}
}

// ── formatUptime ──────────────────────────────────────────────────────────────

func TestFormatUptime(t *testing.T) {
	tests := []struct {
		secs uint64
		want string
	}{
		{0, "0m"},
		{59, "0m"},
		{60, "1m"},
		{3600, "1h 0m"},
		{3661, "1h 1m"},
		{86400, "1d 0h 0m"},
		{90061, "1d 1h 1m"},
	}
	for _, tc := range tests {
		got := formatUptime(tc.secs)
		if got != tc.want {
			t.Errorf("formatUptime(%d): want %q, got %q", tc.secs, tc.want, got)
		}
	}
}

// ── roundFloat ────────────────────────────────────────────────────────────────

func TestRoundFloat(t *testing.T) {
	tests := []struct {
		f    float64
		prec int
		want float64
	}{
		{3.14159, 2, 3.14},
		{3.145, 2, 3.15},
		{100.0, 2, 100.0},
		{0.005, 2, 0.01},
		{1.0 / 3.0, 4, 0.3333},
	}
	for _, tc := range tests {
		got := roundFloat(tc.f, tc.prec)
		if got != tc.want {
			t.Errorf("roundFloat(%v, %d): want %v, got %v", tc.f, tc.prec, tc.want, got)
		}
	}
}

// ── NewNativeTools registration ───────────────────────────────────────────────

func TestNewNativeTools_IncludesSysInfo(t *testing.T) {
	tools := NewNativeTools(nil)
	for _, tool := range tools {
		if tool.GetName() == "sys_info" {
			return
		}
	}
	t.Error("sys_info not found in NewNativeTools output")
}

// ── human-readable output sanity check ───────────────────────────────────────

func TestSysInfoTool_UptimeHumanNonEmpty(t *testing.T) {
	tool := newSysInfoTool(nil)
	res := tool.Run(map[string]any{})

	raw, _ := res.GetContent().(string)
	if !strings.Contains(raw, "uptime_human") {
		t.Error("result should contain uptime_human field")
	}
}
