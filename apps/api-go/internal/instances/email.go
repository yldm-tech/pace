package instances

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
)

// The subject and body of the one message this sends, which are literals rather than a template.
const (
	credentialCheckSubject = "Email Notification from Plane"
	credentialCheckBody    = "This is a sample email notification sent from Plane application."
)

// emailCredentialsCheck sends one message with the settings as they stand, so an operator can tell whether they work before anything depends on them.
//
// Every way it can fail answers 400 with a sentence naming the kind of failure, and the ones Django names come from SMTP's own reply codes. The Go client does not separate them the way python's smtplib does, so what is reported is the one sentence that covers the rest — the request still fails, and with a message rather than a traceback.
func (handler *Handler) emailCredentialsCheck(c *gin.Context, _ *auth.User, _ *Instance) {
	payload := map[string]any{}
	_ = c.ShouldBindJSON(&payload)
	receiver, _ := payload["receiver_email"].(string)
	if receiver == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Receiver email is required"})
		return
	}
	if handler.mailer == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Could not send email. Please check your configuration"})
		return
	}
	if err := handler.mailer.Send(c.Request.Context(), receiver, credentialCheckSubject, credentialCheckBody); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": credentialCheckFailure(err)})
		return
	}
	handler.respond(c, http.StatusOK, gin.H{"message": "Email successfully sent."})
}

// credentialCheckFailure names the failure the way Django names it, for the ones that can be told apart from the error the Go client gives back.
func credentialCheckFailure(err error) string {
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "auth"):
		return "Invalid credentials provided"
	case strings.Contains(message, "dial smtp"), strings.Contains(message, "connection refused"), strings.Contains(message, "no such host"):
		return "Could not connect with the SMTP server."
	case strings.Contains(message, "smtp from"):
		return "From address is invalid."
	case strings.Contains(message, "smtp rcpt"):
		return "All recipient addresses were refused."
	case strings.Contains(message, "timeout"), strings.Contains(message, "deadline exceeded"):
		return "Timeout error while trying to connect to the SMTP server."
	}
	return "Could not send email. Please check your configuration"
}
