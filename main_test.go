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
		{"-600x340.png_more-stuff", ".png", &crop{"600x340", 600, 340}},
		{"-500x370.jpg'=anything-can-follow", ".jpg", &crop{"500x370", 500, 370}},
		{"-x.jpg", ".jpg", nil},
		{"-.png", ".png", nil},
		{"-850x1080x900.jpg", ".jpg", nil},
		{"-850x1080.900.jpg", ".jpg", nil},
		{"-file-other.jpeg", ".jpeg", nil},
		{"-1024x768.jpeg", ".png", nil},
		{"_1024x768.jpeg", ".jpeg", nil},
		{"_something-else.jpg", ".jpg", nil},
		{"234x424.png", ".png", nil},
		{".jpeg", ".jpeg", nil},
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

func TestStringIndexes(t *testing.T) {
	cases := []struct {
		s, substr string
		indexes   []int
	}{
		{"abc", "n", nil},
		{"abc", "a", []int{0}},
		{"abac", "a", []int{0, 2}},
		{"aaabc", "aa", []int{0}},
		{"aabgaa", "aa", []int{0, 4}},
		{"rabcabcd", "abc", []int{1, 4}},
		{"rrabcaabcd", "abc", []int{2, 6}},
		{"rrabaabcd", "abc", []int{5}},
	}
	for i, tc := range cases {
		t.Run("case_"+strconv.Itoa(i), func(t *testing.T) {
			got := stringIndexes(tc.s, tc.substr)
			if len(got) != len(tc.indexes) {
				t.Errorf("got %v but expected %v", got, tc.indexes)
			}
			for j := range got {
				if got[j] != tc.indexes[j] {
					t.Errorf("got %v but expected %v", got, tc.indexes)
				}
			}
		})
	}
}
