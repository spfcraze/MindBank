package mcp

import (
	"fmt"
	"regexp"
	"strings"
)

var uuidRegex = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// validateLabel checks label is non-empty and within reasonable bounds.
func validateLabel(label string) error {
	label = strings.TrimSpace(label)
	if label == "" {
		return fmt.Errorf("label is required")
	}
	if len(label) > 255 {
		return fmt.Errorf("label exceeds maximum length of 255 characters")
	}
	return nil
}

// validateLimit ensures limit is within acceptable bounds.
func validateLimit(limit int) error {
	if limit < 0 {
		return fmt.Errorf("limit cannot be negative")
	}
	if limit > 1000 {
		return fmt.Errorf("limit exceeds maximum of 1000")
	}
	return nil
}

// validateUUID checks if a string is a valid UUID format.
func validateUUID(uuid string) error {
	if uuid == "" {
		return fmt.Errorf("UUID is required")
	}
	if !uuidRegex.MatchString(uuid) {
		return fmt.Errorf("invalid UUID format")
	}
	return nil
}
