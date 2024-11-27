package gitlab

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/sirupsen/logrus"
	"github.com/xanzy/go-gitlab"
)

// WebhookHandler handles GitLab webhook events
type WebhookHandler struct {
	secret []byte
	events map[string][]EventHandler
}

// EventHandler is a function that handles a specific GitLab event
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

	// GitLab sends the event type in the X-Gitlab-Event header
	event := r.Header.Get("X-Gitlab-Event")
	if event == "" {
		logrus.Error("Missing X-Gitlab-Event header")
		http.Error(w, "Missing event header", http.StatusBadRequest)
		return
	}

	logrus.Infof("Received GitLab event: %s", event)

	// Parse the payload based on the event type
	var parsedPayload interface{}
	switch event {
	case "Merge Request Hook":
		parsedPayload = &gitlab.MergeEvent{}
	case "Push Hook":
		parsedPayload = &gitlab.PushEvent{}
	case "System Hook":
		// System hooks need special handling
		// GitLab SDK没有直接提供SystemHookEvent类型，使用通用map
		parsedPayload = make(map[string]interface{})
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

	// GitLab uses the X-Gitlab-Token header for webhook validation
	token := r.Header.Get("X-Gitlab-Token")
	if token == "" {
		logrus.Warn("No X-Gitlab-Token header found, skipping validation")
		return payload, nil
	}

	// Simple token comparison
	if token != string(h.secret) {
		return nil, fmt.Errorf("invalid token")
	}

	return payload, nil
}
