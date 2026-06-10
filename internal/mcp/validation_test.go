package mcp

import (
	"testing"
)

func TestValidateLabel(t *testing.T) {
	tests := []struct {
		name    string
		label   string
		wantErr bool
	}{
		{"valid short", "test", false},
		{"valid long", "a very long label that is still within limits", false},
		{"empty", "", true},
		{"too long", string(make([]byte, 256)), true},
		{"whitespace only", "   ", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateLabel(tt.label)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateLabel(%q) error = %v, wantErr %v", tt.label, err, tt.wantErr)
			}
		})
	}
}

func TestValidateLimit(t *testing.T) {
	tests := []struct {
		name    string
		limit   int
		wantErr bool
	}{
		{"valid small", 10, false},
		{"valid max", 1000, false},
		{"zero", 0, false}, // defaults handled elsewhere
		{"negative", -1, true},
		{"too large", 1001, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateLimit(tt.limit)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateLimit(%d) error = %v, wantErr %v", tt.limit, err, tt.wantErr)
			}
		})
	}
}

func TestValidateUUID(t *testing.T) {
	tests := []struct {
		name    string
		uuid    string
		wantErr bool
	}{
		{"valid v4", "550e8400-e29b-41d4-a716-446655440000", false},
		{"valid v7", "018ff6d0-7f3a-7e1a-8b2c-3d4e5f6a7b8c", false},
		{"empty", "", true},
		{"too short", "abc", true},
		{"invalid chars", "550e8400-e29b-41d4-a716-44665544000g", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateUUID(tt.uuid)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateUUID(%q) error = %v, wantErr %v", tt.uuid, err, tt.wantErr)
			}
		})
	}
}
