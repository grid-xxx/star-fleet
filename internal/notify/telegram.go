package notify

import (
	"fmt"
	"net/http"
	"net/url"
)

// Notifier sends lifecycle notifications.
type Notifier interface {
	PRCreated(prNumber, issueNumber int, title string)
	PRMerged(prNumber, issueNumber int)
	RunFailed(issueNumber int, errMsg string)
}

// Telegram sends messages via the Telegram Bot API.
type Telegram struct {
	BotToken string
	ChatID   string

	// httpPost is overridable for testing; defaults to http.PostForm.
	httpPost func(url string, data url.Values) (*http.Response, error)
}

func (t *Telegram) post() func(string, url.Values) (*http.Response, error) {
	if t.httpPost != nil {
		return t.httpPost
	}
	return http.PostForm
}

func (t *Telegram) send(text string) {
	endpoint := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", t.BotToken)
	resp, err := t.post()(endpoint, url.Values{
		"chat_id":    {t.ChatID},
		"text":       {text},
		"parse_mode": {"HTML"},
	})
	if err != nil {
		return
	}
	resp.Body.Close()
}

func (t *Telegram) PRCreated(prNumber, issueNumber int, title string) {
	t.send(fmt.Sprintf("🚀 PR #%d created for issue #%d: %s", prNumber, issueNumber, title))
}

func (t *Telegram) PRMerged(prNumber, issueNumber int) {
	t.send(fmt.Sprintf("✅ PR #%d merged for issue #%d", prNumber, issueNumber))
}

func (t *Telegram) RunFailed(issueNumber int, errMsg string) {
	t.send(fmt.Sprintf("❌ Fleet run failed for issue #%d: %s", issueNumber, errMsg))
}

// Nop is a no-op notifier used when no notification backend is configured.
type Nop struct{}

func (Nop) PRCreated(int, int, string) {}
func (Nop) PRMerged(int, int)          {}
func (Nop) RunFailed(int, string)      {}

// New returns a Telegram notifier if both token and chatID are non-empty,
// otherwise returns a silent Nop notifier.
func New(botToken, chatID string) Notifier {
	if botToken == "" || chatID == "" {
		return Nop{}
	}
	return &Telegram{BotToken: botToken, ChatID: chatID}
}
