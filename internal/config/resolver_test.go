package config

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeResolver is an in-memory SecretResolver for tests. Counts Get calls
// per name so we can assert deduplication.
type fakeResolver struct {
	values map[string]string
	err    map[string]error
	calls  map[string]int
}

func (f *fakeResolver) Get(_ context.Context, name string) (string, error) {
	f.calls[name]++
	if e, ok := f.err[name]; ok {
		return "", e
	}
	v, ok := f.values[name]
	if !ok {
		return "", errors.New("not found")
	}
	return v, nil
}

func TestResolveKVRefs_NoMarkers(t *testing.T) {
	t.Parallel()

	in := "azure_openai:\n  api_key: ${AZURE_OPENAI_API_KEY}\n"
	out, err := resolveKVRefs(context.Background(), in, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != in {
		t.Errorf("expected passthrough; got mutation: %q", out)
	}
}

func TestResolveKVRefs_NilResolverWithRefsFails(t *testing.T) {
	t.Parallel()

	in := "api_key: ${kv:FOO}\ndb: ${kv:BAR}\n"
	_, err := resolveKVRefs(context.Background(), in, nil)
	if err == nil {
		t.Fatal("expected error when resolver is nil but YAML has ${kv:...}")
	}
	if !strings.Contains(err.Error(), "KEYVAULT_URI") {
		t.Errorf("error %q should mention KEYVAULT_URI", err.Error())
	}
	if !strings.Contains(err.Error(), "FOO") || !strings.Contains(err.Error(), "BAR") {
		t.Errorf("error %q should list the referenced names", err.Error())
	}
}

func TestResolveKVRefs_HappyPath(t *testing.T) {
	t.Parallel()

	r := &fakeResolver{
		values: map[string]string{"FOO": "foo-value", "BAR": "bar-value"},
		calls:  map[string]int{},
	}
	in := "a: ${kv:FOO}\nb: ${kv:BAR}\n"
	out, err := resolveKVRefs(context.Background(), in, r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "a: foo-value\nb: bar-value\n"
	if out != want {
		t.Errorf("got %q; want %q", out, want)
	}
}

func TestResolveKVRefs_DedupesFetches(t *testing.T) {
	t.Parallel()

	r := &fakeResolver{
		values: map[string]string{"FOO": "v"},
		calls:  map[string]int{},
	}
	in := "a: ${kv:FOO}\nb: ${kv:FOO}\nc: ${kv:FOO}\n"
	if _, err := resolveKVRefs(context.Background(), in, r); err != nil {
		t.Fatal(err)
	}
	if r.calls["FOO"] != 1 {
		t.Errorf("Get(FOO) called %d times; want 1 (must dedupe)", r.calls["FOO"])
	}
}

func TestResolveKVRefs_CollectsAllErrors(t *testing.T) {
	t.Parallel()

	r := &fakeResolver{
		values: map[string]string{"OK": "ok"},
		err: map[string]error{
			"BAD1": errors.New("first failure"),
			"BAD2": errors.New("second failure"),
		},
		calls: map[string]int{},
	}
	in := "a: ${kv:OK}\nb: ${kv:BAD1}\nc: ${kv:BAD2}\n"
	_, err := resolveKVRefs(context.Background(), in, r)
	if err == nil {
		t.Fatal("expected joined error")
	}
	if !strings.Contains(err.Error(), "first failure") {
		t.Errorf("error %q should mention BAD1", err.Error())
	}
	if !strings.Contains(err.Error(), "second failure") {
		t.Errorf("error %q should mention BAD2", err.Error())
	}
}

func TestResolveKVRefs_AcceptsAllowedCharSet(t *testing.T) {
	t.Parallel()

	r := &fakeResolver{
		values: map[string]string{
			"AZURE-OPENAI-API-KEY": "k1",
			"db-password":          "k2",
			"NUM-123":              "k3",
		},
		calls: map[string]int{},
	}
	in := "a: ${kv:AZURE-OPENAI-API-KEY}\nb: ${kv:db-password}\nc: ${kv:NUM-123}\n"
	out, err := resolveKVRefs(context.Background(), in, r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "k1") || !strings.Contains(out, "k2") || !strings.Contains(out, "k3") {
		t.Errorf("substitution incomplete: %q", out)
	}
}

func TestResolveKVRefs_RejectsInvalidCharsAsLiteral(t *testing.T) {
	t.Parallel()

	// Underscores and dots aren't in Key Vault's allowed name charset. The
	// regex doesn't match them, so the literal passes through to YAML — which
	// is the right behavior: an operator who typo'd will see "${kv:bad_name}"
	// appear verbatim in errors instead of a silent failure.
	r := &fakeResolver{calls: map[string]int{}}
	in := "a: ${kv:bad_name}\n"
	out, err := resolveKVRefs(context.Background(), in, r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != in {
		t.Errorf("expected passthrough for invalid name; got %q", out)
	}
	if r.calls["bad_name"] != 0 {
		t.Error("Get should NOT have been called for an unmatched pattern")
	}
}
