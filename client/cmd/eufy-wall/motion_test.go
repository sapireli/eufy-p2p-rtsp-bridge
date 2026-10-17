package main

import (
	"eufy-wall/internal/layout"
	"reflect"
	"testing"
)

func TestMotionMatchesEveryDisplayedCameraAndNotPriorBinding(t *testing.T) {
	tiles := []layout.Placed{{Index: 0, Camera: "FRONT"}, {Index: 1, Camera: "GARAGE"}, {Index: 2}, {Index: 3, Camera: "FRONT"}}
	showing := map[int]string{0: "FRONT", 1: "GARAGE", 2: "FRONT", 3: ""}
	if got := motionTileIndices(tiles, showing, "FRONT"); !reflect.DeepEqual(got, []int{0, 2}) {
		t.Fatal(got)
	}
	showing[2] = "GARAGE"
	if got := motionTileIndices(tiles, showing, "FRONT"); !reflect.DeepEqual(got, []int{0}) {
		t.Fatal(got)
	}
	if got := motionTileIndices(tiles, showing, ""); len(got) != 0 {
		t.Fatal(got)
	}
}
