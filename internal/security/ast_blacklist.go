package security

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"mvdan.cc/sh/v3/syntax"
)

// ViolationCategory is a bitmask that classifies the kind of prohibited operation
// found in a shell command.
type ViolationCategory uint

const (
	CategoryNetwork       ViolationCategory = 1 << iota // outbound network tool
	CategoryDestructiveFS                               // destructive filesystem operation
)

// String implements fmt.Stringer for readable logging and test output.
func (c ViolationCategory) String() string {
	switch c {
	case CategoryNetwork:
		return "network"
	case CategoryDestructiveFS:
		return "destructive_fs"
	default:
		return fmt.Sprintf("ViolationCategory(%d)", uint(c))
	}
}

// Violation records a single prohibited command found during AST scanning.
type Violation struct {
	Command  string // command name as it appeared (after filepath.Base stripping)
	Reason   string // human-readable explanation
	Category ViolationCategory
	Line     uint // 1-based line in the script
	Col      uint // 1-based column
}

// ASTBlacklist scans shell scripts for prohibited network and filesystem
// operations. Construct one with NewASTBlacklist.
type ASTBlacklist struct {
	networkCmds   map[string]struct{}
	alwaysBadFS   []string
	conditionalFS map[string]func(args []string) (bool, string)
	parserPool    sync.Pool
}

// NewASTBlacklist returns a ready-to-use ASTBlacklist populated with the
// default prohibited command lists.
func NewASTBlacklist() *ASTBlacklist {
	bl := &ASTBlacklist{
		networkCmds: make(map[string]struct{}),
		conditionalFS: map[string]func([]string) (bool, string){
			"rm":    checkRM,
			"dd":    checkDD,
			"chmod": checkChmod,
		},
	}

	bl.parserPool = sync.Pool{
		New: func() any {
			return syntax.NewParser(syntax.Variant(syntax.LangBash))
		},
	}

	for _, name := range []string{
		"curl", "wget", "nc", "ncat", "netcat",
		"ssh", "scp", "sftp", "rsync",
		"ftp", "tftp", "telnet",
		"socat", "nmap",
		"dig", "nslookup", "host",
		"traceroute", "tracepath",
	} {
		bl.networkCmds[name] = struct{}{}
	}

	// Prefix matches: "mkfs" catches mkfs.ext4, mkfs.vfat, etc.
	bl.alwaysBadFS = []string{"shred", "wipefs", "mkfs", "fdisk", "parted", "blkdiscard"}

	return bl
}

// Scan parses script as a bash script and walks its AST, returning every
// prohibited operation found. An empty slice with a nil error means the script
// is clean. A non-nil error means the script could not be parsed and should be
// treated as suspicious by the caller.
func (bl *ASTBlacklist) Scan(script string) ([]Violation, error) {
	parser := bl.parserPool.Get().(*syntax.Parser)
	defer bl.parserPool.Put(parser)

	f, err := parser.Parse(strings.NewReader(script), "")
	if err != nil {
		return nil, err
	}

	var violations []Violation

	syntax.Walk(f, func(node syntax.Node) bool {
		call, ok := node.(*syntax.CallExpr)
		if !ok {
			return true // descend into non-command nodes
		}

		name, ok := commandName(call)
		if !ok {
			return true // non-literal command — skip
		}

		pos := call.Pos()

		// 1. Network check
		if _, found := bl.networkCmds[name]; found {
			violations = append(violations, Violation{
				Command:  name,
				Reason:   "outbound network tool is prohibited",
				Category: CategoryNetwork,
				Line:     pos.Line(),
				Col:      pos.Col(),
			})
			return true
		}

		// 2. Always-bad filesystem check — exact match or dot-namespaced variant
		// (e.g. "mkfs" matches "mkfs" and "mkfs.ext4" but not "mkfscheck").
		for _, prefix := range bl.alwaysBadFS {
			if name == prefix || strings.HasPrefix(name, prefix+".") {
				violations = append(violations, Violation{
					Command:  name,
					Reason:   name + " is a prohibited destructive filesystem operation",
					Category: CategoryDestructiveFS,
					Line:     pos.Line(),
					Col:      pos.Col(),
				})
				return true
			}
		}

		// 3. Conditional filesystem check
		if checker, found := bl.conditionalFS[name]; found {
			if deny, reason := checker(literalArgs(call)); deny {
				violations = append(violations, Violation{
					Command:  name,
					Reason:   reason,
					Category: CategoryDestructiveFS,
					Line:     pos.Line(),
					Col:      pos.Col(),
				})
			}
		}

		return true
	})

	return violations, nil
}

