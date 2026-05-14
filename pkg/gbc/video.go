package gbc

import "encoding/gob"

type PpuMode int

const (
	ACCESS_OAM  PpuMode = 2
	ACCESS_VRAM PpuMode = 3
	HBLANK      PpuMode = 0
	VBLANK      PpuMode = 1
)

const (
	CLOCKS_ACCESS_OAM  int = 80
	CLOCKS_ACCESS_VRAM int = 172
	CLOCKS_HBLANK      int = 204
	CLOCKS_VBLANK      int = 456
)

const (
	SCREEN_WIDTH  int = 160
	SCREEN_HEIGHT int = 144
	MAX_SPRITES   int = 10
)

const (
	TILE_SET_ZERO_ADDRESS uint16 = 0x8000
	TILE_SET_ONE_ADDRESS  uint16 = 0x8800
	TILE_MAP_ZERO_ADDRESS uint16 = 0x9800
	TILE_MAP_ONE_ADDRESS  uint16 = 0x9C00
)

const (
	BG_MAP_SIZE    = 256
	TILE_WIDTH_PX  = 8
	TILE_HEIGHT_PX = 8
	TILES_PER_LINE = 32
	TILE_BYTES     = 16
	SPRITE_BYTES   = 4
)

const (
	GB_PALETTE_GREY  = 1
	GB_PALETTE_GREEN = 2
)

type PixelInfo struct {
	isNotTransparent bool
	bgAttrBitNotSet  bool
	spritePixel      bool
	spriteX          int
}

type Palette struct {
	colors [4]uint32
}

type Tile struct {
	Pixels [8][8]uint8
}

type Sprite struct {
	Ready bool
	x, y  int
	tile  uint8

	options uint8
}

func (sprite *Sprite) cgbPaletteNumber() uint8 {
	return sprite.options & 7
}

func (sprite *Sprite) cgbVramBank() uint8 {
	return (sprite.options >> 3) & 1
}

func (sprite *Sprite) paletteNumber() uint8 {
	return (sprite.options >> 4) & 1
}

func (sprite *Sprite) xFlip() uint8 {
	return (sprite.options >> 5) & 1
}

func (sprite *Sprite) yFlip() uint8 {
	return (sprite.options >> 6) & 1
}

func (sprite *Sprite) renderPriority() bool {
	return (sprite.options>>7)&1 == 1
}

type Ppu struct {
	frontend Frontend
	GBC      *Console

	VRAM     [2][0x2000]uint8
	VRAMBank uint8
	OamRAM   [0xA0]uint8

	// CGB Palette RAM
	CRAMBg         [0x40]uint8
	CRAMBgAddr     uint8
	CRAMBgAutoInc  bool
	CRAMObj        [0x40]uint8
	CRAMObjAddr    uint8
	CRAMObjAutoInc bool

	// VRAM tiles and sprites rearranged here
	tiles   [1024]Tile
	sprites [40]Sprite

	STAT, LCDC      uint8
	SCY, SCX        uint8
	LY, LYC, WY, WX uint8
	BGP             uint8
	OBP0, OBP1      uint8
	WindowScanline  uint8

	GBPalette uint8

	// A clone of the screen
	screen [SCREEN_WIDTH][SCREEN_HEIGHT]PixelInfo

	Mode       PpuMode
	CycleCount int
	FrameCount int

	wasDisplayEnabled         bool
	wasModeInterruptTriggered bool
}

func (ppu *Ppu) Save(encoder *gob.Encoder) {
	panicIfErr(encoder.Encode(ppu.VRAM))
	panicIfErr(encoder.Encode(ppu.VRAMBank))
	panicIfErr(encoder.Encode(ppu.OamRAM))
	panicIfErr(encoder.Encode(ppu.CRAMBg))
	panicIfErr(encoder.Encode(ppu.CRAMBgAddr))
	panicIfErr(encoder.Encode(ppu.CRAMBgAutoInc))
	panicIfErr(encoder.Encode(ppu.CRAMObj))
	panicIfErr(encoder.Encode(ppu.CRAMObjAddr))
	panicIfErr(encoder.Encode(ppu.CRAMObjAutoInc))
	panicIfErr(encoder.Encode(ppu.tiles))
	panicIfErr(encoder.Encode(ppu.sprites))
	panicIfErr(encoder.Encode(ppu.STAT))
	panicIfErr(encoder.Encode(ppu.LCDC))
	panicIfErr(encoder.Encode(ppu.SCY))
	panicIfErr(encoder.Encode(ppu.SCX))
	panicIfErr(encoder.Encode(ppu.LY))
	panicIfErr(encoder.Encode(ppu.LYC))
	panicIfErr(encoder.Encode(ppu.WY))
	panicIfErr(encoder.Encode(ppu.WX))
	panicIfErr(encoder.Encode(ppu.BGP))
	panicIfErr(encoder.Encode(ppu.OBP0))
	panicIfErr(encoder.Encode(ppu.OBP1))
	panicIfErr(encoder.Encode(ppu.WindowScanline))
	panicIfErr(encoder.Encode(ppu.Mode))
	panicIfErr(encoder.Encode(ppu.CycleCount))
	panicIfErr(encoder.Encode(ppu.FrameCount))
	panicIfErr(encoder.Encode(ppu.wasDisplayEnabled))
	panicIfErr(encoder.Encode(ppu.wasModeInterruptTriggered))
}

