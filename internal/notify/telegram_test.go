package notify

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"
)

func TestPRCreated_Format(t *testing.T) {
	t.Parallel()
	var captured url.Values
	tg := &Telegram{
		BotToken: "tok",
		ChatID:   "123",
		httpPost: func(endpoint string, data url.Values) (*http.Response, error) {
			captured = data
			return &http.Response{Body: http.NoBody}, nil
		},
	}

	tg.PRCreated(7, 3, "Add dark mode")

	want := "🚀 PR #7 created for issue #3: Add dark mode"
	if got := captured.Get("text"); got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
	if got := captured.Get("parse_mode"); got != "HTML" {
		t.Errorf("parse_mode = %q, want HTML", got)
	}
	if got := captured.Get("chat_id"); got != "123" {
		t.Errorf("chat_id = %q, want 123", got)
	}
}

func TestPRMerged_Format(t *testing.T) {
	t.Parallel()
	var captured url.Values
	tg := &Telegram{
		BotToken: "tok",
		ChatID:   "456",
		httpPost: func(endpoint string, data url.Values) (*http.Response, error) {
			captured = data
			return &http.Response{Body: http.NoBody}, nil
		},
	}

	tg.PRMerged(10, 5)

	want := "✅ PR #10 merged for issue #5"
	if got := captured.Get("text"); got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
}

func TestRunFailed_Format(t *testing.T) {
	t.Parallel()
	var captured url.Values
	tg := &Telegram{
		BotToken: "tok",
		ChatID:   "789",
		httpPost: func(endpoint string, data url.Values) (*http.Response, error) {
			captured = data
			return &http.Response{Body: http.NoBody}, nil
		},
	}

	tg.RunFailed(12, "agent crash")

	want := "❌ Fleet run failed for issue #12: agent crash"
	if got := captured.Get("text"); got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
}

func TestSend_UsesCorrectEndpoint(t *testing.T) {
	t.Parallel()
	var capturedEndpoint string
	tg := &Telegram{
		BotToken: "mytoken",
		ChatID:   "99",
		httpPost: func(endpoint string, data url.Values) (*http.Response, error) {
			capturedEndpoint = endpoint
			return &http.Response{Body: http.NoBody}, nil
		},
	}

	tg.PRCreated(1, 1, "test")

	want := "https://api.telegram.org/botmytoken/sendMessage"
	if capturedEndpoint != want {
		t.Errorf("endpoint = %q, want %q", capturedEndpoint, want)
	}
}

func TestSend_HTTPError_DoesNotPanic(t *testing.T) {
	t.Parallel()
	tg := &Telegram{
		BotToken: "tok",
		ChatID:   "123",
		httpPost: func(endpoint string, data url.Values) (*http.Response, error) {
			return nil, fmt.Errorf("connection refused")
		},
	}

	// Should not panic on HTTP errors — notifications are fire-and-forget.
	tg.PRCreated(1, 1, "test")
}

func TestNop_DoesNotPanic(t *testing.T) {
	t.Parallel()
	n := Nop{}
	n.PRCreated(1, 2, "title")
	n.PRMerged(1, 2)
	n.RunFailed(2, "err")
}

func TestNew_WithCredentials(t *testing.T) {
	t.Parallel()
	n := New("token", "chat")
	if _, ok := n.(*Telegram); !ok {
		t.Errorf("New(token, chat) = %T, want *Telegram", n)
	}
}

func TestNew_MissingToken(t *testing.T) {
	t.Parallel()
	n := New("", "chat")
	if _, ok := n.(Nop); !ok {
		t.Errorf("New(\"\", chat) = %T, want Nop", n)
	}
}

func TestNew_MissingChatID(t *testing.T) {
	t.Parallel()
	n := New("token", "")
	if _, ok := n.(Nop); !ok {
		t.Errorf("New(token, \"\") = %T, want Nop", n)
	}
}

func TestNew_BothEmpty(t *testing.T) {
	t.Parallel()
	n := New("", "")
	if _, ok := n.(Nop); !ok {
		t.Errorf("New(\"\", \"\") = %T, want Nop", n)
	}
}
