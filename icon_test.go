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

// L'icona dell'exe (lollipop.ico -> rsrc_windows_amd64.syso) e' disegnata dallo stesso codice dell'icona della
// traybar, in rosso. Il test verifica che il file nel repo corrisponda al codice; per rigenerarlo: go generate.
func TestAppIcon(t *testing.T) {
	var pngs [][]byte
	sizes := []int{16, 20, 24, 32, 48, 64, 128, 256}
	for _, n := range sizes {
		var buf bytes.Buffer
		if err := png.Encode(&buf, lollipopImage(color.RGBA{255, 0, 0, 255}, n)); err != nil {
			t.Fatal(err)
		}
		pngs = append(pngs, buf.Bytes())
	}
	// Formato ICO: intestazione, una voce di 16 byte per immagine, poi le immagini (PNG, ammesso da Vista in poi).
	var ico bytes.Buffer
	le := func(v ...any) {
		for _, x := range v {
			_ = binary.Write(&ico, binary.LittleEndian, x)
		}
	}
	le(uint16(0), uint16(1), uint16(len(sizes)))
	offset := 6 + 16*len(sizes)
	for i, n := range sizes {
		le(uint8(n%256), uint8(n%256), uint8(0), uint8(0), uint16(1), uint16(32), uint32(len(pngs[i])), uint32(offset)) // 256 si scrive 0
		offset += len(pngs[i])
	}
	for _, p := range pngs {
		ico.Write(p)
	}

	if *update {
		if err := os.WriteFile("lollipop.ico", ico.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	if old, err := os.ReadFile("lollipop.ico"); err != nil || !bytes.Equal(old, ico.Bytes()) {
		t.Error("lollipop.ico non corrisponde al disegno in ui.go: rigenera con go generate")
	}
}
