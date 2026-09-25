package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"image/color"
	"image/png"
	"os"
	"testing"
)

var update = flag.Bool("update", false, "rigenera lollipop.ico")

// Not a real test: it writes lollipop.ico (-> rsrc_windows_amd64.syso) from the tray icon drawing.
// It runs only with -update, i.e. from "go generate".
func TestAppIcon(t *testing.T) {
	if !*update {
		t.Skip("solo con -update (go generate)")
	}
	var pngs [][]byte
	sizes := []int{16, 20, 24, 32, 48, 64, 128, 256}
	for _, n := range sizes {
		var buf bytes.Buffer
		if err := png.Encode(&buf, lollipopImage(color.RGBA{255, 0, 0, 255}, n)); err != nil {
			t.Fatal(err)
		}
		pngs = append(pngs, buf.Bytes())
	}
	// ICO: header, one 16-byte entry per image, then the images (PNG payloads are fine since Vista).
	var ico bytes.Buffer
	le := func(v ...any) {
		for _, x := range v {
			_ = binary.Write(&ico, binary.LittleEndian, x)
		}
	}
	le(uint16(0), uint16(1), uint16(len(sizes)))
	offset := 6 + 16*len(sizes)
	for i, n := range sizes {
		le(uint8(n%256), uint8(n%256), uint8(0), uint8(0), uint16(1), uint16(32), uint32(len(pngs[i])), uint32(offset)) // 256 is written as 0
		offset += len(pngs[i])
	}
	for _, p := range pngs {
		ico.Write(p)
	}

	if err := os.WriteFile("lollipop.ico", ico.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}
