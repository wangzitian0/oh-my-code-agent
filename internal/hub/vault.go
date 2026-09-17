package hub

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"unicode"
)

// ExpandEnv replaces ${VAR_NAME} or $VAR_NAME in s based on the values of the
// provided envMap, falling back to os.Getenv.
// Undefined variables are replaced with an empty string.
// Syntax errors (such as unclosed braces) leave the text unchanged.
func ExpandEnv(s string, envMap map[string]string) string {
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		c := s[i]
		if c != '$' || i+1 >= len(s) {
			b.WriteByte(c)
			i++
			continue
		}
		next := s[i+1]
		// "$$" escapes to a literal "$"
		if next == '$' {
			b.WriteByte('$')
			i += 2
			continue
		}
		if next == '{' {
			end := strings.IndexByte(s[i+2:], '}')
			if end < 0 {
				// Unclosed brace: leave unchanged
				b.WriteByte(c)
				i++
				continue
			}
			name := s[i+2 : i+2+end]
			if !isValidEnvName(name) {
				// Invalid variable name: leave unchanged
				b.WriteByte(c)
				i++
				continue
			}
			b.WriteString(lookupEnv(name, envMap))
			i += 2 + end + 1
			continue
		}
		if isAlphaOrUnderscore(rune(next)) {
			j := i + 1
			for j < len(s) && isAlnumOrUnderscore(rune(s[j])) {
				j++
			}
			name := s[i+1 : j]
			b.WriteString(lookupEnv(name, envMap))
			i = j
			continue
		}
		// Lone '$' or invalid following character
		b.WriteByte(c)
		i++
	}
	return b.String()
}

func isValidEnvName(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i, r := range s {
		if i == 0 {
			if !isAlphaOrUnderscore(r) {
				return false
			}
		} else {
			if !isAlnumOrUnderscore(r) {
				return false
			}
		}
	}
	return true
}

func isAlphaOrUnderscore(r rune) bool {
	return unicode.IsLetter(r) || r == '_'
}

func isAlnumOrUnderscore(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

func lookupEnv(key string, envMap map[string]string) string {
	if envMap != nil {
		if v, ok := envMap[key]; ok {
			return v
		}
	}
	return os.Getenv(key)
}

// LoadEnvFile parses a standard .env file into key-value pairs.
// It ignores empty lines and comment lines starting with '#', supports
// optional 'export ' prefixes, and strips surrounding single or double quotes.
func LoadEnvFile(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("hub: read env file %q: %w", path, err)
	}

	res := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}

		idx := strings.IndexByte(line, '=')
		if idx <= 0 {
			continue
		}

		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])

		// Handle quoted values or unquoted inline comments
		if len(val) >= 2 && ((val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'')) {
			val = val[1 : len(val)-1]
		} else if commentIdx := strings.Index(val, " #"); commentIdx != -1 {
			val = strings.TrimSpace(val[:commentIdx])
		}

		res[key] = val
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("hub: scan env file %q: %w", path, err)
	}

	return res, nil
}

// LoadEnvFiles loads multiple env files in order, with later files overriding earlier ones.
func LoadEnvFiles(paths []string) (map[string]string, error) {
	merged := make(map[string]string)
	for _, p := range paths {
		if p == "" {
			continue
		}
		if strings.HasPrefix(p, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				p = home + p[1:]
			}
		}
		if _, err := os.Stat(p); os.IsNotExist(err) {
			continue
		}
		env, err := LoadEnvFile(p)
		if err != nil {
			return nil, err
		}
		for k, v := range env {
			merged[k] = v
		}
	}
	return merged, nil
}

// HydrateToolConfig returns a new ToolConfig with all environment variable
// templates (${VAR}) expanded in Command, Args, WorkingDir, and Env values.
func HydrateToolConfig(cfg ToolConfig, envMap map[string]string) ToolConfig {
	out := cfg
	out.Command = ExpandEnv(cfg.Command, envMap)
	out.WorkingDir = ExpandEnv(cfg.WorkingDir, envMap)

	if len(cfg.Args) > 0 {
		out.Args = make([]string, len(cfg.Args))
		for i, a := range cfg.Args {
			out.Args[i] = ExpandEnv(a, envMap)
		}
	}

	if len(cfg.Env) > 0 {
		out.Env = make(map[string]string, len(cfg.Env))
		for k, v := range cfg.Env {
			out.Env[k] = ExpandEnv(v, envMap)
		}
	}

	return out
}
