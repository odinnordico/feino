package chat

import (
	"os"
	"testing"

	"github.com/odinnordico/feino/internal/i18n"
)

func TestMain(m *testing.M) {
	i18n.Init("en")
	os.Exit(m.Run())
}
