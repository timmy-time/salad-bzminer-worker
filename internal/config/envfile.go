package config

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// ParseEnvFile parses the small dotenv subset used by config/defaults.env:
// KEY=VALUE lines, blank lines and # comments, and optional surrounding quotes.
// No variable expansion is performed.
func ParseEnvFile(r io.Reader) (map[string]string, error) {
	out := make(map[string]string)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		idx := strings.IndexByte(line, '=')
		if idx < 1 {
			return nil, fmt.Errorf("defaults line %d: expected KEY=VALUE", lineNo)
		}
		key := strings.TrimSpace(line[:idx])
		if !validEnvKey(key) {
			return nil, fmt.Errorf("defaults line %d: invalid variable name %q", lineNo, key)
		}
		value := strings.TrimSpace(line[idx+1:])
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') ||
				(value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}
		out[key] = value
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read defaults: %w", err)
	}
	return out, nil
}

func validEnvKey(key string) bool {
	if key == "" {
		return false
	}
	for i, r := range key {
		if i == 0 {
			if !((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '_') {
				return false
			}
			continue
		}
		if !((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_') {
			return false
		}
	}
	return true
}
