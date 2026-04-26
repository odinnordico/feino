package tools

import (
	"testing"

	"github.com/odinnordico/feino/internal/memory"
)

// assertSchemasRejectExtraProperties walks every tool in the slice and fails
// the test for any whose JSON schema is missing `additionalProperties: false`,
// has it set to true, or whose schema map is nil.
//
// Why we care: when an LLM hallucinates a parameter name (e.g. `waitFor`
// instead of the documented `wait_for`), an unconstrained schema causes the
// provider to silently accept the call and our handler to run with the
// real argument missing. The user sees "the tool did nothing" — debugging is
// painful. With `additionalProperties: false` the provider rejects the call
// before it reaches us, with a clear "Unexpected key: waitFor" error that the
// LLM can self-correct on retry.
//
// Subtests are named per-tool so a CI failure pinpoints exactly which
// schema regressed.
func assertSchemasRejectExtraProperties(t *testing.T, suite string, list []Tool) {
	t.Helper()
	if len(list) == 0 {
		t.Fatalf("%s: tool list is empty", suite)
	}
	for _, tool := range list {
		name := tool.GetName()
		t.Run(suite+"/"+name, func(t *testing.T) {
			schema := tool.GetParameters()
			if schema == nil {
				t.Fatalf("%s: GetParameters returned nil", name)
			}
			ap, present := schema["additionalProperties"]
			if !present {
				t.Errorf("%s: schema is missing additionalProperties (must be false to fail loudly on hallucinated params)", name)
				return
			}
			b, ok := ap.(bool)
			if !ok {
				t.Errorf("%s: additionalProperties is %T, want bool", name, ap)
				return
			}
			if b {
				t.Errorf("%s: additionalProperties is true; must be false to reject hallucinated params", name)
			}
		})
	}
}

// TestNativeTools_AllSchemasRejectExtraProperties exercises every tool
// returned by NewNativeTools (shell, files, git, web, http, currency,
// sysinfo, notify, weather, and the full browser suite).
func TestNativeTools_AllSchemasRejectExtraProperties(t *testing.T) {
	assertSchemasRejectExtraProperties(t, "native", NewNativeTools(nil))
}

// TestMemoryTools_AllSchemasRejectExtraProperties exercises the memory tool
// suite separately because NewMemoryTools needs a memory.Store and is not
// included in NewNativeTools.
func TestMemoryTools_AllSchemasRejectExtraProperties(t *testing.T) {
	store, err := memory.NewFileStore(t.TempDir() + "/memory.json")
	if err != nil {
		t.Fatalf("create memory store: %v", err)
	}
	assertSchemasRejectExtraProperties(t, "memory", NewMemoryTools(store, nil))
}
