package main

import (
	"strconv"
	"testing"
)

func TestIsCropVariant(t *testing.T) {
	cases := []struct {
		objectNameStart, possibleCrop, ext string
		isCrop                             bool
	}{
		{"content/my-photo", "content/my-photo-600x340.png", "png", true},
	}
	for i, tc := range cases {
		t.Run("case_"+strconv.Itoa(i), func(t *testing.T) {
			got := isCropVariant(tc.objectNameStart, tc.possibleCrop, tc.ext)
			if got != tc.isCrop {
				t.Errorf("got %v but expected %v", got, tc.isCrop)
			}
		})
	}
}
