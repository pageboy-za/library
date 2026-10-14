package library

import (
	"errors"
	"strings"
	"unicode"
)

var ErrISBN = errors.New("Enter a valid book ISBN-10 or ISBN-13 (978 or 979)")

func ISBN(raw string) (string, error) {
	s := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || r == '-' {
			return -1
		}
		return unicode.ToUpper(r)
	}, raw)
	if len(s) == 10 {
		sum := 0
		for i, c := range s {
			v := int(c - '0')
			if c == 'X' && i == 9 {
				v = 10
			} else if c < '0' || c > '9' {
				return "", ErrISBN
			}
			sum += (10 - i) * v
		}
		if sum%11 != 0 {
			return "", ErrISBN
		}
		s = "978" + s[:9]
		sum = 0
		for i, c := range s {
			w := 1
			if i%2 == 1 {
				w = 3
			}
			sum += int(c-'0') * w
		}
		s += string(rune('0' + (10-sum%10)%10))
	}
	if len(s) != 13 || (!strings.HasPrefix(s, "978") && !strings.HasPrefix(s, "979")) {
		return "", ErrISBN
	}
	sum := 0
	for i, c := range s {
		if c < '0' || c > '9' {
			return "", ErrISBN
		}
		w := 1
		if i%2 == 1 {
			w = 3
		}
		sum += int(c-'0') * w
	}
	if sum%10 != 0 {
		return "", ErrISBN
	}
	return s, nil
}
