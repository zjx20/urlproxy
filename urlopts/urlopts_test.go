package urlopts

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtract(t *testing.T) {
	cases := []struct {
		input    string
		finalUrl string
		opts     string
	}{
		/* extract the first segment of path as the host */
		{input: "", finalUrl: "", opts: ""},
		{input: "/", finalUrl: "/", opts: ""},
		{input: "/hostname/some/path", finalUrl: "/some/path", opts: "uOptHost=hostname"},
		{input: "hostname/some/path", finalUrl: "some/path", opts: "uOptHost=hostname"},
		// don't touch the path if uOptHost is specified
		{input: "/hostname/some/path?uOptHost=otherhost", finalUrl: "/hostname/some/path", opts: "uOptHost=otherhost"},

		/* extract options from query parameters */
		{
			input:    "/hostname/some/path?uOptSocks=off&uOptHeader=Header1:Value1&uOptHeader=Header2:Value2",
			finalUrl: "/some/path",
			opts:     "uOptHeader=Header1:Value1/uOptHeader=Header2:Value2/uOptHost=hostname/uOptSocks=off",
		},
		{
			input:    "/hostname/some/path?uOptSocks=off&foo=bar",
			finalUrl: "/some/path?foo=bar",
			opts:     "uOptHost=hostname/uOptSocks=off",
		},
		// the order of non-option parameters should be reserved
		{
			input:    "/hostname/some/path?z=v1&uOptSocks=off&uOptHeader=Header1:Value1&a=v2&uOptHeader=Header2:Value2",
			finalUrl: "/some/path?z=v1&a=v2",
			opts:     "uOptHeader=Header1:Value1/uOptHeader=Header2:Value2/uOptHost=hostname/uOptSocks=off",
		},
		// unknown options are ignored
		{input: "/hostname/some/path?uOptNonExistOption=should_be_ignored", finalUrl: "/some/path", opts: "uOptHost=hostname"},

		/* extract options from path */
		{
			input:    "/uOptSocks=off/hostname/some/uOptHeader=Header1%3AValue1/path/uOptHeader=Header2%3AValue2",
			finalUrl: "/some/path",
			opts:     "uOptHeader=Header1:Value1/uOptHeader=Header2:Value2/uOptHost=hostname/uOptSocks=off",
		},
		// "%2F" (the escaped form of "/") should be handled correctly
		{
			input:    "/uOptSocks=off/hostname/some/uOptHeader=Header1%3AValue1/path/uOptHeader=Header2%3AVa%2Flue2",
			finalUrl: "/some/path",
			opts:     "uOptHeader=Header1:Value1/uOptHeader=Header2:Va%2Flue2/uOptHost=hostname/uOptSocks=off",
		},

		/* handle for uOptScheme */
		// don't extract host from the path if scheme is not http/https
		{input: "/some/path?uOptScheme=file", finalUrl: "/some/path", opts: "uOptScheme=file"},
		{input: "/hostname/some/path?uOptScheme=https", finalUrl: "/some/path", opts: "uOptHost=hostname/uOptScheme=https"},
	}

	for _, c := range cases {
		u, err := url.Parse(c.input)
		require.NoError(t, err, "input: %s", c.input)
		after, opts := Extract(u)
		assert.Equal(t, c.finalUrl, after.String(), "input: %s", c.input)
		assert.Equal(t, c.opts, SortedOptionPath(opts), "input: %s", c.input)
	}
}