// commandName extracts the command name from the first word of a CallExpr.
// Returns ("", false) if the first word contains any non-literal part (e.g. a
// variable expansion or command substitution), since the command cannot be
// determined statically in that case.
func commandName(call *syntax.CallExpr) (string, bool) {
	if len(call.Args) == 0 {
		return "", false
	}
	word := call.Args[0]

	// Fast path: single literal part (the common case — avoid Builder allocation).
	if len(word.Parts) == 1 {
		lit, ok := word.Parts[0].(*syntax.Lit)
		if !ok {
			return "", false
		}
		name := filepath.Base(lit.Value)
		if name == "" || name == "." {
			return "", false
		}
		return name, true
	}

	var sb strings.Builder
	for _, part := range word.Parts {
		lit, ok := part.(*syntax.Lit)
		if !ok {
			return "", false
		}
		sb.WriteString(lit.Value)
	}
	name := filepath.Base(sb.String())
	if name == "" || name == "." {
		return "", false
	}
	return name, true
}

// literalArgs returns the static literal string values of all arguments after
// the command name. Words that contain any non-literal part are skipped.
func literalArgs(call *syntax.CallExpr) []string {
	var args []string
	for _, word := range call.Args[1:] {
		// Fast path: single literal part (the common case — avoid Builder allocation).
		if len(word.Parts) == 1 {
			if lit, ok := word.Parts[0].(*syntax.Lit); ok {
				args = append(args, lit.Value)
			}
			continue
		}

		var sb strings.Builder
		allLit := true
		for _, part := range word.Parts {
			lit, ok := part.(*syntax.Lit)
			if !ok {
				allLit = false
				break
			}
			sb.WriteString(lit.Value)
		}
		if allLit {
			args = append(args, sb.String())
		}
	}
	return args
}

// checkRM reports whether an rm invocation uses recursive+force flags.
// Both short forms (-r/-R/-f) and GNU long forms (--recursive/--force) are detected.
func checkRM(args []string) (dangerous bool, reason string) {
	var hasRecursive, hasForce bool
	for _, arg := range args {
		switch arg {
		case "--recursive":
			hasRecursive = true
		case "--force":
			hasForce = true
		default:
			// Only inspect short flags (single dash, not double dash).
			if !strings.HasPrefix(arg, "-") || strings.HasPrefix(arg, "--") {
				continue
			}
			for _, ch := range arg[1:] {
				switch ch {
				case 'r', 'R':
					hasRecursive = true
				case 'f':
					hasForce = true
				}
			}
		}
	}
	if hasRecursive && hasForce {
		return true, "rm with recursive and force flags performs recursive forced deletion"
	}
	return false, ""
}

// checkDD reports whether a dd invocation writes directly to a block device.
func checkDD(args []string) (dangerous bool, reason string) {
	for _, arg := range args {
		if strings.HasPrefix(arg, "of=") {
			val := arg[len("of="):]
			if strings.HasPrefix(val, "/dev/") {
				return true, "dd with of=/dev/... writes directly to a block device"
			}
		}
	}
	return false, ""
}

// checkChmod reports whether a chmod invocation sets world-writable permissions
// recursively. Detects symbolic modes: 777, a+w, o+w, and bare +w (equivalent
// to a+w).
func checkChmod(args []string) (dangerous bool, reason string) {
	var hasRecursive, hasWorldWritable bool
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") {
			for _, ch := range arg[1:] {
				if ch == 'R' {
					hasRecursive = true
				}
			}
		} else if arg == "777" ||
			strings.Contains(arg, "a+w") ||
			strings.Contains(arg, "o+w") ||
			arg == "+w" {
			hasWorldWritable = true
		}
	}
	if hasRecursive && hasWorldWritable {
		return true, "chmod -R with world-writable mode is a privilege escalation risk"
	}
	return false, ""
}
