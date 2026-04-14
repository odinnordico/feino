package web

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// atRefRe matches @token or @relative/path tokens in user text.
// A token is a non-whitespace sequence starting with @.
var atRefRe = regexp.MustCompile(`@([\w./\-]+)`)

// ExpandRefs replaces @<token> and @<path> references in text with file
// content wrapped in XML tags:
//
//   - @<upload-token>  → resolved via fileSvc.Resolve, content inlined
//   - @<relative-path> → read relative to fileSvc's working dir
//
// Unknown/unreadable references are left verbatim so the model can see them.
func ExpandRefs(text string, fileSvc *fileService) string {
	return atRefRe.ReplaceAllStringFunc(text, func(match string) string {
		ref := match[1:] // strip leading @

		// First try upload-token lookup.
		if path, ok := fileSvc.Resolve(ref); ok {
			content, err := os.ReadFile(path)
			if err != nil {
				return match // leave verbatim if unreadable
			}
			return fmt.Sprintf("<file path=%q>\n%s\n</file>", ref, strings.TrimRight(string(content), "\n"))
		}

		// Then try as a relative path from working dir.
		full := fileSvc.AbsPath(ref)
		content, err := os.ReadFile(full)
		if err != nil {
			return match // leave verbatim
		}
		return fmt.Sprintf("<file path=%q>\n%s\n</file>", ref, strings.TrimRight(string(content), "\n"))
	})
}
