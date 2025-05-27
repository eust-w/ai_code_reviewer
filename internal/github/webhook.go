package github

import (
	"encoding/json"
	"io"
	"net/http"
	"fmt"
	"github.com/google/go-github/v60/github"
	"github.com/sirupsen/logrus"
)

// WebhookHandler handles GitHub webhook events
type WebhookHandler struct {
	secret []byte
	events map[string][]EventHandler
}

// EventHandler is a function that handles a specific GitHub event
type EventHandler func(payload interface{}) error

// NewWebhookHandler creates a new webhook handler
func NewWebhookHandler(secret string) *WebhookHandler {
	return &WebhookHandler{
		secret: []byte(secret),
		events: make(map[string][]EventHandler),
	}
}

// On registers a handler for a specific event
func (h *WebhookHandler) On(event string, handler EventHandler) {
	h.events[event] = append(h.events[event], handler)
}

// HandleWebhook handles incoming webhook requests
func (h *WebhookHandler) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	payload, err := h.validatePayload(r)
	if err != nil {
		logrus.Errorf("Error validating webhook payload: %v", err)
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	event := r.Header.Get("X-GitHub-Event")
	if event == "" {
		logrus.Error("Missing X-GitHub-Event header")
		http.Error(w, "Missing event header", http.StatusBadRequest)
		return
	}

	logrus.Infof("Received GitHub event: %s", event)

	// Parse the payload based on the event type
	var parsedPayload interface{}
	switch event {
	case "pull_request":
		parsedPayload = &github.PullRequestEvent{}
	case "push":
		parsedPayload = &github.PushEvent{}
	case "ping":
		w.WriteHeader(http.StatusOK)
		return
	default:
		logrus.Warnf("Unsupported event type: %s", event)
		w.WriteHeader(http.StatusOK)
		return
	}

	if err := json.Unmarshal(payload, parsedPayload); err != nil {
		logrus.Errorf("Error parsing webhook payload: %v", err)
		http.Error(w, "Error parsing payload", http.StatusBadRequest)
		return
	}

	// Call registered handlers for this event
	handlers, ok := h.events[event]
	if !ok {
		logrus.Debugf("No handlers registered for event: %s", event)
		w.WriteHeader(http.StatusOK)
		return
	}

	for _, handler := range handlers {
		if err := handler(parsedPayload); err != nil {
			logrus.Errorf("Error handling event: %v", err)
			// Continue processing other handlers
		}
	}

	w.WriteHeader(http.StatusOK)
}

// validatePayload validates the webhook payload
func (h *WebhookHandler) validatePayload(r *http.Request) ([]byte, error) {
	payload, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}

	// If no secret is set, skip validation
	if len(h.secret) == 0 {
		return payload, nil
	}

	signature := r.Header.Get("X-Hub-Signature-256")
	if signature == "" {
		signature = r.Header.Get("X-Hub-Signature")
	}

	if signature == "" {
		return nil, fmt.Errorf("missing Hub signature")
	}

	if err := github.ValidateSignature(signature, payload, h.secret); err != nil {
		return nil, err
	}

	return payload, nil
}
