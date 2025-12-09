package notify

import (
	"fmt"
	"strings"

	"github.com/gen2brain/beeep"
	"github.com/sleuth-io/skills/internal/logger"
)

func init() {
	// Set the app name for notifications
	beeep.AppName = "Skills"
}

// Send sends a desktop notification
// Falls back gracefully if notifications aren't available
func Send(title, message string) {
	log := logger.Get()

	err := beeep.Notify(title, message, "")
	if err != nil {
		// Log the error but don't fail - notifications are a nice-to-have
		log.Debug("failed to send desktop notification", "error", err, "title", title)
	} else {
		log.Debug("desktop notification sent", "title", title)
	}
}

// ArtifactInfo contains information about an installed artifact
type ArtifactInfo struct {
	Name string
	Type string
}

// SendArtifactNotification sends a notification with artifact details
func SendArtifactNotification(artifacts []ArtifactInfo) {
	title := "Installed"
	var message string

	if len(artifacts) == 0 {
		return // Don't send notification if nothing installed
	} else if len(artifacts) <= 3 {
		// List individual artifacts
		for _, art := range artifacts {
			lowerType := strings.ToLower(art.Type)
			message += fmt.Sprintf("- The %s %s\n", art.Name, lowerType)
		}
		// Remove trailing newline
		message = strings.TrimSuffix(message, "\n")
	} else {
		// Show first 3 and count remaining
		for i := 0; i < 3; i++ {
			lowerType := strings.ToLower(artifacts[i].Type)
			message += fmt.Sprintf("- The %s %s\n", artifacts[i].Name, lowerType)
		}
		remaining := len(artifacts) - 3
		message += fmt.Sprintf("and %d more", remaining)
	}

	Send(title, message)
}
