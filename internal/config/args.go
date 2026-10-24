package config

import (
	"fmt"
	"strings"
)

const maxExtraArgsBytes = 4096
const maxExtraArgs = 64

var managedFlags = map[string]bool{
	"-a": true, "--algorithm": true,
	"-w": true, "--wallet": true,
	"-p": true, "--pool": true, "--pool2": true, "--pool-fallback": true,
	"-r": true, "--rig_name": true, "--worker": true,
	"--cpu":    true,
	"--nvidia": true, "--amd": true, "--intel": true, "--igpu": true,
	"--devices": true,
	"-o":        true, "--output": true,
	"--set": true, "--config": true, "--save-config": true,
	"--auto-update": true, "--plugin-update": true, "--plugin-manifest": true,
	"--http_enabled": true, "--http_address": true, "--http_port": true,
	"--log-level": true, "--log_level": true,
	"--dmon": true, "--gpu-info": true, "--cpu-info": true, "--ram-info": true,
	"--list-metrics": true, "--list-columns": true, "--list-algos": true,
	"--list-plugins": true, "--build-info": true, "--bench": true,
	"--benchmark": true, "--print-config": true, "--config-doc": true,
	"--version": true, "-V": true, "--help": true, "-h": true,
}

var managedPrefixes = []string{
	"-a=", "-w=", "-p=",
	"--http_", "--api_", "--set=", "--devices", "--nvidia", "--amd",
	"--intel", "--igpu", "--wallet", "--pool", "--algorithm", "--worker",
	"--config", "--auto-update", "--plugin-",
}

// SplitArgs tokenizes a shell-like argument string without invoking a shell.
// It supports single quotes, double quotes, and backslash escapes.
func SplitArgs(s string) ([]string, error) {
	if strings.ContainsAny(s, "\x00\r\n") {
		return nil, fmt.Errorf("MINER_EXTRA_ARGS: control characters are not allowed")
	}
	var args []string
	var b strings.Builder
	started := false
	inSingle := false
	inDouble := false

	flush := func() {
		if started {
			args = append(args, b.String())
			b.Reset()
			started = false
		}
	}

	for i := 0; i < len(s); i++ {
		c := s[i]
		if inSingle {
			if c == '\'' {
				inSingle = false
			} else {
				b.WriteByte(c)
			}
			started = true
			continue
		}
		if inDouble {
			switch c {
			case '"':
				inDouble = false
			case '\\':
				if i+1 >= len(s) {
					return nil, fmt.Errorf("MINER_EXTRA_ARGS: trailing backslash")
				}
				i++
				b.WriteByte(s[i])
			default:
				b.WriteByte(c)
			}
			started = true
			continue
		}
		switch c {
		case ' ', '\t':
			flush()
		case '\'':
			inSingle = true
			started = true
		case '"':
			inDouble = true
			started = true
		case '\\':
			if i+1 >= len(s) {
				return nil, fmt.Errorf("MINER_EXTRA_ARGS: trailing backslash")
			}
			i++
			b.WriteByte(s[i])
			started = true
		default:
			b.WriteByte(c)
			started = true
		}
	}
	if inSingle {
		return nil, fmt.Errorf("MINER_EXTRA_ARGS: unmatched single quote")
	}
	if inDouble {
		return nil, fmt.Errorf("MINER_EXTRA_ARGS: unmatched double quote")
	}
	flush()
	return args, nil
}

// ValidateExtraArgs rejects NUL/CR/LF, excessive length, and flags that the
// worker manages from environment variables.
func ValidateExtraArgs(args []string) error {
	if len(args) > maxExtraArgs {
		return fmt.Errorf("MINER_EXTRA_ARGS: too many arguments (%d > %d)", len(args), maxExtraArgs)
	}
	total := 0
	for _, a := range args {
		total += len(a)
		if strings.ContainsAny(a, "\x00\r\n") {
			return fmt.Errorf("MINER_EXTRA_ARGS: control characters are not allowed")
		}
		if a == "--" {
			return fmt.Errorf("MINER_EXTRA_ARGS: %q is managed by the worker", a)
		}
		name := a
		if i := strings.IndexByte(name, '='); i >= 0 {
			name = name[:i]
		}
		if managedFlags[name] || cpuFlagManaged(name) {
			return fmt.Errorf("MINER_EXTRA_ARGS: %s is managed by the worker", name)
		}
		for _, p := range managedPrefixes {
			if strings.HasPrefix(name, p) {
				return fmt.Errorf("MINER_EXTRA_ARGS: %s is managed by the worker", name)
			}
		}
	}
	if total > maxExtraArgsBytes {
		return fmt.Errorf("MINER_EXTRA_ARGS: too long (%d > %d bytes)", total, maxExtraArgsBytes)
	}
	return nil
}

func cpuFlagManaged(name string) bool {
	rest, ok := strings.CutPrefix(name, "--cpu")
	if !ok {
		return false
	}
	if rest == "" {
		return true
	}
	for i := range len(rest) {
		if rest[i] < '0' || rest[i] > '9' {
			return false
		}
	}
	return true
}
