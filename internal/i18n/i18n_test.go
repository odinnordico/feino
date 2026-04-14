package i18n

import (
	"sync"
	"testing"
)

// reset clears the package-level localizer so each test starts clean.
func reset() {
	mu.Lock()
	loc = nil
	mu.Unlock()
}

// ── Before Init ───────────────────────────────────────────────────────────────

func TestT_BeforeInit_ReturnsID(t *testing.T) {
	reset()
	if got := T("some.key"); got != "some.key" {
		t.Errorf("T before Init: want %q, got %q", "some.key", got)
	}
}

func TestTf_BeforeInit_ReturnsID(t *testing.T) {
	reset()
	if got := Tf("some.key", nil); got != "some.key" {
		t.Errorf("Tf before Init: want %q, got %q", "some.key", got)
	}
}

func TestTp_BeforeInit_ReturnsID(t *testing.T) {
	reset()
	if got := Tp("some.key", 1, nil); got != "some.key" {
		t.Errorf("Tp before Init: want %q, got %q", "some.key", got)
	}
}

// ── After Init with English ───────────────────────────────────────────────────

func TestT_KnownKey(t *testing.T) {
	reset()
	Init("en")
	got := T("state_idle")
	if got != "idle" {
		t.Errorf("T(state_idle): want %q, got %q", "idle", got)
	}
}

func TestT_UnknownKey_ReturnsID(t *testing.T) {
	reset()
	Init("en")
	const id = "definitely.does.not.exist"
	if got := T(id); got != id {
		t.Errorf("T(unknown): want %q, got %q", id, got)
	}
}

func TestTf_WithTemplateData(t *testing.T) {
	reset()
	Init("en")
	got := Tf("lang_changed", map[string]any{"Lang": "Español"})
	want := "language changed to Español"
	if got != want {
		t.Errorf("Tf(lang_changed): want %q, got %q", want, got)
	}
}

func TestTp_Singular(t *testing.T) {
	reset()
	Init("en")
	got := Tp("plugins_reloaded", 1, map[string]any{"Count": 1})
	want := "plugins reloaded — 1 script plugin active"
	if got != want {
		t.Errorf("Tp(plugins_reloaded, 1): want %q, got %q", want, got)
	}
}

func TestTp_Plural(t *testing.T) {
	reset()
	Init("en")
	got := Tp("plugins_reloaded", 3, map[string]any{"Count": 3})
	want := "plugins reloaded — 3 script plugins active"
	if got != want {
		t.Errorf("Tp(plugins_reloaded, 3): want %q, got %q", want, got)
	}
}

// ── Init idempotency ──────────────────────────────────────────────────────────

func TestInit_Idempotent(t *testing.T) {
	reset()
	Init("en")
	Init("en")
	// Second call must not panic or corrupt state.
	if got := T("state_idle"); got != "idle" {
		t.Errorf("after double Init: want %q, got %q", "idle", got)
	}
}

func TestInit_Concurrent(t *testing.T) {
	reset()
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			Init("en")
			_ = T("state_idle")
		})
	}
	wg.Wait()
}

// ── detectLang ────────────────────────────────────────────────────────────────

func TestDetectLang_POSIXEncoding(t *testing.T) {
	t.Setenv("LANGUAGE", "")
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "es_MX.UTF-8")
	if got := detectLang(); got != "es-MX" {
		t.Errorf("POSIX locale: want %q, got %q", "es-MX", got)
	}
}

func TestDetectLang_ColonSeparatedLanguage(t *testing.T) {
	t.Setenv("LANGUAGE", "fr:de:en")
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "")
	// Should pick "fr", not "fr:de:en".
	if got := detectLang(); got != "fr" {
		t.Errorf("LANGUAGE colon list: want %q, got %q", "fr", got)
	}
}

func TestDetectLang_CLocale(t *testing.T) {
	t.Setenv("LANGUAGE", "")
	t.Setenv("LC_ALL", "C")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "")
	if got := detectLang(); got != "en" {
		t.Errorf("C locale: want %q, got %q", "en", got)
	}
}

func TestDetectLang_POSIXLocale(t *testing.T) {
	t.Setenv("LANGUAGE", "")
	t.Setenv("LC_ALL", "POSIX")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "")
	if got := detectLang(); got != "en" {
		t.Errorf("POSIX locale: want %q, got %q", "en", got)
	}
}

func TestDetectLang_AllUnset(t *testing.T) {
	for _, k := range []string{"LANGUAGE", "LC_ALL", "LC_MESSAGES", "LANG"} {
		t.Setenv(k, "")
	}
	if got := detectLang(); got != "en" {
		t.Errorf("all unset: want %q, got %q", "en", got)
	}
}

func TestDetectLang_LCAllWinsOverLang(t *testing.T) {
	t.Setenv("LANGUAGE", "")
	t.Setenv("LC_ALL", "pt_BR.UTF-8")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "en_US.UTF-8")
	if got := detectLang(); got != "pt-BR" {
		t.Errorf("LC_ALL priority: want %q, got %q", "pt-BR", got)
	}
}

func TestDetectLang_AutoPassedToInit(t *testing.T) {
	reset()
	t.Setenv("LANGUAGE", "")
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "en_US.UTF-8")
	Init("") // empty string → auto-detect
	if got := T("state_idle"); got != "idle" {
		t.Errorf("auto-detect Init: want %q, got %q", "idle", got)
	}
}
