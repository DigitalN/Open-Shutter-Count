package cli

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/urfave/cli/v3"
)

func takesATestedList(flags []cli.Flag) bool {
	return slices.ContainsFunc(flags, func(f cli.Flag) bool {
		return slices.Contains(f.Names(), "tested-list")
	})
}

// Reading and models take --tested-list. The commands Find Shutter
// Counter.command runs (list, probe, compare) don't, nor does decode.
func TestOnlyReadingAndModelsTakeATestedList(t *testing.T) {
	app := NewApp()
	assert.True(t, takesATestedList(app.Flags), "reading with no command")

	for _, c := range app.Commands {
		want := c.Name == "read" || c.Name == "models"
		assert.Equal(t, want, takesATestedList(c.Flags), c.Name)
	}
}
