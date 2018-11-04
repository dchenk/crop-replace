package main

import (
	"strconv"
	"testing"
)

func TestGetCropVariant(t *testing.T) {
	cases := []struct {
		fileNameEnd, ext string
		dimensions       *crop
	}{
		{"-600x340.png", ".png", &crop{"600x340", 600, 340}},
		{"-1024x768.jpeg", ".jpeg", &crop{"1024x768", 1024, 768}},
		{"_1024x768.jpeg", ".jpeg", nil},
		{"-850x1080x900.jpg", ".jpg", nil},
		{"-file-other.jpeg", ".jpeg", nil},
		{"-1024x768.jpeg", ".png", nil},
		{"_something-else.jpg", ".jpg", nil},
		{"234x424.png", ".png", nil},
	}
	for i, tc := range cases {
		t.Run("case_"+strconv.Itoa(i), func(t *testing.T) {
			got := getCropVariant(tc.fileNameEnd, tc.ext)
			if got == nil && tc.dimensions != nil || got != nil && tc.dimensions == nil {
				t.Errorf("got %v but expected %v", got, tc.dimensions)
			}
			if got != nil && (got.str != tc.dimensions.str ||
				got.width != tc.dimensions.width || got.height != tc.dimensions.height) {
				t.Errorf("got %v but expected %v", got, tc.dimensions)
			}
		})
	}
}
