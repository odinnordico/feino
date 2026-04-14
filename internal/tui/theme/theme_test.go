package theme

import "testing"

func TestDarkTheme_NonZeroStyles(t *testing.T) {
	th := DarkTheme()
	if th.Primary == "" {
		t.Error("DarkTheme: Primary colour must not be empty")
	}
	if th.HeaderStyle.GetBackground() == nil {
		t.Error("DarkTheme: HeaderStyle background must be set")
	}
}

func TestLightTheme_NonZeroStyles(t *testing.T) {
	th := LightTheme()
	if th.Primary == "" {
		t.Error("LightTheme: Primary colour must not be empty")
	}
}

func TestFromConfig_Dispatch(t *testing.T) {
	dark := FromConfig("dark")
	light := FromConfig("light")
	neo := FromConfig("neo")
	auto := FromConfig("auto")

	if dark.Background == light.Background {
		t.Error("dark and light themes should have different backgrounds")
	}
	// Unknown/empty values fall back to NeoTheme.
	if FromConfig("").Background != neo.Background {
		t.Error("FromConfig(\"\") should fall back to NeoTheme")
	}
	// "auto" is still a valid distinct value.
	if auto.Background == neo.Background {
		t.Error("FromConfig(\"auto\") and NeoTheme should have different backgrounds")
	}
}

func TestNeoTheme_NonZeroStyles(t *testing.T) {
	th := NeoTheme()
	if th.Primary == "" {
		t.Error("NeoTheme: Primary colour must not be empty")
	}
	if th.HeaderStyle.GetBackground() == nil {
		t.Error("NeoTheme: HeaderStyle background must be set")
	}
	if th.SpinnerStyle.GetForeground() == nil {
		t.Error("NeoTheme: SpinnerStyle foreground must be set")
	}
}

func TestFromConfig_Neo(t *testing.T) {
	neo := NeoTheme()
	if FromConfig("neo").Background != neo.Background {
		t.Error("FromConfig(\"neo\") should return NeoTheme")
	}
	if FromConfig("unknown").Background != neo.Background {
		t.Error("FromConfig(\"unknown\") should fall back to NeoTheme")
	}
	if FromConfig("").Background != neo.Background {
		t.Error("FromConfig(\"\") should fall back to NeoTheme")
	}
}

func TestGlamourStyle(t *testing.T) {
	if NeoTheme().GlamourStyle() != "dark" {
		t.Error("NeoTheme.GlamourStyle() should return \"dark\"")
	}
	if DarkTheme().GlamourStyle() != "dark" {
		t.Error("DarkTheme.GlamourStyle() should return \"dark\"")
	}
	if LightTheme().GlamourStyle() != "light" {
		t.Error("LightTheme.GlamourStyle() should return \"light\"")
	}
}
