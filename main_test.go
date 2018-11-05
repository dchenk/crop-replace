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
		{"\tabc_deabc_dekiabc_def", "abc_de", []int{1, 7, 15}},
	}
	for i, tc := range cases {
		t.Run("case_"+strconv.Itoa(i), func(t *testing.T) {
			got := stringIndexes(tc.s, tc.substr)
			if len(got) != len(tc.indexes) {
				t.Fatalf("got %v but expected %v", got, tc.indexes)
			}
			for j := range got {
				if got[j] != tc.indexes[j] {
					t.Errorf("got %v but expected %v", got, tc.indexes)
				}
			}
		})
	}
}

func TestReplaceCrops(t *testing.T) {
	atts := []attachment{
		{
			fileName: "abc.png", ext: ".png", crops: nil,
		},
		{
			fileName: "bcd.png", ext: ".png",
			crops: []crop{
				{"200x180", 200, 180},
				{"400x320", 400, 320},
			},
		},
		{
			fileName: "rjj.jpeg", ext: ".jpeg",
			crops: []crop{
				{"600x450", 600, 450},
			},
		},
		{
			fileName: "rrrr-aa.png", ext: ".png",
			crops: []crop{
				{"200x180", 200, 180},
			},
		},
	}
	cases := []struct {
		original string
		files    []attachment
		desired  string
	}{
		{"abc.png", atts, "abc.png"},                                         // No replacement needed
		{"<img src='abc.png'>", atts, "<img src='abc.png'>"},                 // No replacement needed
		{"abc-400x300.png", atts, "abc.png"},                                 // Default to un-cropped
		{"bcd-30x15.png", atts, "bcd.png"},                                   // Default to un-cropped
		{"bcd-210x195.png", atts, "bcd-200x180.png"},                         // Use close variant
		{"bcd-520x305.png", atts, "bcd-400x320.png"},                         // Use close variant (30% wider)
		{"jkljk-210x195.png", atts, "jkljk-210x195.png"},                     // No matching attachment
		{"HELLO WORLD bcd-210x195.png", atts, "HELLO WORLD bcd-200x180.png"}, // Ignore surroundings
		{"Hi: bcd-210x195.png\tText...", atts, "Hi: bcd-200x180.png\tText..."},
		{"bcd-210x195.png\tText...", atts, "bcd-200x180.png\tText..."},
	}
	for i, tc := range cases {
		t.Run("case_"+strconv.Itoa(i), func(t *testing.T) {
			got := replaceCrops(tc.original, tc.files)
			if got != tc.desired {
				t.Errorf("got\n\t%v\nbut expected\n\t%v", got, tc.desired)
			}
		})
	}
}

func TestFindSuitableCrop(t *testing.T) {
	cases := []struct {
		inPost       *crop
		haveInBucket []crop
		good         bool
		okDiff       int
	}{
		{
			inPost: &crop{"500x450", 500, 450},
			haveInBucket: []crop{
				{"500x450", 500, 450},
				{"400x330", 400, 330},
			},
			good:   true,
			okDiff: -1,
		},
		{
			inPost: &crop{"500x450", 500, 450},
			haveInBucket: []crop{
				{"510x460", 510, 460},
				{"400x330", 400, 330},
			},
			good:   false,
			okDiff: 0,
		},
	}
	for i, tc := range cases {
		t.Run("case_"+strconv.Itoa(i), func(t *testing.T) {
			good, okDiff := findSuitableCrop(tc.inPost, tc.haveInBucket)
			if good != tc.good {
				t.Errorf("got %v but expected %v for the bool", good, tc.good)
			}
			if okDiff != tc.okDiff {
				t.Errorf("got %v but expected %v for the int", okDiff, tc.okDiff)
			}
		})
	}
}
