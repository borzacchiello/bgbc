package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
	"testing"

	"borzGBC/pkg/gbc"
)

/*
 * Test Frontend
 */

type ImageVideoDriver struct {
	backImg  *image.RGBA
	frontImg *image.RGBA
	num      int

	SerialFunction func(sb, sc uint8) (uint8, uint8)
}

func MkImageVideoDriver() *ImageVideoDriver {
	res := &ImageVideoDriver{}

	upLeft := image.Point{0, 0}
	lowRight := image.Point{160, 144}
	res.backImg = image.NewRGBA(image.Rectangle{upLeft, lowRight})
	res.frontImg = image.NewRGBA(image.Rectangle{upLeft, lowRight})

	return res
}

func (d *ImageVideoDriver) SetPixel(x, y int, c uint32) {
	var r, g, b, a uint8
	r = uint8((c >> 24) & 0xFF)
	g = uint8((c >> 16) & 0xFF)
	b = uint8((c >> 8) & 0xFF)
	a = uint8(c & 0xFF)

	d.backImg.SetRGBA(x, y, color.RGBA{r, g, b, a})
}

func (d *ImageVideoDriver) CommitScreen() {
	d.frontImg = d.backImg
	d.num += 1
}

func (d *ImageVideoDriver) SaveScreen(path string) {
	f, _ := os.Create(path)
	defer f.Close()

	png.Encode(f, d.frontImg)
}

func (pl *ImageVideoDriver) NotifyAudioSample(l, r int8) {
	// Ignore audio
}

func (pl *ImageVideoDriver) GetCurrentImage() *image.RGBA {
	return pl.frontImg
}

func (pl *ImageVideoDriver) ExchangeSerial(sb, sc uint8) (uint8, uint8) {
	if pl.SerialFunction != nil {
		return pl.SerialFunction(sb, sc)
	}
	return 0, 0
}

func imagesAreEqual(img1, img2 image.Image) bool {
	if img1.Bounds() != img2.Bounds() {
		return false
	}

	for x := img1.Bounds().Min.X; x < img1.Bounds().Max.X; x++ {
		for y := img1.Bounds().Min.Y; y < img1.Bounds().Max.Y; y++ {
			if img1.At(x, y) != img2.At(x, y) {
				return false
			}
		}
	}
	return true
}

/*
 * Utility functions
 */

func runRomTestWithSerial(t *testing.T, test string, frames int, serialFunction func(sb, sc uint8) (uint8, uint8)) {
	testName := strings.Split(test, ".")[0]
	pl := MkImageVideoDriver()
	pl.SerialFunction = serialFunction
	romPath := fmt.Sprintf("test/data/%s", test)
	rom, err := os.ReadFile(romPath)
	if err != nil {
		t.Error(err)
		return
	}
	console, err := gbc.MakeConsole(rom, pl)
	if err != nil {
		t.Error("Unable to create console")
		return
	}

	console.PPU.GBPalette = gbc.GB_PALETTE_GREY
	for console.PPU.FrameCount < frames {
		console.Step()
	}

	pl.SaveScreen(fmt.Sprintf("test/data/%s.result.png", testName))

	f, err := os.Open(fmt.Sprintf("test/data/%s.png", testName))
	if err != nil {
		t.Error("Unable to read expected screen")
		return
	}
	defer f.Close()
	expected, err := png.Decode(f)
	if err != nil {
		t.Error("Unable to decode expected screen")
		return
	}

	if !imagesAreEqual(expected, pl.GetCurrentImage()) {
		t.Fail()
	}
}

/*
 * Real tests
 */

func runRomTest(t *testing.T, test string, frames int) {
	runRomTestWithSerial(t, test, frames, nil)
}

func TestBlargCpuInstrs(t *testing.T) {
	runRomTest(t, "Blargg/cpu_instrs.gb", 4000)
}

func TestBlargInstrTiming(t *testing.T) {
	runRomTest(t, "Blargg/instr_timing.gb", 1000)
}

func TestDMGAcid2(t *testing.T) {
	runRomTest(t, "MattCurrie/dmg-acid2.gb", 1000)
}

func TestCGBAcid2(t *testing.T) {
	runRomTest(t, "MattCurrie/cgb-acid2.gbc", 1000)
}

func TestMooneyeOamDma_basic(t *testing.T) {
	runRomTest(t, "Mooneye/oam_dma/basic.gb", 1000)
}

func TestMooneyeOamDma_reg_read(t *testing.T) {
	runRomTest(t, "Mooneye/oam_dma/reg_read.gb", 1000)
}

func TestMooneyeBits_mem_oam(t *testing.T) {
	runRomTest(t, "Mooneye/bits/mem_oam.gb", 1000)
}

func TestMooneyeBits_reg_f(t *testing.T) {
	runRomTest(t, "Mooneye/bits/reg_f.gb", 1000)
}

func TestMooneye_sprite_priority(t *testing.T) {
	runRomTest(t, "Mooneye/sprite_priority.gb", 1000)
}

func TestMooneyeTimer_div_write(t *testing.T) {
	runRomTest(t, "Mooneye/timer/div_write.gb", 1000)
}

func TestMooneyeTimer_tim00(t *testing.T) {
	runRomTest(t, "Mooneye/timer/tim00.gb", 1000)
}

