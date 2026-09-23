package parser

import (
	"regexp"
	"strings"
)

// mdxJSXTagLine matches a line made only of JSX tags: "<Foo />", "</Foo>",
// "<Foo prop="x">", fragments, and several tags in a row.
var mdxJSXTagLine = regexp.MustCompile(`^\s*(<\s*/?\s*([A-Za-z][\w.:-]*)?(\s[^<>]*)?/?\s*>\s*)+$`)

// mdxESMStart matches the first line of an ESM import or export statement.
var mdxESMStart = regexp.MustCompile(`^(import|export)([\s{*'"]|$)`)

// blankMDXNoise returns a copy of an MDX body with ESM statements and JSX tag
// lines replaced by spaces. Replacing rather than removing keeps every byte
// offset, so goldmark's segments still map to the raw file; the blanked lines
// read as blank, which ends the HTML block goldmark would otherwise discard
// along with the JSX children inside it.
func blankMDXNoise(body []byte) []byte {
	out := append([]byte(nil), body...)
	lines := splitRawLines(out)
	blank := func(l rawLine) {
		for i := l.start; i < l.end; i++ {
			out[i] = ' '
		}
	}
	fence := ""
	for i := 0; i < len(lines); i++ {
		t := lines[i].text(out)
		trimmed := strings.TrimSpace(t)
		if fence != "" {
			if strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, fence[:1]) == "" {
				fence = ""
			}
			continue
		}
		if f := codeFence(trimmed); f != "" {
			fence = f
			continue
		}
		// An ESM block is a top-level paragraph whose lines are all import or
		// export statements; bracket depth carries multi-line statements.
		if mdxESMStart.MatchString(t) && (i == 0 || isBlank(lines[i-1].text(out))) {
			end, ok := i, true
			depth := 0
			for ; end < len(lines) && !isBlank(lines[end].text(out)); end++ {
				line := lines[end].text(out)
				if depth == 0 && !mdxESMStart.MatchString(line) {
					ok = false
					break
				}
				depth += strings.Count(line, "{") + strings.Count(line, "(") + strings.Count(line, "[") -
					strings.Count(line, "}") - strings.Count(line, ")") - strings.Count(line, "]")
				depth = max(depth, 0)
			}
			if ok {
				for j := i; j < end; j++ {
					blank(lines[j])
				}
				i = end - 1
				continue
			}
		}
		if mdxJSXTagLine.MatchString(t) {
			blank(lines[i])
		}
	}
	return out
}

// codeFence returns the fence marker a trimmed line opens, or "".
func codeFence(trimmed string) string {
	for _, c := range []string{"`", "~"} {
		if n := len(trimmed) - len(strings.TrimLeft(trimmed, c)); n >= 3 {
			return strings.Repeat(c, n)
		}
	}
	return ""
}
