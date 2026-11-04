package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompareTargets(t *testing.T) {
	targets, err := compareTargets(20, 7)
	require.NoError(t, err)
	assert.Equal(t, []target{
		{20, "photos with the shutter moving"},
		{7, "electronic shutter photos"},
		{27, "all photos"},
	}, targets)

	targets, err = compareTargets(20, 0)
	require.NoError(t, err)
	assert.Equal(t, []target{{20, labelPhotosTaken}}, targets)

	_, err = compareTargets(0, 7)
	require.Error(t, err)

	_, err = compareTargets(7, 7)
	require.Error(t, err, "equal counts couldn't say which a match counts")
}

func TestDescribeBytesFindsEachTarget(t *testing.T) {
	targets, err := compareTargets(20, 7)
	require.NoError(t, err)

	// Three little endian 32 bit counters: +20, +7 and +27.
	before := "64000000" + "c8000000" + "2c010000"
	after := "78000000" + "cf000000" + "47010000"

	lines, hits := describeBytes(before, after, targets)

	assert.Contains(t, lines, "32 bits little endian at byte 0: 100 -> 120   <== photos with the shutter moving")
	assert.Contains(t, lines, "32 bits little endian at byte 4: 200 -> 207   <== electronic shutter photos")
	assert.Contains(t, lines, "32 bits little endian at byte 8: 300 -> 327   <== all photos")
	assert.Equal(t, len(lines)-1, hits)
}

func TestDescribeBytesFindsABigEndianCounter(t *testing.T) {
	lines, hits := describeBytes("00000064", "00000078", []target{{20, labelPhotosTaken}})

	assert.Contains(t, lines, "32 bits big endian at byte 0: 100 -> 120   <== "+labelPhotosTaken)
	assert.Positive(t, hits)
}

func TestDescribeBytesIgnoresOtherChanges(t *testing.T) {
	lines, hits := describeBytes("64000000", "65000000", []target{{20, labelPhotosTaken}})

	assert.Equal(t, []string{"changed (4 -> 4 bytes)"}, lines)
	assert.Zero(t, hits)
}

func TestDescribeD167ShowsPlainWords(t *testing.T) {
	ev := &probedEvents{Records: []probedRecord{
		{Key: d167Key, Data: "10000000" + "01000000" + "a00f0000" + "b8880000"},
	}}

	assert.Equal(t, "0xd167 word 1: 16, word 2: 1, word 3: 4000, word 4: 35000", describeD167(ev))
	assert.Equal(t, "0xd167: not reported", describeD167(&probedEvents{}))
}

func TestEventTableKeepsTheLastValue(t *testing.T) {
	const key = "prop 0xd101"

	table := eventTable(&probedEvents{Records: []probedRecord{
		{Key: key, Data: "01"},
		{Key: key, Data: "02"},
	}})

	assert.Equal(t, map[string]string{key: "02"}, table)
}
