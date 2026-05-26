package keyvault

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azsecrets"
)

// fakeAzClient is an in-memory azClient for tests. Counts calls per name so
// we can assert cache hit/miss behavior.
type fakeAzClient struct {
	values map[string]string
	err    error
	calls  map[string]*int32
}

func newFakeAz(values map[string]string) *fakeAzClient {
	calls := make(map[string]*int32, len(values))
	for k := range values {
		var c int32
		calls[k] = &c
	}
	return &fakeAzClient{values: values, calls: calls}
}

func (f *fakeAzClient) GetSecret(_ context.Context, name, _ string, _ *azsecrets.GetSecretOptions) (azsecrets.GetSecretResponse, error) {
	if f.err != nil {
		return azsecrets.GetSecretResponse{}, f.err
	}
	if c, ok := f.calls[name]; ok {
		atomic.AddInt32(c, 1)
	}
	v, ok := f.values[name]
	if !ok {
		return azsecrets.GetSecretResponse{}, errors.New("not found")
	}
	return azsecrets.GetSecretResponse{Secret: azsecrets.Secret{Value: &v}}, nil
}

func (f *fakeAzClient) callCount(name string) int32 {
	if c, ok := f.calls[name]; ok {
		return atomic.LoadInt32(c)
	}
	return 0
}

func newTestClient(az azClient, ttl time.Duration) *Client {
	return &Client{
		az:    az,
		ttl:   ttl,
		cache: make(map[string]entry),
	}
}

func TestClient_Get_HitsAzureOnce(t *testing.T) {
	t.Parallel()

	az := newFakeAz(map[string]string{"FOO": "hello"})
	c := newTestClient(az, time.Hour)

	for i := 0; i < 5; i++ {
		v, err := c.Get(context.Background(), "FOO")
		if err != nil {
			t.Fatalf("iter %d: unexpected error: %v", i, err)
		}
		if v != "hello" {
			t.Errorf("iter %d: value = %q; want hello", i, v)
		}
	}
	if got := az.callCount("FOO"); got != 1 {
		t.Errorf("Azure call count = %d; want 1 (subsequent reads must be cache hits)", got)
	}
}

func TestClient_Get_RefetchesAfterTTL(t *testing.T) {
	t.Parallel()

	az := newFakeAz(map[string]string{"FOO": "v1"})
	c := newTestClient(az, 50*time.Millisecond)

	if _, err := c.Get(context.Background(), "FOO"); err != nil {
		t.Fatal(err)
	}
	// Mutate the upstream value and wait past the TTL.
	az.values["FOO"] = "v2"
	time.Sleep(80 * time.Millisecond)

	got, err := c.Get(context.Background(), "FOO")
	if err != nil {
		t.Fatal(err)
	}
	if got != "v2" {
		t.Errorf("post-TTL value = %q; want v2 (cache should have expired)", got)
	}
	if got := az.callCount("FOO"); got != 2 {
		t.Errorf("Azure call count = %d; want 2", got)
	}
}

func TestClient_Get_EmptyValueIsError(t *testing.T) {
	t.Parallel()

	az := newFakeAz(map[string]string{"FOO": ""})
	c := newTestClient(az, time.Hour)

	_, err := c.Get(context.Background(), "FOO")
	if !errors.Is(err, ErrEmptyValue) {
		t.Errorf("err = %v; want ErrEmptyValue", err)
	}
}

func TestClient_Get_AzureErrorPropagates(t *testing.T) {
	t.Parallel()

	az := &fakeAzClient{err: errors.New("boom")}
	c := newTestClient(az, time.Hour)

	_, err := c.Get(context.Background(), "FOO")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errorContains(err, "boom") || !errorContains(err, `"FOO"`) {
		t.Errorf("err = %v; want wrapped with secret name and underlying message", err)
	}
}

func TestClient_Get_DifferentSecretsCachedIndependently(t *testing.T) {
	t.Parallel()

	az := newFakeAz(map[string]string{"FOO": "f", "BAR": "b"})
	c := newTestClient(az, time.Hour)

	_, _ = c.Get(context.Background(), "FOO")
	_, _ = c.Get(context.Background(), "BAR")
	_, _ = c.Get(context.Background(), "FOO")
	_, _ = c.Get(context.Background(), "BAR")

	if got := az.callCount("FOO"); got != 1 {
		t.Errorf("FOO Azure calls = %d; want 1", got)
	}
	if got := az.callCount("BAR"); got != 1 {
		t.Errorf("BAR Azure calls = %d; want 1", got)
	}
}

func errorContains(err error, substr string) bool {
	if err == nil {
		return false
	}
	return contains(err.Error(), substr)
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
