package mail

import (
	"context"
	"sync"
)

// Message is one outbound email snapshot for E2E capture.
type Message struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	HTML    string `json:"html"`
	Text    string `json:"text"`
}

// Capture records emails (and optionally forwards to an inner Sender).
type Capture struct {
	Inner Sender
	mu    sync.Mutex
	all   []Message
}

func NewCapture(inner Sender) *Capture {
	if inner == nil {
		inner = LogSender{}
	}
	return &Capture{Inner: inner}
}

func (c *Capture) Send(ctx context.Context, to, subject, html, text string) error {
	c.mu.Lock()
	c.all = append(c.all, Message{To: to, Subject: subject, HTML: html, Text: text})
	c.mu.Unlock()
	if c.Inner != nil {
		return c.Inner.Send(ctx, to, subject, html, text)
	}
	return nil
}

func (c *Capture) Last() (Message, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.all) == 0 {
		return Message{}, false
	}
	return c.all[len(c.all)-1], true
}

func (c *Capture) LastFor(to string) (Message, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := len(c.all) - 1; i >= 0; i-- {
		if c.all[i].To == to {
			return c.all[i], true
		}
	}
	return Message{}, false
}

func (c *Capture) All() []Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Message, len(c.all))
	copy(out, c.all)
	return out
}
