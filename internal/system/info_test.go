package system

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseCPUModel(t *testing.T) {
	t.Parallel()

	data := `processor	: 0
vendor_id	: GenuineIntel
model name	: Intel(R) Core(TM) i9-13900K
cpu MHz		: 3000.000
processor	: 1
model name	: Intel(R) Core(TM) i9-13900K
`

	model := parseCPUModel(data)
	assert.Equal(t, "Intel(R) Core(TM) i9-13900K", model)
}

func TestParseCPUModel_Empty(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "", parseCPUModel(""))
}

func TestParseMemTotal(t *testing.T) {
	t.Parallel()

	data := `MemTotal:       65536000 kB
MemFree:        32768000 kB
MemAvailable:   50000000 kB
`

	mb := parseMemTotal(data)
	assert.Equal(t, uint64(64000), mb) // 65536000 / 1024
}

func TestParseMemTotal_Empty(t *testing.T) {
	t.Parallel()

	assert.Equal(t, uint64(0), parseMemTotal(""))
}

func TestParseDFOutput(t *testing.T) {
	t.Parallel()

	output := `     1G-blocks      Used     Avail Use%
          500G      200G      300G  40%`

	disk, err := parseDFOutput(output)
	assert.NoError(t, err)
	assert.Equal(t, float64(500), disk.TotalGB)
	assert.Equal(t, float64(200), disk.UsedGB)
	assert.Equal(t, float64(300), disk.AvailableGB)
	assert.Equal(t, "40%", disk.UsedPercent)
}

func TestParseDFOutput_Invalid(t *testing.T) {
	t.Parallel()

	_, err := parseDFOutput("single line")
	assert.Error(t, err)
}
