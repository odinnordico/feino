package chat

import (
	"testing"
)

func TestModel_HistoryTruncation(t *testing.T) {
	m := Model{
		messages: make([]renderedMessage, maxHistoryMessages+10),
	}

	m = m.truncateHistory()

	if len(m.messages) != maxHistoryMessages {
		t.Errorf("expected %d messages after truncation, got %d", maxHistoryMessages, len(m.messages))
	}
}
