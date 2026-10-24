package identity

import (
	"strings"
	"testing"

	"salad-bzminer-worker/internal/gpu"
)

func TestModelSlug(t *testing.T) {
	cases := map[string]string{
		"NVIDIA GeForce RTX 4090": "rtx4090",
		"NVIDIA RTX 5090":         "rtx5090",
		"GRID P4-8Q":              "gridp48q",
		"Tesla T4":                "t4",
		"":                        "gpu",
	}
	for in, want := range cases {
		if got := ModelSlug(in); got != want {
			t.Errorf("ModelSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSanitizeWorkerName(t *testing.T) {
	cases := map[string]string{
		"My Rig!":        "my-rig",
		"  salad__01  ":  "salad-01",
		"---":            "",
		"RTX 4090 / a81": "rtx-4090-a81",
	}
	for in, want := range cases {
		if got := SanitizeWorkerName(in); got != want {
			t.Errorf("SanitizeWorkerName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuild(t *testing.T) {
	got := Build("salad", "NVIDIA GeForce RTX 4090", "a81c2f3d")
	if got != "salad-rtx4090-a81c2f3d" {
		t.Fatalf("Build = %q", got)
	}
	if len(Build(strings.Repeat("p", 40), strings.Repeat("m", 40), "12345678")) > 32 {
		t.Fatal("name exceeds 32 chars")
	}
	if !strings.HasSuffix(Build(strings.Repeat("p", 40), "NVIDIA RTX 4090", "deadbeef"), "-deadbeef") {
		t.Fatal("suffix lost during truncation")
	}
}

func TestSuffixStableAndUnique(t *testing.T) {
	a := Suffix("instance-1", "machine-1")
	b := Suffix("instance-1", "machine-1")
	if a != b {
		t.Fatalf("suffix not stable: %q != %q", a, b)
	}
	if len(a) != 8 {
		t.Fatalf("suffix length = %d", len(a))
	}
	seen := make(map[string]bool, 1000)
	for i := range 1000 {
		id := "instance-" + string(rune('a'+i%26)) + "-" + string(rune('0'+i%10)) + "-" + string(rune(i))
		seen[Suffix(id)] = true
	}
	if len(seen) != 1000 {
		t.Fatalf("collisions: %d unique of 1000", len(seen))
	}
}

func TestResolveSourceUsesSaladInstance(t *testing.T) {
	lookup := func(k string) (string, bool) {
		if k == "SALAD_INSTANCE_ID" {
			return "inst-123", true
		}
		return "", false
	}
	src := ResolveSource(lookup, "host-1", []gpu.GPU{{UUID: "GPU-x"}})
	if !strings.Contains(src, "inst-123") || !strings.Contains(src, "host-1") || !strings.Contains(src, "GPU-x") {
		t.Fatalf("source = %q", src)
	}
}
