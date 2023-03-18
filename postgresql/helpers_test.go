package postgresql

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFindStringSubmatchMap(t *testing.T) {

	resultMap := findStringSubmatchMap(`(?si).*\$(?P<Body>.*)\$.*`, "aa $somehing_to_extract$ bb")

	assert.Equal(t,
		resultMap,
		map[string]string{
			"Body": "somehing_to_extract",
		},
	)
}
