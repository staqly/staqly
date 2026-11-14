package app

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	ProvidersOnByDefault = true // unset PROVIDER_<ID>_ENABLED means off; most tests are not about that
	os.Exit(m.Run())
}
