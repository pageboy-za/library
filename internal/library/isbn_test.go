package library

import "testing"

func TestISBN(t *testing.T) {
	for _, s := range []string{"0-14-032872-6", "9780140328721", " 978 0140328721 "} {
		got, e := ISBN(s)
		if e != nil || got != "9780140328721" {
			t.Fatalf("%q => %q %v", s, got, e)
		}
	}
	for _, s := range []string{"9780140328722", "1234567890128", "014032872X", "", "9780140328721 90000"} {
		if _, e := ISBN(s); e == nil {
			t.Fatalf("accepted %q", s)
		}
	}
}
