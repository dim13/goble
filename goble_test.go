package goble

import "testing"

func TestPropertyStringer(t *testing.T) {
	testCases := []struct {
		p Property
		s string
	}{
		{0, ""},
		{Read | Indicate, "read indicate"},
		{Write | WriteWithoutResponse, "writeWithoutResponse write"},
	}
	for _, tc := range testCases {
		if tc.p.String() != tc.s {
			t.Errorf("got %v, want %v", tc.p, tc.s)
		}
	}
}
