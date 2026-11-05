package canon

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// r8D167 is what the maintainer's EOS R8 reported after the 2026-11-04 test.
var r8D167 = []byte{
	0x10, 0, 0, 0, 0x01, 0, 0, 0, 0xd0, 0x07, 0, 0, 0xf0, 0x55, 0, 0,
}

func TestDecodeReleaseCounter(t *testing.T) {
	counts, err := DecodeReleaseCounter(r8D167)
	require.NoError(t, err)

	assert.Equal(t, ReleaseCounter{
		Mechanical: Range{Min: 1001, Max: 2000},
		All:        Range{Min: 21001, Max: 22000},
		Electronic: Range{Min: 19001, Max: 20999},
	}, counts)
}

func TestDecodeReleaseCounterNewCamera(t *testing.T) {
	// libgphoto2's M50 Mark II dump: 1000 and 1000.
	counts, err := DecodeReleaseCounter([]byte{
		0x10, 0, 0, 0, 0x01, 0, 0, 0, 0xe8, 0x03, 0, 0, 0xe8, 0x03, 0, 0,
	})
	require.NoError(t, err)

	assert.Equal(t, Range{Min: 1, Max: 1000}, counts.Mechanical)
	assert.Equal(t, Range{Min: 0, Max: 999}, counts.Electronic)
}

func TestDecodeReleaseCounterRefusesOtherLayouts(t *testing.T) {
	for name, raw := range map[string][]byte{
		"short":               r8D167[:12],
		"other header":        append([]byte{0x10, 0, 0, 0, 0x02, 0, 0, 0}, r8D167[8:]...),
		"not thousands":       {0x10, 0, 0, 0, 0x01, 0, 0, 0, 0xd1, 0x07, 0, 0, 0xf0, 0x55, 0, 0},
		"mechanical over all": {0x10, 0, 0, 0, 0x01, 0, 0, 0, 0xf0, 0x55, 0, 0, 0xd0, 0x07, 0, 0},
	} {
		_, err := DecodeReleaseCounter(raw)
		assert.Error(t, err, name)
	}
}
