//go:build wasm

package main

import (
	"bgbc/pkg/gbc"
	"encoding/base64"
	"fmt"
	"syscall/js"
	"time"
	"unsafe"
)

var SCALE = 1

type jsFrontend struct {
	console *gbc.Console

	width, heigh int
	img          []uint8

	start time.Time
	ticks int
}

func (f *jsFrontend) NotifyAudioSample(l, r int8) {}

func (f *jsFrontend) SetPixel(x, y int, c uint32) {
	var r, g, b, a uint8
	r = uint8((c >> 24) & 0xFF)
	g = uint8((c >> 16) & 0xFF)
	b = uint8((c >> 8) & 0xFF)
	a = uint8(c & 0xFF)

	for i := 0; i < SCALE; i++ {
		for j := 0; j < SCALE; j++ {
			f.img[(y*SCALE+i)*SCALE*f.width*4+x*SCALE*4+0+j*4] = r
			f.img[(y*SCALE+i)*SCALE*f.width*4+x*SCALE*4+1+j*4] = g
			f.img[(y*SCALE+i)*SCALE*f.width*4+x*SCALE*4+2+j*4] = b
			f.img[(y*SCALE+i)*SCALE*f.width*4+x*SCALE*4+3+j*4] = a
		}
	}
}

func (f *jsFrontend) CommitScreen() {}

func (f *jsFrontend) ExchangeSerial(sb, sc uint8) (uint8, uint8) {
	return 0, 0
}

func base64Decode(str string) (string, bool) {
	data, err := base64.StdEncoding.DecodeString(str)
	if err != nil {
		return "", true
	}
	return string(data), false
}

var gPl *jsFrontend = nil

func emulator_init(this js.Value, args []js.Value) interface{} {
	fmt.Println("entering mainLoop")

	if len(args) != 1 {
		fmt.Println("!Err: missing ROM")
		return 0
	}

	data, err := base64.StdEncoding.DecodeString(args[0].String())
	if err != nil {
		fmt.Printf("!Err: base64.StdEncoding.DecodeString failed [%s]\n", err)
		return 0
	}
	gPl = &jsFrontend{
		width: 160,
		heigh: 144,
		img:   make([]uint8, 160*144*4*SCALE*SCALE),
	}

	console, err := gbc.MakeConsole(data, gPl)
	if err != nil {
		fmt.Printf("!Err: gbc.MakeConsole failed [%s]\n", err)
		return 0
	}
	gPl.console = console
	return unsafe.Pointer(&gPl.img)
}

func emulator_step(this js.Value, args []js.Value) interface{} {
	if gPl == nil {
		return false
	}
	gPl.start = time.Now()
	gPl.ticks += gPl.console.Step()
	return true
}

func emulator_start_timer(this js.Value, args []js.Value) interface{} {
	if gPl == nil {
		return false
	}

	gPl.start = time.Now()
	gPl.ticks = 0
	return true
}

func emulator_end_timer(this js.Value, args []js.Value) interface{} {
	if gPl == nil {
		return false
	}

	elapsed := time.Since(gPl.start)
	if int(elapsed.Milliseconds()) < gPl.console.GetMs(gPl.ticks) {
		return gPl.console.GetMs(gPl.ticks) - int(elapsed.Milliseconds())
	}
	return 0
}

func emulator_notify_input(this js.Value, args []js.Value) interface{} {
	if gPl == nil || len(args) == 0 {
		return false
	}

	gPl.console.Input.BackState.Unserialize(uint8(args[0].Int()))
	return true
}

func emulator_load_sav(this js.Value, args []js.Value) interface{} {
	if gPl == nil || len(args) == 0 {
		return false
	}

	data, err := base64.StdEncoding.DecodeString(args[0].String())
	if err != nil {
		fmt.Printf("!Err base64.StdEncoding.DecodeString failed [%s]\n", err)
		return false
	}

	err = gPl.console.LoadSav(data)
	if err != nil {
		fmt.Printf("!Err gPl.console.LoadSav failed [%s]\n", err)
		return false
	}
	return true
}

func emulator_store_sav(this js.Value, args []js.Value) interface{} {
	if gPl == nil {
		return ""
	}

	data, err := gPl.console.StoreSav()
	if err != nil {
		fmt.Printf("!Err gPl.console.StoreSav failed [%s]\n", err)
		return ""
	}
	return base64.StdEncoding.EncodeToString(data)
}

func main() {
	js.Global().Set("emulator_init", js.FuncOf(emulator_init))
	js.Global().Set("emulator_step", js.FuncOf(emulator_step))
	js.Global().Set("emulator_notify_input", js.FuncOf(emulator_notify_input))
	js.Global().Set("emulator_start_timer", js.FuncOf(emulator_start_timer))
	js.Global().Set("emulator_end_timer", js.FuncOf(emulator_end_timer))
	js.Global().Set("emulator_load_sav", js.FuncOf(emulator_load_sav))
	js.Global().Set("emulator_store_sav", js.FuncOf(emulator_store_sav))

	c := make(chan int) // channel to keep the wasm running, it is not a library as in rust/c/c++, so we need to keep the binary running
	<-c                 // pause the execution so that the resources we create for JS keep available
}