func (ppu *Ppu) Load(decoder *gob.Decoder) error {
	errs := []error{
		decoder.Decode(&ppu.VRAM),
		decoder.Decode(&ppu.VRAMBank),
		decoder.Decode(&ppu.OamRAM),
		decoder.Decode(&ppu.CRAMBg),
		decoder.Decode(&ppu.CRAMBgAddr),
		decoder.Decode(&ppu.CRAMBgAutoInc),
		decoder.Decode(&ppu.CRAMObj),
		decoder.Decode(&ppu.CRAMObjAddr),
		decoder.Decode(&ppu.CRAMObjAutoInc),
		decoder.Decode(&ppu.tiles),
		decoder.Decode(&ppu.sprites),
		decoder.Decode(&ppu.STAT),
		decoder.Decode(&ppu.LCDC),
		decoder.Decode(&ppu.SCY),
		decoder.Decode(&ppu.SCX),
		decoder.Decode(&ppu.LY),
		decoder.Decode(&ppu.LYC),
		decoder.Decode(&ppu.WY),
		decoder.Decode(&ppu.WX),
		decoder.Decode(&ppu.BGP),
		decoder.Decode(&ppu.OBP0),
		decoder.Decode(&ppu.OBP1),
		decoder.Decode(&ppu.WindowScanline),
		decoder.Decode(&ppu.Mode),
		decoder.Decode(&ppu.CycleCount),
		decoder.Decode(&ppu.FrameCount),
		decoder.Decode(&ppu.wasDisplayEnabled),
		decoder.Decode(&ppu.wasModeInterruptTriggered),
	}
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// LCDC Values
func (ppu *Ppu) DisplayEnabled() bool {
	return (ppu.LCDC>>7)&1 != 0
}

func (ppu *Ppu) WindowTileMap() bool {
	return (ppu.LCDC>>6)&1 != 0
}

func (ppu *Ppu) WindowEnabled() bool {
	return (ppu.LCDC>>5)&1 != 0
}

func (ppu *Ppu) BgWindowTileData() bool {
	return (ppu.LCDC>>4)&1 != 0
}

func (ppu *Ppu) BgTileMapDisplay() bool {
	return (ppu.LCDC>>3)&1 != 0
}

func (ppu *Ppu) SpriteSize() bool {
	return (ppu.LCDC>>2)&1 != 0
}

func (ppu *Ppu) SpritesEnabled() bool {
	return (ppu.LCDC>>1)&1 != 0
}

func (ppu *Ppu) BgEnabled() bool {
	return ppu.LCDC&1 != 0
}

// STAT Values
//
//lint:ignore U1000 Ignore
func (ppu *Ppu) modeFlag() uint8 {
	return ppu.STAT & 3
}

func (ppu *Ppu) coincidenceFlag() bool {
	return (ppu.STAT>>2)&1 != 0
}

func (ppu *Ppu) hblankInterrupt() bool {
	return (ppu.STAT>>3)&1 != 0
}

func (ppu *Ppu) vblankInterrupt() bool {
	return (ppu.STAT>>4)&1 != 0
}

func (ppu *Ppu) oamInterrupt() bool {
	return (ppu.STAT>>5)&1 != 0
}

func (ppu *Ppu) coincidenceInterrupt() bool {
	return (ppu.STAT>>6)&1 != 0
}

func MakePpu(GBC *Console, frontend Frontend) *Ppu {
	ppu := &Ppu{
		frontend:  frontend,
		GBC:       GBC,
		Mode:      ACCESS_OAM,
		GBPalette: GB_PALETTE_GREEN,
	}
	return ppu
}

func (ppu *Ppu) DisableLCD() {
	ppu.LCDC &= 0x7F
}

func (ppu *Ppu) setPixel(x, y int, c uint8, pixelInfo PixelInfo, palette *Palette) {
	color := palette.colors[c]

	ppu.screen[x][y] = pixelInfo
	ppu.frontend.SetPixel(x, y, color)
}

func (ppu *Ppu) ReadVRam(addr uint16) uint8 {
	return ppu.VRAM[ppu.VRAMBank][addr]
}

func (ppu *Ppu) WriteVRam(addr uint16, value uint8) {
	ppu.VRAM[ppu.VRAMBank][addr] = value

	if addr >= 0x1800 {
		// TileMap, we do not care
		return
	}

	// Update tiles metadata
	// It is not strictly needed, but helps readability during rendering process
	addr &= 0xfffe
	tile := (addr >> 4) & 0x1ff
	y := (addr >> 1) & 7
	if ppu.VRAMBank == 1 {
		// Store Bank1 Tiles starting from index 512
		tile += 512
	}

	for x := uint8(0); x < 8; x++ {
		bitIndex := uint8(1 << (7 - x))
		v := uint8(0)
		if ppu.ReadVRam(addr)&bitIndex != 0 {
			v += 1
		}
		if ppu.ReadVRam(addr+1)&bitIndex != 0 {
			v += 2
		}
		ppu.tiles[tile].Pixels[y][x] = v
	}
}

func (ppu *Ppu) ReadOam(addr uint16) uint8 {
	return ppu.OamRAM[addr]
}

func (ppu *Ppu) WriteOam(addr uint16, value uint8) {
	ppu.OamRAM[addr] = value

	// Update sprites metadata
	// It is not strictly needed, but helps readability during rendering process
	sprite := &ppu.sprites[addr>>2]
	sprite.Ready = false
	switch addr & 3 {
	case 0:
		sprite.y = int(value) - 16
	case 1:
		sprite.x = int(value) - 8
	case 2:
		sprite.tile = value
	case 3:
		sprite.options = value
		sprite.Ready = true
	}
}

func (ppu *Ppu) SetCRamBgAddr(addr uint8) {
	ppu.CRAMBgAutoInc = (addr>>7)&1 != 0
	ppu.CRAMBgAddr = addr & 0x3f
}

func (ppu *Ppu) SetCRAMObjAddr(addr uint8) {
	ppu.CRAMObjAutoInc = (addr>>7)&1 != 0
	ppu.CRAMObjAddr = addr & 0x3f
}

func (ppu *Ppu) ReadCRamBg() uint8 {
	return ppu.CRAMBg[ppu.CRAMBgAddr]
}

func (ppu *Ppu) WriteCRamBg(value uint8) {
	ppu.CRAMBg[ppu.CRAMBgAddr] = value
	if ppu.CRAMBgAutoInc {
		ppu.CRAMBgAddr += 1
		if ppu.CRAMBgAddr == 64 {
			ppu.CRAMBgAddr = 0
		}
	}
}

func (ppu *Ppu) ReadCRamObj() uint8 {
	return ppu.CRAMObj[ppu.CRAMObjAddr]
}

func (ppu *Ppu) WriteCRamObj(value uint8) {
	ppu.CRAMObj[ppu.CRAMObjAddr] = value
	if ppu.CRAMObjAutoInc {
		ppu.CRAMObjAddr += 1
		if ppu.CRAMObjAddr == 64 {
			ppu.CRAMObjAddr = 0
		}
	}
}

func (ppu *Ppu) statIRQActive() bool {
	if !ppu.DisplayEnabled() {
		return false
	}

	if ppu.coincidenceFlag() && ppu.coincidenceInterrupt() {
		return true
	}

	switch ppu.Mode {
	case HBLANK:
		return ppu.hblankInterrupt()
	case VBLANK:
		return ppu.vblankInterrupt()
	case ACCESS_OAM:
		return ppu.oamInterrupt()
	default:
		return false
	}
}

func (ppu *Ppu) updateSTATInterrupt() {
	active := ppu.statIRQActive()
	if active && !ppu.wasModeInterruptTriggered {
		ppu.GBC.CPU.SetInterrupt(InterruptLCDStat.Mask)
	}
	ppu.wasModeInterruptTriggered = active
}

func (ppu *Ppu) setMode(mode PpuMode) {
	ppu.Mode = mode & 3
	ppu.STAT &= ^uint8(3)
	ppu.STAT |= uint8(ppu.Mode)
	ppu.updateSTATInterrupt()
}

func (ppu *Ppu) setCoincidenceFlag(value bool) {
	ppu.STAT &= ^uint8(4)
	if value {
		ppu.STAT |= 4
	}
	ppu.updateSTATInterrupt()
}

func getRGBFromColor(c uint8, palette uint8) uint32 {
	paletteGrey := []uint32{0xFFFFFFFF, 0xAAAAAAFF, 0x555555FF, 0x000000FF}
	paletteGreen := []uint32{0xe0f8d0ff, 0x88c070ff, 0x346856ff, 0x081820ff}

	if palette == GB_PALETTE_GREY {
		return paletteGrey[c]
	}
	return paletteGreen[c]
}

func (ppu *Ppu) loadPalette(reg uint8) Palette {
	var c0, c1, c2, c3 uint8
	c0 = reg & 3
	c1 = (reg >> 2) & 3
	c2 = (reg >> 4) & 3
	c3 = (reg >> 6) & 3

	return Palette{colors: [4]uint32{
		getRGBFromColor(c0, ppu.GBPalette),
		getRGBFromColor(c1, ppu.GBPalette),
		getRGBFromColor(c2, ppu.GBPalette),
		getRGBFromColor(c3, ppu.GBPalette)}}
}

func getRGBFromCRAM(cram []uint8, off int) uint32 {
	val := (uint(cram[off+1]) << 8) | uint(cram[off])

	r := (val & 0x1F) * 255 / 31
	g := ((val >> 5) & 0x1F) * 255 / 31
	b := ((val >> 10) & 0x1F) * 255 / 31
	return uint32((r << 24) | (g << 16) | (b << 8) | 0xFF)
}

func (ppu *Ppu) getCgbTileAttribute(tileAddr uint16) uint8 {
	attrOff := tileAddr - 0x8000
	attr := ppu.VRAM[1][attrOff]
	return attr
}

func (ppu *Ppu) getCgbBgPalette(attr uint8) Palette {
	paletteNum := attr & 7
	paletteOff := int(paletteNum) * 8
	palette := Palette{[4]uint32{
		getRGBFromCRAM(ppu.CRAMBg[:], paletteOff),
		getRGBFromCRAM(ppu.CRAMBg[:], paletteOff+2),
		getRGBFromCRAM(ppu.CRAMBg[:], paletteOff+4),
		getRGBFromCRAM(ppu.CRAMBg[:], paletteOff+6),
	}}
	return palette
}

func (ppu *Ppu) getCgbSpritePalette(sprite *Sprite) Palette {
	paletteNum := sprite.cgbPaletteNumber()
	paletteOff := int(paletteNum) * 8
	palette := Palette{[4]uint32{
		getRGBFromCRAM(ppu.CRAMObj[:], paletteOff),
		getRGBFromCRAM(ppu.CRAMObj[:], paletteOff+2),
		getRGBFromCRAM(ppu.CRAMObj[:], paletteOff+4),
		getRGBFromCRAM(ppu.CRAMObj[:], paletteOff+6),
	}}
	return palette
}

func (ppu *Ppu) drawBgLine() {
	addr := TILE_MAP_ZERO_ADDRESS
	if ppu.BgTileMapDisplay() {
		addr = TILE_MAP_ONE_ADDRESS
	}

	palette := ppu.loadPalette(ppu.BGP)
	useTileSetZero := ppu.BgWindowTileData()
	addr += ((uint16(ppu.SCY) + uint16(ppu.LY)) / 8 * 32) % 1024

	startRowAddr := addr
	endRowAddr := addr + 32
	addr += uint16(ppu.SCX) >> 3

	screen_x := 0
	screen_y := int(ppu.LY)

	x := ppu.SCX & 7
	y := (ppu.SCY + ppu.LY) & 7
	for i := uint16(0); i < 21; i++ {
		tileAddr := addr + i
		if tileAddr >= endRowAddr {
			tileAddr = startRowAddr + tileAddr%endRowAddr
		}

		flipH := false
		flipV := false
		bank := 0
		if ppu.GBC.CGBMode {
			tileAttr := ppu.getCgbTileAttribute(tileAddr)
			if (tileAttr>>3)&1 != 0 {
				bank = 1
			}
			if (tileAttr>>5)&1 != 0 {
				flipH = true
			}
			if (tileAttr>>6)&1 != 0 {
				flipV = true
			}
			palette = ppu.getCgbBgPalette(tileAttr)
		}

		tile := int(ppu.VRAM[0][tileAddr-0x8000])
		if !useTileSetZero && tile < 128 {
			tile += 256
		}
		if bank == 1 {
			tile += 512
		}

		for ; x < 8; x++ {
			if screen_x >= SCREEN_WIDTH {
				break
			}

			tileX := x
			tileY := y
			if flipH {
				tileX = 7 - tileX
			}
			if flipV {
				tileY = 7 - tileY
			}

			color := ppu.tiles[tile].Pixels[tileY][tileX]
			pixelInfo := PixelInfo{isNotTransparent: color != 0}
			if ppu.GBC.CGBMode {
				tileAttr := ppu.getCgbTileAttribute(tileAddr)
				pixelInfo.bgAttrBitNotSet = (tileAttr>>7)&1 == 0
			}
			ppu.setPixel(screen_x, screen_y, color, pixelInfo, &palette)
			screen_x++
		}
		x = 0
	}
}

func (ppu *Ppu) drawWindowLine() {
	if ppu.WY > ppu.LY {
		return
	}

	addr := TILE_MAP_ZERO_ADDRESS
	if ppu.WindowTileMap() {
		addr = TILE_MAP_ONE_ADDRESS
	}

	palette := ppu.loadPalette(ppu.BGP)
	useTileSetZero := ppu.BgWindowTileData()
	addr += ((uint16(ppu.WindowScanline) - uint16(ppu.WY)) / 8) * 32

	y := (uint16(ppu.LY) - uint16(ppu.WY)) & 7

	screen_x := int(ppu.WX) - 7
	screen_y := int(ppu.LY)

	for tileAddr := addr; tileAddr < addr+20; tileAddr++ {
		flipH := false
		flipV := false
		bank := 0
		if ppu.GBC.CGBMode {
			tileAttr := ppu.getCgbTileAttribute(tileAddr)
			if (tileAttr>>3)&1 != 0 {
				bank = 1
			}
			if (tileAttr>>5)&1 != 0 {
				flipH = true
			}
			if (tileAttr>>6)&1 != 0 {
				flipV = true
			}
			palette = ppu.getCgbBgPalette(tileAttr)
		}

		tile := int(ppu.VRAM[0][tileAddr-0x8000])
		if !useTileSetZero && tile < 128 {
			tile += 256
		}
		if bank == 1 {
			tile += 512
		}

		for x := 0; x < 8; x++ {
			if screen_x < 0 || screen_x >= SCREEN_WIDTH {
				continue
			}

			tileX := x
			tileY := y
			if flipH {
				tileX = 7 - tileX
			}
			if flipV {
				tileY = 7 - tileY
			}

			color := ppu.tiles[tile].Pixels[tileY][tileX]
			pixelInfo := PixelInfo{isNotTransparent: color != 0}
			if ppu.GBC.CGBMode {
				tileAttr := ppu.getCgbTileAttribute(tileAddr)
				pixelInfo.bgAttrBitNotSet = (tileAttr>>7)&1 == 0
			}
			ppu.setPixel(screen_x, screen_y, color, pixelInfo, &palette)
			screen_x++
		}
	}
}

func (ppu *Ppu) drawSprites() {
	spriteHeight := 8
	if ppu.SpriteSize() {
		spriteHeight = 16
	}

	renderedSprites := 0
	for i := 0; i < 40; i++ {
		sprite := &ppu.sprites[i]
		if !sprite.Ready {
			continue
		}
		if (sprite.y > int(ppu.LY)) || (sprite.y+spriteHeight) <= int(ppu.LY) {
			continue
		}

		if renderedSprites >= MAX_SPRITES {
			continue
		}
		renderedSprites++

		if (sprite.x < -7) || (sprite.x >= SCREEN_WIDTH) {
			continue
		}

		pixelY := int(ppu.LY) - sprite.y
		if sprite.yFlip() != 0 {
			off := 0
			if ppu.SpriteSize() {
				off = 8
			}
			pixelY = (7 + off) - pixelY
		}

		screen_x := 0
		screen_y := int(ppu.LY)

		for x := 0; x < 8; x++ {
			tileNum := int(sprite.tile)
			if ppu.SpriteSize() {
				tileNum -= tileNum % 2
			}
			if ppu.GBC.CGBMode && sprite.cgbVramBank() == 1 {
				tileNum += 512
			}

			screen_x = sprite.x + x
			if screen_x < 0 || screen_x >= SCREEN_WIDTH {
				continue
			}

			pixelX := x
			if sprite.xFlip() != 0 {
				pixelX = 7 - x
			}

			palette := ppu.loadPalette(ppu.OBP0)
			if sprite.paletteNumber() != 0 {
				palette = ppu.loadPalette(ppu.OBP1)
			}

			color := uint8(0)
			if ppu.SpriteSize() && pixelY >= 8 {
				color = ppu.tiles[tileNum+1].Pixels[pixelY-8][pixelX]
			} else {
				color = ppu.tiles[tileNum].Pixels[pixelY][pixelX]
			}
			if color == 0 {
				continue
			}
			pixelInfo := ppu.screen[screen_x][screen_y]
			if !ppu.GBC.CGBMode {
				if (!pixelInfo.spritePixel && !pixelInfo.isNotTransparent) || (pixelInfo.spritePixel && pixelInfo.spriteX > sprite.x) || (!pixelInfo.spritePixel && !sprite.renderPriority()) {
					ppu.setPixel(screen_x, screen_y, color, PixelInfo{spritePixel: true, spriteX: sprite.x}, &palette)
				}
			} else {
				if !pixelInfo.spritePixel && (!ppu.BgEnabled() || !pixelInfo.isNotTransparent || (!sprite.renderPriority() && pixelInfo.bgAttrBitNotSet)) {
					palette = ppu.getCgbSpritePalette(sprite)
					ppu.setPixel(screen_x, screen_y, color, PixelInfo{spritePixel: true, spriteX: sprite.x}, &palette)
				}
			}
		}
	}
}

func (ppu *Ppu) writeScanline() {
	if !ppu.DisplayEnabled() {
		return
	}

	if ppu.GBC.CGBMode || ppu.BgEnabled() {
		ppu.drawBgLine()
	}

	if ppu.WindowEnabled() {
		ppu.drawWindowLine()
	}

	if ppu.SpritesEnabled() {
		ppu.drawSprites()
	}
}

func (ppu *Ppu) checkCoincidenceLY_LYC() {
	ppu.setCoincidenceFlag(ppu.LYC == ppu.LY)
}

func (ppu *Ppu) Tick(ticks int) {
	clocks := ticks * 4
	if ppu.GBC.DoubleSpeedMode {
		clocks = ticks * 2
	}

	if ppu.wasDisplayEnabled && !ppu.DisplayEnabled() {
		ppu.LY = 0
		ppu.Mode = HBLANK
		ppu.GBC.DMA.SignalHdma()
	}
	ppu.wasDisplayEnabled = ppu.DisplayEnabled()

	ppu.CycleCount += clocks
	if ppu.CycleCount >= 70224 {
		ppu.CycleCount %= 70224
	}

	switch ppu.Mode {
	case ACCESS_OAM:
		if ppu.CycleCount >= CLOCKS_ACCESS_OAM {
			ppu.CycleCount %= CLOCKS_ACCESS_OAM
			ppu.setMode(ACCESS_VRAM)
		}
	case ACCESS_VRAM:
		if ppu.CycleCount >= CLOCKS_ACCESS_VRAM {
			ppu.CycleCount %= CLOCKS_ACCESS_VRAM
			ppu.setMode(HBLANK)
			ppu.GBC.DMA.SignalHdma()

			ppu.writeScanline()
		}
	case HBLANK:
		if ppu.CycleCount >= CLOCKS_HBLANK {
			ppu.CycleCount %= CLOCKS_HBLANK

			ppu.LY += 1
			ppu.checkCoincidenceLY_LYC()

			if ppu.WX <= 166 {
				ppu.WindowScanline += 1
			}

			if ppu.LY == 144 {
				ppu.setMode(VBLANK)

				ppu.FrameCount += 1
				if ppu.DisplayEnabled() {
					ppu.frontend.CommitScreen()
					ppu.GBC.CPU.SetInterrupt(InterruptVBlank.Mask)
				}
			} else {
				ppu.setMode(ACCESS_OAM)
			}
		}
	case VBLANK:
		if ppu.CycleCount >= CLOCKS_VBLANK {
			ppu.CycleCount %= CLOCKS_VBLANK

			ppu.LY += 1
			ppu.checkCoincidenceLY_LYC()

			if ppu.LY == 154 {
				ppu.LY = 0
				ppu.WindowScanline = 0
				ppu.checkCoincidenceLY_LYC()
				ppu.setMode(ACCESS_OAM)
			}
		}
	}
}
