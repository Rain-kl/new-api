package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeRelaySelectionPath(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"/pg/chat/completions", "/v1/chat/completions"},
		{"/v1/chat/completions", "/v1/chat/completions"},
		{"/pg/foo", "/v1/foo"},
		{"/pg", "/v1"},
		{"/other/path", "/other/path"},
		{"", ""},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, NormalizeRelaySelectionPath(tc.in), "input %q", tc.in)
	}
}
