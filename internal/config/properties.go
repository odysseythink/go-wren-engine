package config

import (
	"bufio"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// parseProperties parses a Java-compatible .properties file.
// Rules mirrored from java.util.Properties.load(InputStream):
//   • Comments: lines beginning with # or ! (leading whitespace OK)
//   • Empty lines ignored
//   • Separators: =, :, or arbitrary whitespace
//   • Line continuation: trailing unescaped \ before newline
//   • Escapes: \n \r \t \\ \= \: \# \! \ (space) \uXXXX
func parseProperties(r io.Reader) (map[string]string, error) {
	result := make(map[string]string)
	reader := bufio.NewReader(r)
	var buf strings.Builder

	for {
		rawLine, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return nil, err
		}
		isEOF := err == io.EOF

		// Strip line terminators (\n, \r\n, \r)
		line := rawLine
		if strings.HasSuffix(line, "\n") {
			line = line[:len(line)-1]
			if strings.HasSuffix(line, "\r") {
				line = line[:len(line)-1]
			}
		} else if strings.HasSuffix(line, "\r") {
			line = line[:len(line)-1]
		}

		if buf.Len() > 0 {
			// Drop leading whitespace on continuation lines
			start := 0
			for start < len(line) && isPropsSpace(line[start]) {
				start++
			}
			line = line[start:]
		}

		// Determine if line ends with an unescaped backslash
		backslashCount := 0
		for i := len(line) - 1; i >= 0; i-- {
			if line[i] == '\\' {
				backslashCount++
			} else {
				break
			}
		}
		if backslashCount%2 == 1 {
			// Continuation: drop trailing backslash and keep accumulating
			buf.WriteString(line[:len(line)-1])
			if isEOF {
				break
			}
			continue
		}

		buf.WriteString(line)
		if err := processLogicalLine(buf.String(), result); err != nil {
			return nil, err
		}
		buf.Reset()

		if isEOF {
			break
		}
	}

	if buf.Len() > 0 {
		if err := processLogicalLine(buf.String(), result); err != nil {
			return nil, err
		}
	}

	return result, nil
}

func processLogicalLine(line string, result map[string]string) error {
	i := 0
	for i < len(line) && isPropsSpace(line[i]) {
		i++
	}
	if i >= len(line) {
		return nil
	}
	c := line[i]
	if c == '#' || c == '!' {
		return nil
	}

	// Locate end of key, respecting escapes
	keyStart := i
	for i < len(line) {
		if line[i] == '\\' {
			if i+1 < len(line) {
				i += 2
				continue
			}
			i++
			break
		}
		if line[i] == '=' || line[i] == ':' || isPropsSpace(line[i]) {
			break
		}
		i++
	}
	keyEnd := i

	// Skip separator and any following whitespace
	if i < len(line) {
		sep := line[i]
		if sep == '=' || sep == ':' {
			i++
			for i < len(line) && isPropsSpace(line[i]) {
				i++
			}
		} else if isPropsSpace(sep) {
			for i < len(line) && isPropsSpace(line[i]) {
				i++
			}
		}
	}
	valueStart := i

	rawKey := line[keyStart:keyEnd]
	rawValue := line[valueStart:]

	key := unescapeProps(rawKey)
	value := unescapeProps(rawValue)
	result[key] = value
	return nil
}

func isPropsSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\f'
}

func unescapeProps(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\\' && i+1 < len(s) {
			next := s[i+1]
			switch next {
			case 'n':
				b.WriteByte('\n')
				i++
			case 'r':
				b.WriteByte('\r')
				i++
			case 't':
				b.WriteByte('\t')
				i++
			case '\\':
				b.WriteByte('\\')
				i++
			case '=':
				b.WriteByte('=')
				i++
			case ':':
				b.WriteByte(':')
				i++
			case '#':
				b.WriteByte('#')
				i++
			case '!':
				b.WriteByte('!')
				i++
			case ' ':
				b.WriteByte(' ')
				i++
			case 'u':
				if i+5 < len(s) {
					hex := s[i+2 : i+6]
					if val, err := strconv.ParseUint(hex, 16, 16); err == nil {
						b.WriteRune(rune(val))
						i += 5
						continue
					}
				}
				// Malformed unicode escape – treat 'u' literally
				b.WriteByte('u')
				i++
			default:
				b.WriteByte(next)
				i++
			}
		} else {
			b.WriteByte(c)
		}
	}
	return b.String()
}

func escapeProps(s string, isKey bool) string {
	var b strings.Builder
	b.Grow(len(s) * 2)
	first := true
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString("\\\\")
		case '\t':
			b.WriteString("\\t")
		case '\n':
			b.WriteString("\\n")
		case '\r':
			b.WriteString("\\r")
		case '\f':
			b.WriteString("\\f")
		case '=':
			b.WriteString("\\=")
		case ':':
			b.WriteString("\\:")
		case '#':
			b.WriteString("\\#")
		case '!':
			b.WriteString("\\!")
		case ' ':
			if first || isKey {
				b.WriteByte('\\')
			}
			b.WriteByte(' ')
		default:
			if r < ' ' || r > '~' {
				b.WriteString(fmt.Sprintf("\\u%04x", r))
			} else {
				b.WriteRune(r)
			}
		}
		first = false
	}
	return b.String()
}

// writePropertiesWithTimestamp writes a properties map in Java Properties.store() format.
//   • First line:  #<header>
//   • Second line: #<RFC1123-ish timestamp>
//   • Remaining:   keys in alphabetic order, values escaped
func writePropertiesWithTimestamp(w io.Writer, props map[string]string, header, timestamp string) error {
	bw := bufio.NewWriter(w)
	if _, err := bw.WriteString("#" + header + "\n"); err != nil {
		return err
	}
	if _, err := bw.WriteString("#" + timestamp + "\n"); err != nil {
		return err
	}

	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		ek := escapeProps(k, true)
		ev := escapeProps(props[k], false)
		if _, err := bw.WriteString(ek + "=" + ev + "\n"); err != nil {
			return err
		}
	}
	return bw.Flush()
}
