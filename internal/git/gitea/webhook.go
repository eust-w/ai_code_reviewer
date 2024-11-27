package gitea

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"github.com/sirupsen/logrus"
)

// WebhookHandler handles Gitea webhook events
type WebhookHandler struct {
	secret []byte
	events map[string][]EventHandler
}

// EventHandler is a function that handles a specific Gitea event
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

	// Gitea sends the event type in the X-Gitea-Event header
	event := r.Header.Get("X-Gitea-Event")
	if event == "" {
		logrus.Error("Missing X-Gitea-Event header")
		http.Error(w, "Missing event header", http.StatusBadRequest)
		return
	}

	logrus.Infof("Received Gitea event: %s", event)

	// Parse the payload based on the event type
	var parsedPayload interface{}

	switch event {
	case "pull_request":
		// 使用我们自定义的HookPullRequestEvent类型来解析
		prEvent := &HookPullRequestEvent{}
		err = json.Unmarshal(payload, prEvent)
		parsedPayload = prEvent
	case "push":
		// 使用通用的map来解析，因为Gitea SDK没有直接提供PushPayload类型
		pushEvent := make(map[string]interface{})
		err = json.Unmarshal(payload, &pushEvent)
		parsedPayload = pushEvent
	case "ping":
		w.WriteHeader(http.StatusOK)
		return
	default:
		logrus.Warnf("Unsupported event type: %s", event)
		w.WriteHeader(http.StatusOK)
		return
	}

	if err != nil {
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

	// Gitea uses the X-Gitea-Signature header for webhook validation
	signature := r.Header.Get("X-Gitea-Signature")
	if signature == "" {
		logrus.Warn("No X-Gitea-Signature header found, skipping validation")
		return payload, nil
	}

	// Gitea uses HMAC SHA-256 for webhook signatures
	// We need to verify the signature
	if !validateGiteaSignature(h.secret, signature, payload) {
		return nil, fmt.Errorf("invalid signature")
	}

	return payload, nil
}

// validateGiteaSignature validates the HMAC SHA-256 signature from Gitea webhook
func validateGiteaSignature(secret []byte, signature string, payload []byte) bool {
	// Create a new HMAC with SHA-256
	h := hmac.New(sha256.New, secret)
	
	// Write the payload to the HMAC
	h.Write(payload)
	
	// Get the computed signature
	computedSignature := hex.EncodeToString(h.Sum(nil))
	
	// Compare the computed signature with the provided signature
	return hmac.Equal([]byte(computedSignature), []byte(signature))
}
