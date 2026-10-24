package identity

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"salad-bzminer-worker/internal/gpu"
)

const maxWorkerName = 32

// Suffix returns a stable 8-hex-character suffix derived from the supplied
// identity parts. With no usable parts it falls back to random bytes.
func Suffix(parts ...string) string {
	nonEmpty := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			nonEmpty = append(nonEmpty, p)
		}
	}
	if len(nonEmpty) == 0 {
		var b [4]byte
		if _, err := rand.Read(b[:]); err != nil {
			return "00000000"
		}
		return hex.EncodeToString(b[:])
	}
	sum := sha256.Sum256([]byte(strings.Join(nonEmpty, "\x00")))
	return hex.EncodeToString(sum[:])[:8]
}

// ModelSlug turns a GPU name into a compact, pool-safe model identifier.
func ModelSlug(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	for _, prefix := range []string{"nvidia ", "geforce ", "tesla "} {
		s = strings.TrimPrefix(s, prefix)
	}
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
		if b.Len() == 16 {
			break
		}
	}
	if b.Len() == 0 {
		return "gpu"
	}
	return b.String()
}

// SanitizeWorkerName lowercases a worker name and replaces runs of characters
// outside [a-z0-9] with a single hyphen. The result is capped at 32 chars.
func SanitizeWorkerName(s string) string {
	var b strings.Builder
	lastHyphen := false
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastHyphen = false
		default:
			if !lastHyphen && b.Len() > 0 {
				b.WriteByte('-')
				lastHyphen = true
			}
		}
		if b.Len() == maxWorkerName {
			break
		}
	}
	return strings.Trim(b.String(), "-")
}

// Build assembles prefix-model-suffix, keeping the suffix and capping the
// total at 32 characters.
func Build(prefix, model, suffix string) string {
	prefix = SanitizeWorkerName(prefix)
	model = ModelSlug(model)
	suffix = SanitizeWorkerName(suffix)
	if suffix == "" {
		suffix = "00000000"
	}
	if len(suffix) > 8 {
		suffix = suffix[len(suffix)-8:]
	}

	budget := maxWorkerName - len(suffix) - 1
	if budget < 1 {
		return suffix
	}
	if prefix == "" {
		if len(model) > budget {
			model = model[:budget]
		}
		return strings.Trim(model+"-"+suffix, "-")
	}
	if model == "" {
		model = "gpu"
	}
	// Give the prefix priority but keep at least four model characters.
	if keep := budget - len(prefix) - 1; keep < len(model) {
		if keep < 4 {
			keep = 4
		}
		if keep > len(model) {
			keep = len(model)
		}
		model = model[:keep]
	}
	if len(prefix)+1+len(model) > budget {
		keep := budget - len(model) - 1
		if keep < 1 {
			keep = 1
		}
		prefix = prefix[:keep]
	}
	out := strings.Trim(prefix+"-"+model, "-") + "-" + suffix
	if len(out) > maxWorkerName {
		out = out[len(out)-maxWorkerName:]
	}
	return strings.Trim(out, "-")
}

// ResolveSource builds the per-replica identity used to derive the worker
// suffix. Only documented SaladCloud variables are used; hostname is the
// portable fallback (SaladCloud defaults HOSTNAME to the instance ID).
func ResolveSource(lookup func(string) (string, bool), hostname string, gpus []gpu.GPU) string {
	parts := make([]string, 0, 6)
	for _, key := range []string{
		"SALAD_INSTANCE_ID",
		"SALAD_MACHINE_ID",
		"SALAD_CONTAINER_GROUP_ID",
	} {
		if v, ok := lookup(key); ok {
			parts = append(parts, v)
		}
	}
	parts = append(parts, hostname)
	if len(gpus) > 0 {
		if gpus[0].UUID != "" {
			parts = append(parts, gpus[0].UUID)
		} else if gpus[0].PCIID != "" {
			parts = append(parts, gpus[0].PCIID)
		}
	}
	nonEmpty := make([]string, 0, len(parts))
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			nonEmpty = append(nonEmpty, p)
		}
	}
	if len(nonEmpty) == 0 {
		var b [8]byte
		if _, err := rand.Read(b[:]); err != nil {
			return "unknown"
		}
		return hex.EncodeToString(b[:])
	}
	return strings.Join(nonEmpty, "\x00")
}
