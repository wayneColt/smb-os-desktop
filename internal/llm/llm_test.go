package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFromEnv(t *testing.T) {
	env := map[string]string{}
	get := func(k string) string { return env[k] }
	if FromEnv(get) != nil {
		t.Fatal("no key: the model must stay off")
	}
	env["ANTHROPIC_API_KEY"] = "  "
	if FromEnv(get) != nil {
		t.Fatal("blank key: the model must stay off")
	}
	env["ANTHROPIC_API_KEY"] = "k"
	c := FromEnv(get)
	if c == nil || c.Model != "claude-sonnet-5-5" || c.BaseURL != DefaultBaseURL {
		t.Fatalf("defaults %+v", c)
	}
	env["AIOS_MODEL"] = "claude-opus-5-5"
	if FromEnv(get).Model != "claude-opus-5-5" {
		t.Fatal("AIOS_MODEL ignored")
	}
}

func TestAskSendsTheMessagesAPIRequest(t *testing.T) {
	var got struct {
		path, method, key, version, ctype string
		body                              request
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.path, got.method = r.URL.Path, r.Method
		got.key, got.version, got.ctype = r.Header.Get("x-api-key"), r.Header.Get("anthropic-version"), r.Header.Get("content-type")
		_ = json.NewDecoder(r.Body).Decode(&got.body)
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"thinking","thinking":""},{"type":"text","text":"Hello."},{"type":"text","text":"Second."}],"stop_reason":"end_turn"}`))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL + "/", APIKey: "sk-test", Model: "claude-sonnet-5-5", MaxTokens: 300, HTTP: srv.Client()}
	text, err := c.Ask(context.Background(), "be brief", "hi")
	if err != nil {
		t.Fatal(err)
	}
	if text != "Hello.\nSecond." {
		t.Fatalf("text %q", text)
	}
	if got.path != "/v1/messages" || got.method != "POST" || got.key != "sk-test" || got.version != "2023-06-01" || got.ctype != "application/json" {
		t.Fatalf("request %+v", got)
	}
	b := got.body
	if b.Model != "claude-sonnet-5-5" || b.MaxTokens != 300 || b.System != "be brief" || len(b.Messages) != 1 ||
		b.Messages[0].Role != "user" || b.Messages[0].Content != "hi" {
		t.Fatalf("body %+v", b)
	}
}

func TestAskErrorsNeverCarryTheKey(t *testing.T) {
	for name, h := range map[string]http.HandlerFunc{
		"401": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`))
		},
		"refusal": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"content":[],"stop_reason":"refusal"}`))
		},
		"empty": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"  "}],"stop_reason":"max_tokens"}`))
		},
	} {
		srv := httptest.NewServer(h)
		c := &Client{BaseURL: srv.URL, APIKey: "sk-secret-value", Model: "m", MaxTokens: 10, HTTP: srv.Client()}
		_, err := c.Ask(context.Background(), "", "q")
		srv.Close()
		if err == nil {
			t.Fatalf("%s: expected an error", name)
		}
		if strings.Contains(err.Error(), "sk-secret-value") {
			t.Fatalf("%s: error leaks the key: %v", name, err)
		}
		if name == "refusal" && !errors.Is(err, ErrRefused) {
			t.Fatalf("refusal: %v", err)
		}
		if name == "401" && !strings.Contains(err.Error(), "401") {
			t.Fatalf("401: %v", err)
		}
	}
}

func TestAskConnectionError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	c := &Client{BaseURL: url, APIKey: "sk-secret-value", Model: "m", MaxTokens: 10}
	_, err := c.Ask(context.Background(), "", "q")
	if err == nil || strings.Contains(err.Error(), "sk-secret-value") {
		t.Fatalf("err %v", err)
	}
}
