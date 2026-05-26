package azlanguage

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRedactEntities_TwoEntitiesDescendingOffsets(t *testing.T) {
	t.Parallel()

	text := "Meu cliente João Silva mora em Belo Horizonte"
	entities := []Entity{
		{Text: "João Silva", Category: "Person", Offset: 12, Length: 10},
		{Text: "Belo Horizonte", Category: "Address", Offset: 31, Length: 14},
	}

	got, cats := redactEntities(text, entities)
	want := "Meu cliente [PERSON_REDACTED] mora em [ADDRESS_REDACTED]"
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
	if cats["Person"] != 1 || cats["Address"] != 1 {
		t.Errorf("categories = %v; want {Person:1, Address:1}", cats)
	}
}

func TestRedactEntities_OutOfBoundsOffsetIsSkipped(t *testing.T) {
	t.Parallel()

	text := "hello"
	entities := []Entity{
		{Text: "bad", Category: "Person", Offset: 100, Length: 3}, // past end
	}
	got, cats := redactEntities(text, entities)
	if got != "hello" {
		t.Errorf("got %q; want passthrough %q", got, "hello")
	}
	// Counter still increments — admins want to know the API saw an entity
	// even if the offset was unusable for substitution.
	if cats["Person"] != 1 {
		t.Errorf("categories = %v; want Person:1 even when offset skipped", cats)
	}
}

func TestRedactEntities_NoEntitiesReturnsOriginal(t *testing.T) {
	t.Parallel()

	got, cats := redactEntities("nothing here", nil)
	if got != "nothing here" {
		t.Errorf("got %q; want passthrough", got)
	}
	if len(cats) != 0 {
		t.Errorf("categories = %v; want empty map", cats)
	}
}

func TestClient_Mask_EmptyTextSkipsCall(t *testing.T) {
	t.Parallel()

	// Server that should NEVER be called.
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("client must not call API for empty input")
	}))
	defer srv.Close()

	c := New(srv.URL, "key", "2024-11-01", "pt-BR", time.Second)
	res, err := c.Mask(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Redacted != "" {
		t.Errorf("got %q; want empty", res.Redacted)
	}
}

func TestClient_Mask_HappyPath(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Confirm header + URL shape
		if got := r.Header.Get("Ocp-Apim-Subscription-Key"); got != "test-key" {
			t.Errorf("subscription header = %q; want test-key", got)
		}
		if !strings.Contains(r.URL.RawQuery, "api-version=2024-11-01") {
			t.Errorf("api-version not in query: %q", r.URL.RawQuery)
		}

		// Confirm body shape
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req["kind"] != "PiiEntityRecognition" {
			t.Errorf("kind = %v; want PiiEntityRecognition", req["kind"])
		}

		// Echo a valid response
		_, _ = io.WriteString(w, `{
			"kind":"PiiEntityRecognitionResults",
			"results":{
				"documents":[{
					"id":"1",
					"redactedText":"Meu cliente *** *** mora",
					"entities":[
						{"text":"João Silva","category":"Person","offset":12,"length":10,"confidenceScore":0.99}
					]
				}],
				"errors":[]
			}
		}`)
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key", "2024-11-01", "pt-BR", time.Second)
	res, err := c.Mask(context.Background(), "Meu cliente João Silva mora")
	if err != nil {
		t.Fatal(err)
	}
	want := "Meu cliente [PERSON_REDACTED] mora"
	if res.Redacted != want {
		t.Errorf("redacted = %q; want %q", res.Redacted, want)
	}
	if len(res.Entities) != 1 {
		t.Errorf("entities count = %d; want 1", len(res.Entities))
	}
	if res.Categories["Person"] != 1 {
		t.Errorf("categories = %v; want Person:1", res.Categories)
	}
}

func TestClient_Mask_NoEntities(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{
			"kind":"PiiEntityRecognitionResults",
			"results":{"documents":[{"id":"1","redactedText":"clean","entities":[]}],"errors":[]}
		}`)
	}))
	defer srv.Close()

	c := New(srv.URL, "k", "2024-11-01", "pt-BR", time.Second)
	res, err := c.Mask(context.Background(), "clean text")
	if err != nil {
		t.Fatal(err)
	}
	// When the API returns zero entities, we pass through unchanged.
	if res.Redacted != "clean text" {
		t.Errorf("redacted = %q; want passthrough", res.Redacted)
	}
}

func TestClient_Mask_Non2xxIsError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"invalid key"}`)
	}))
	defer srv.Close()

	c := New(srv.URL, "wrong", "2024-11-01", "pt-BR", time.Second)
	_, err := c.Mask(context.Background(), "x")
	if err == nil {
		t.Fatal("expected error on 401")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("err = %v; want to mention status 401", err)
	}
}

func TestClient_Mask_ContextDeadline(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond) // delay past the client timeout
	}))
	defer srv.Close()

	c := New(srv.URL, "k", "2024-11-01", "pt-BR", 30*time.Millisecond)
	_, err := c.Mask(context.Background(), "x")
	if err == nil {
		t.Fatal("expected timeout error")
	}
	// http.Client wraps deadline as a generic Client.Timeout error; in newer
	// Go versions it's wrapped enough that errors.Is(context.DeadlineExceeded)
	// works. We assert the broader behavior: any non-nil error here is
	// acceptable, since the contract is "fail when slow".
	if errors.Is(err, context.Canceled) {
		t.Errorf("got context.Canceled; want timeout-style error: %v", err)
	}
}

func TestClient_Mask_EmptyDocumentsTreatedAsClean(t *testing.T) {
	t.Parallel()

	// Cognitive Services sometimes returns the wrapper with no documents
	// when it can't process the input. Treat as zero detections rather than
	// an error — the chat handler will still send the original text downstream.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"kind":"PiiEntityRecognitionResults","results":{"documents":[],"errors":[]}}`)
	}))
	defer srv.Close()

	c := New(srv.URL, "k", "2024-11-01", "pt-BR", time.Second)
	res, err := c.Mask(context.Background(), "anything")
	if err != nil {
		t.Fatal(err)
	}
	if res.Redacted != "anything" {
		t.Errorf("redacted = %q; want original passthrough", res.Redacted)
	}
}