func TestMooneyeTimer_tim01_div_trigger(t *testing.T) {
	runRomTest(t, "Mooneye/timer/tim01_div_trigger.gb", 1000)
}

func TestMooneyeTimer_tim10(t *testing.T) {
	runRomTest(t, "Mooneye/timer/tim10.gb", 1000)
}

func TestMooneyeTimer_tim11(t *testing.T) {
	runRomTest(t, "Mooneye/timer/tim11.gb", 1000)
}

func TestMooneyeTimer_tima_reload(t *testing.T) {
	runRomTest(t, "Mooneye/timer/tima_reload.gb", 1000)
}

func TestMooneyeMbc1_bits_bank1(t *testing.T) {
	runRomTest(t, "Mooneye/mbc1/bits_bank1.gb", 1000)
}

func TestMooneyeMbc1_bits_bank2(t *testing.T) {
	runRomTest(t, "Mooneye/mbc1/bits_bank2.gb", 1000)
}

func TestMooneyeMbc1_bits_mode(t *testing.T) {
	runRomTest(t, "Mooneye/mbc1/bits_mode.gb", 1000)
}

func TestMooneyeMbc1_bits_ramg(t *testing.T) {
	runRomTest(t, "Mooneye/mbc1/bits_ramg.gb", 1000)
}

func TestMooneyeMbc1_ram_64kb(t *testing.T) {
	runRomTest(t, "Mooneye/mbc1/ram_64kb.gb", 1000)
}

func TestMooneyeMbc1_ram_256kb(t *testing.T) {
	runRomTest(t, "Mooneye/mbc1/ram_256kb.gb", 1000)
}

func TestMooneyeMbc1_rom_1Mb(t *testing.T) {
	runRomTest(t, "Mooneye/mbc1/rom_1Mb.gb", 1000)
}

func TestMooneyeMbc1_rom_2Mb(t *testing.T) {
	runRomTest(t, "Mooneye/mbc1/rom_2Mb.gb", 1000)
}

func TestMooneyeMbc1_rom_4Mb(t *testing.T) {
	runRomTest(t, "Mooneye/mbc1/rom_4Mb.gb", 1000)
}

func TestMooneyeMbc1_rom_8Mb(t *testing.T) {
	runRomTest(t, "Mooneye/mbc1/rom_8Mb.gb", 1000)
}

func TestMooneyeMbc1_rom_16Mb(t *testing.T) {
	runRomTest(t, "Mooneye/mbc1/rom_16Mb.gb", 1000)
}

func TestMooneyeMbc1_rom_512Kb(t *testing.T) {
	runRomTest(t, "Mooneye/mbc1/rom_512kb.gb", 1000)
}

func TestMooneyeMbc5_rom_1Mb(t *testing.T) {
	runRomTest(t, "Mooneye/mbc5/rom_1Mb.gb", 1000)
}

func TestMooneyeMbc5_rom_2Mb(t *testing.T) {
	runRomTest(t, "Mooneye/mbc5/rom_2Mb.gb", 1000)
}

func TestMooneyeMbc5_rom_4Mb(t *testing.T) {
	runRomTest(t, "Mooneye/mbc5/rom_4Mb.gb", 1000)
}

func TestMooneyeMbc5_rom_8Mb(t *testing.T) {
	runRomTest(t, "Mooneye/mbc5/rom_8Mb.gb", 1000)
}

func TestMooneyeMbc5_rom_16Mb(t *testing.T) {
	runRomTest(t, "Mooneye/mbc5/rom_16Mb.gb", 1000)
}

func TestMooneyeMbc5_rom_32Mb(t *testing.T) {
	runRomTest(t, "Mooneye/mbc5/rom_32Mb.gb", 1000)
}

func TestMooneyeMbc5_rom_64Mb(t *testing.T) {
	runRomTest(t, "Mooneye/mbc5/rom_64Mb.gb", 1000)
}

func TestMooneyeMbc5_rom_512Kb(t *testing.T) {
	runRomTest(t, "Mooneye/mbc5/rom_512kb.gb", 1000)
}

func TestMooneyeDaa(t *testing.T) {
	runRomTest(t, "Mooneye/instr/daa.gb", 1000)
}

func TestMooneyeIePush(t *testing.T) {
	runRomTest(t, "Mooneye/interrupts/ie_push.gb", 1000)
}

func TestMooneyeIntr_1_2(t *testing.T) {
	runRomTest(t, "Mooneye/interrupts/intr_1_2_timing-GS.gb", 1000)
}

func TestMooneyeIntr_2_0(t *testing.T) {
	runRomTest(t, "Mooneye/interrupts/intr_2_0_timing.gb", 1000)
}

func TestMooneyeIntrStatIrq(t *testing.T) {
	runRomTest(t, "Mooneye/interrupts/stat_irq_blocking.gb", 1000)
}

func TestSerial(t *testing.T) {
	fistRun := true
	runRomTestWithSerial(t, "Serial/gb-link.gb", 1000, func(sb, sc uint8) (uint8, uint8) {
		if sc == 0x80 {
			if fistRun {
				fistRun = false
				return 2, 0x81
			} else {
				return 0xaa, 0x81
			}
		}
		return 0, 0
	})
}
