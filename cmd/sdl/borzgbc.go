package main

import (
	"borzGBC/pkg/gbc"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/veandco/go-sdl2/sdl"
	"github.com/veandco/go-sdl2/ttf"
)

var (
	SDL_WHITE = sdl.Color{R: 255, G: 255, B: 255, A: 0}
	SDL_BLACK = sdl.Color{R: 0, G: 0, B: 0, A: 0}
)

// Number of frames after which the inputs will be synced (in network serial mode)
var SERIAL_FRAME_SYNC = 3

// If true, show the companion screen (only for debug purposes)
var SHOW_SERIAL_COMPANION = false

type serialSync struct {
	companion *gbc.Console
	remote    net.Conn

	surface  *sdl.Surface
	renderer *sdl.Renderer

	running          bool
	rxSB, rxSC       chan uint8
	txSB, txSC       chan uint8
	txInput, rxInput chan uint8
}

type serialRemote struct {
	address  string
	roomName string
}

func makeSerialSync() *serialSync {
	var renderer *sdl.Renderer = nil
	var surface *sdl.Surface = nil

	if SHOW_SERIAL_COMPANION {
		window, renderer, err := sdl.CreateWindowAndRenderer(
			int32(160), int32(144), 0)
		if err != nil {
			panic(err)
		}
		window.SetTitle("COMPANION")
		surface, err = sdl.CreateRGBSurface(
			0, int32(160), int32(144), 32, 0xFF000000, 0x00FF0000, 0x0000FF00, 0x000000FF)
		if err != nil {
			panic(err)
		}

		renderer.SetDrawColor(0, 0, 0, 255)
		renderer.Clear()
	}

	res := &serialSync{
		companion: nil,
		remote:    nil,
		surface:   surface,
		renderer:  renderer,
		running:   true,
		rxSB:      make(chan uint8),
		rxSC:      make(chan uint8),
		txSB:      make(chan uint8),
		txSC:      make(chan uint8),
		txInput:   make(chan uint8),
		rxInput:   make(chan uint8),
	}
	res.running = true
	return res
}

func (s *serialSync) NotifyAudioSample(l, r int8) {}
func (s *serialSync) SetPixel(x, y int, c uint32) {
	if SHOW_SERIAL_COMPANION {
		var r, g, b, a uint8
		r = uint8((c >> 24) & 0xFF)
		g = uint8((c >> 16) & 0xFF)
		b = uint8((c >> 8) & 0xFF)
		a = uint8(c & 0xFF)

		pixels := s.surface.Pixels()
		pixels[y*int(s.surface.Pitch)+x*int(s.surface.BytesPerPixel())+0] = a
		pixels[y*int(s.surface.Pitch)+x*int(s.surface.BytesPerPixel())+1] = b
		pixels[y*int(s.surface.Pitch)+x*int(s.surface.BytesPerPixel())+2] = g
		pixels[y*int(s.surface.Pitch)+x*int(s.surface.BytesPerPixel())+3] = r
	}
}
func (pl *serialSync) CommitScreen() {
	if SHOW_SERIAL_COMPANION {
		texture, err := pl.renderer.CreateTextureFromSurface(pl.surface)
		if err != nil {
			fmt.Println("Unable to create texture while rendering")
			return
		}
		defer texture.Destroy()

		rect := sdl.Rect{
			X: 0,
			Y: 0,
			W: int32(160),
			H: int32(144)}
		pl.renderer.Copy(texture, nil, &rect)
		pl.renderer.Present()
		pl.renderer.SetDrawColor(0xff, 0xff, 0xff, 0xff)
		pl.renderer.Clear()
	}
}

func (s *serialSync) ExchangeSerial(sb, sc uint8) (uint8, uint8) {
	if s.running {
		txSB := <-s.txSB
		txSC := <-s.txSC
		s.rxSB <- sb
		s.rxSC <- sc
		return txSB, txSC
	}
	return 0, 0
}

type SDLPlugin struct {
	window        *sdl.Window
	renderer      *sdl.Renderer
	surface       *sdl.Surface
	width, height int
	scale         int

	pushNotificationCounter int
	pushNotificationText    string
	font                    *ttf.Font
	charWidth, charHeight   int

	audioSpec      sdl.AudioSpec
	audioDevice    sdl.AudioDeviceID
	soundBuffer    []byte
	soundBufferIdx int

	fastMode int
	slowMode bool

	serial *serialSync
}

func MakeSDLPlugin(scale int) (*SDLPlugin, error) {
	var err error

	if err := sdl.Init(sdl.INIT_EVERYTHING); err != nil {
		return nil, err
	}
	pl := &SDLPlugin{
		width:    160,
		height:   144,
		scale:    scale,
		fastMode: 0,
		slowMode: false,
	}

	pl.window, pl.renderer, err = sdl.CreateWindowAndRenderer(
		int32(pl.width*pl.scale), int32(pl.height*pl.scale), 0)
	if err != nil {
		return nil, err
	}
	pl.setTitle()

	pl.surface, err = sdl.CreateRGBSurface(
		0, int32(pl.width), int32(pl.height), 32, 0xFF000000, 0x00FF0000, 0x0000FF00, 0x000000FF)
	if err != nil {
		return nil, err
	}

	pl.renderer.SetDrawColor(0, 0, 0, 255)
	pl.renderer.Clear()

	// Audio
	want := sdl.AudioSpec{}
	want.Freq = 44100
	want.Format = sdl.AUDIO_S8
	want.Channels = 2
	want.Samples = 2048
	pl.audioDevice, err = sdl.OpenAudioDevice("", false, &want, &pl.audioSpec, 0)
	if err != nil {
		return nil, err
	}
	pl.soundBuffer = make([]byte, pl.audioSpec.Samples*2)
	sdl.PauseAudioDevice(pl.audioDevice, false)

	// Fonts
	err = ttf.Init()
	if err != nil {
		return nil, err
	}
	pl.font, err = ttf.OpenFont("assets/courier.ttf", 20)
	if err != nil {
		return nil, err
	}
	// deduce font size
	s, err := pl.font.RenderUTF8Solid("A", SDL_WHITE)
	if err != nil {
		return nil, err
	}
	pl.charHeight = int(s.H)
	pl.charWidth = int(s.W)
	s.Free()

	return pl, nil
}

func (pl *SDLPlugin) ExchangeSerial(sb, sc uint8) (uint8, uint8) {
	if pl.serial != nil && pl.serial.running {
		pl.serial.txSB <- sb
		pl.serial.txSC <- sc
		rxSB := <-pl.serial.rxSB
		rxSC := <-pl.serial.rxSC
		return rxSB, rxSC
	}
	return 0, 0
}

func (pl *SDLPlugin) NotifyAudioSample(l, r int8) {
	// log.Printf("adding sample: %d, %d\n", l, r)
	if pl.soundBufferIdx >= len(pl.soundBuffer) {
		pl.soundBufferIdx = 0
		sdl.QueueAudio(pl.audioDevice, pl.soundBuffer[:])
	}
	pl.soundBuffer[pl.soundBufferIdx] = byte(l)
	pl.soundBuffer[pl.soundBufferIdx+1] = byte(r)
	pl.soundBufferIdx += 2
}

func (pl *SDLPlugin) DisplayNotification(text string) {
	pl.pushNotificationCounter = 120
	pl.pushNotificationText = text
	if len(pl.pushNotificationText)*pl.charWidth-20 > pl.width*pl.scale {
		// Trim the text to fit the screen
		newLen := (pl.width*pl.scale+20)/pl.charWidth - 1
		pl.pushNotificationText = pl.pushNotificationText[:newLen]
	}
}

func (pl *SDLPlugin) Destroy() {
	pl.renderer.Destroy()
	pl.window.Destroy()
	pl.surface.Free()
	pl.font.Close()
	sdl.CloseAudioDevice(pl.audioDevice)
	sdl.Quit()
}

func (pl *SDLPlugin) SetPixel(x, y int, c uint32) {
	var r, g, b, a uint8
	r = uint8((c >> 24) & 0xFF)
	g = uint8((c >> 16) & 0xFF)
	b = uint8((c >> 8) & 0xFF)
	a = uint8(c & 0xFF)

	pixels := pl.surface.Pixels()
	pixels[y*int(pl.surface.Pitch)+x*int(pl.surface.BytesPerPixel())+0] = a
	pixels[y*int(pl.surface.Pitch)+x*int(pl.surface.BytesPerPixel())+1] = b
	pixels[y*int(pl.surface.Pitch)+x*int(pl.surface.BytesPerPixel())+2] = g
	pixels[y*int(pl.surface.Pitch)+x*int(pl.surface.BytesPerPixel())+3] = r
}

func (pl *SDLPlugin) CommitScreen() {
	texture, err := pl.renderer.CreateTextureFromSurface(pl.surface)
	if err != nil {
		fmt.Println("Unable to create texture while rendering")
		return
	}
	defer texture.Destroy()

	rect := sdl.Rect{
		X: 0,
		Y: 0,
		W: int32(pl.width * pl.scale),
		H: int32(pl.height * pl.scale)}
	pl.renderer.Copy(texture, nil, &rect)

	if pl.pushNotificationCounter > 0 {
		pl.pushNotificationCounter -= 1

		pushSurface, err := pl.font.RenderUTF8Shaded(
			pl.pushNotificationText, SDL_WHITE, SDL_BLACK)
		if err != nil {
			fmt.Println("Unable to render push notification")
			return
		}
		defer pushSurface.Free()
		pushTexture, err := pl.renderer.CreateTextureFromSurface(pushSurface)
		if err != nil {
			fmt.Println("Unable to create texture while rendering (push notification)")
			return
		}
		defer texture.Destroy()
		pushRect := sdl.Rect{
			X: 10,
			Y: 10,
			W: int32(pl.charWidth * len(pl.pushNotificationText)),
			H: int32(pl.charHeight)}
		pl.renderer.Copy(pushTexture, nil, &pushRect)
	}

	pl.renderer.Present()

	pl.renderer.SetDrawColor(0xff, 0xff, 0xff, 0xff)
	pl.renderer.Clear()
}

func (pl *SDLPlugin) setTitle() {
	title := "BorzGBC"
	if pl.fastMode > 0 {
		title += fmt.Sprintf(" - fast x%d", pow2(pl.fastMode))
	} else if pl.slowMode {
		title += " - SLOW"
	}
	pl.window.SetTitle(title)
}

func pow2(n int) int {
	if n == 0 {
		return 1
	}
	result := 2
	for i := 2; i <= n; i++ {
		result *= 2
	}
	return result
}

func (pl *SDLPlugin) initializeSerialServer(console *gbc.Console, serialRemote serialRemote) error {
	addr, err := net.ResolveTCPAddr("tcp4", serialRemote.address)
	if err != nil {
		return err
	}
	conn, err := net.DialTCP("tcp4", nil, addr)
	if err != nil {
		return err
	}

	// send room name
	roomNameBuf := make([]byte, 40)
	copy(roomNameBuf, []byte(serialRemote.roomName))
	_, err = conn.Write(roomNameBuf)
	if err != nil {
		return err
	}

	fmt.Printf("Connected to serial server, waiting for peer...\n")
	fmt.Printf("ROOM NAME (to share): %s\n", serialRemote.roomName)

	// wait of peers
	waitByte := make([]byte, 1)
	var peerNum byte
	for {
		_, err = io.ReadFull(conn, waitByte)
		if err != nil {
			return err
		}
		if waitByte[0] == byte(43) {
			// Read peer number
			peerNumBuf := make([]byte, 1)
			_, err = io.ReadFull(conn, peerNumBuf)
			if err != nil {
				return err
			}
			peerNum = peerNumBuf[0]
			break
		}
	}

	fmt.Printf("Both peers connected (I am peer%d), exchanging data...\n", peerNum)

	// Exchange ROM and STATE - peer1 sends first, peer2 receives first to avoid deadlock
	var peerRomData []byte
	var peerSavData []byte

	if peerNum == 1 {
		// Peer1: send ROM, receive ROM, send STATE, receive STATE
		romDataSize := uint32(len(console.ROM))
		romDataSizeBuf := make([]byte, 4)
		binary.BigEndian.PutUint32(romDataSizeBuf, romDataSize)
		_, err = conn.Write(romDataSizeBuf)
		if err != nil {
			return err
		}
		_, err = conn.Write(console.ROM)
		if err != nil {
			return err
		}
		fmt.Printf("ROM sent (%d bytes)...\n", romDataSize+4)

		peerRomDataSizeBuf := make([]byte, 4)
		_, err = io.ReadFull(conn, peerRomDataSizeBuf)
		if err != nil {
			return err
		}
		peerRomDataSize := binary.BigEndian.Uint32(peerRomDataSizeBuf)
		peerRomData = make([]byte, peerRomDataSize)
		_, err = io.ReadFull(conn, peerRomData)
		if err != nil {
			return err
		}
		fmt.Printf("ROM received... (%d bytes)\n", peerRomDataSize+4)

		savData := console.SaveState()
		savDataSizeBuf := make([]byte, 4)
		binary.BigEndian.PutUint32(savDataSizeBuf, uint32(len(savData)))
		_, err = conn.Write(savDataSizeBuf)
		if err != nil {
			return err
		}
		_, err = conn.Write(savData)
		if err != nil {
			return err
		}
		fmt.Printf("STATE sent (%d bytes)...\n", len(savData)+4)

		peerSavDataSizeBuf := make([]byte, 4)
		_, err = io.ReadFull(conn, peerSavDataSizeBuf)
		if err != nil {
			return err
		}
		peerSavDataSize := binary.BigEndian.Uint32(peerSavDataSizeBuf)
		peerSavData = make([]byte, peerSavDataSize)
		_, err = io.ReadFull(conn, peerSavData)
		if err != nil {
			return err
		}
		fmt.Printf("STATE received... (%d bytes)\n", peerSavDataSize+4)
	} else {
		// Peer2: receive ROM, send ROM, receive STATE, send STATE
		peerRomDataSizeBuf := make([]byte, 4)
		_, err = io.ReadFull(conn, peerRomDataSizeBuf)
		if err != nil {
			return err
		}
		peerRomDataSize := binary.BigEndian.Uint32(peerRomDataSizeBuf)
		peerRomData = make([]byte, peerRomDataSize)
		_, err = io.ReadFull(conn, peerRomData)
		if err != nil {
			return err
		}
		fmt.Printf("ROM received... (%d bytes)\n", peerRomDataSize+4)

		romDataSize := uint32(len(console.ROM))
		romDataSizeBuf := make([]byte, 4)
		binary.BigEndian.PutUint32(romDataSizeBuf, romDataSize)
		_, err = conn.Write(romDataSizeBuf)
		if err != nil {
			return err
		}
		_, err = conn.Write(console.ROM)
		if err != nil {
			return err
		}
		fmt.Printf("ROM sent (%d bytes)...\n", romDataSize+4)

		peerSavDataSizeBuf := make([]byte, 4)
		_, err = io.ReadFull(conn, peerSavDataSizeBuf)
		if err != nil {
			return err
		}
		peerSavDataSize := binary.BigEndian.Uint32(peerSavDataSizeBuf)
		peerSavData = make([]byte, peerSavDataSize)
		_, err = io.ReadFull(conn, peerSavData)
		if err != nil {
			return err
		}
		fmt.Printf("STATE received... (%d bytes)\n", peerSavDataSize+4)

		savData := console.SaveState()
		savDataSizeBuf := make([]byte, 4)
		binary.BigEndian.PutUint32(savDataSizeBuf, uint32(len(savData)))
		_, err = conn.Write(savDataSizeBuf)
		if err != nil {
			return err
		}
		_, err = conn.Write(savData)
		if err != nil {
			return err
		}
		fmt.Printf("STATE sent (%d bytes)...\n", len(savData)+4)
	}

	// initialize peer console
	pl.serial = makeSerialSync()
	pl.serial.remote = conn
	pl.serial.companion, err = gbc.MakeConsole(peerRomData, pl.serial)
	if err != nil {
		return err
	}
	err = pl.serial.companion.LoadState(peerSavData)
	if err != nil {
		return err
	}

	fmt.Printf("Companion console initialized...\n")

	// start the network sync loop
	go func() {
		rawRXData := make([]byte, 1)
		for pl.serial.running {
			_, err := io.ReadFull(pl.serial.remote, rawRXData)
			if err != nil || len(rawRXData) == 0 {
				break
			}
			pl.serial.rxInput <- rawRXData[0]
		}
		close(pl.serial.rxInput)
		pl.serial.remote.Close()
		pl.serial.running = false
	}()
	go func() {
		for pl.serial.running {
			toSend := <-pl.serial.txInput
			_, err := pl.serial.remote.Write([]byte{toSend})
			if err != nil {
				break
			}
		}
		pl.serial.remote.Close()
		pl.serial.running = false
	}()

	// start the emulator
	go func() {
		syncCount := 0
		for pl.serial.running {
			if syncCount == SERIAL_FRAME_SYNC {
				pl.serial.companion.Input.BackState.Unserialize(<-pl.serial.rxInput)
				syncCount = 0
			} else {
				syncCount += 1
			}
			pl.serial.companion.Step()
		}
		// read remaining data from channels...
		_ = <-pl.serial.txSB + <-pl.serial.txSC

		close(pl.serial.rxSB)
		close(pl.serial.rxSC)
		pl.serial.remote.Close()
		pl.serial.running = false
	}()
	return nil
}

func saveState(rom string, console *gbc.Console, n int) error {
	statePath := fmt.Sprintf("%s.state.%d", rom, n)
	return os.WriteFile(statePath, console.SaveState(), 0644)
}

func loadState(rom string, console *gbc.Console, n int) error {
	statePath := fmt.Sprintf("%s.state.%d", rom, n)
	data, err := os.ReadFile(statePath)
	if err != nil {
		return err
	}
	return console.LoadState(data)
}

func (pl *SDLPlugin) Run(rom string, console *gbc.Console, serialRemote serialRemote) error {
	// serial server
	if serialRemote.address != "" {
		if err := pl.initializeSerialServer(console, serialRemote); err != nil {
			return fmt.Errorf("unable to initialize serial client: %s", err)
		}
	}

	syncCount := 0
	currentInput := gbc.JoypadState{}
	freezedInput := gbc.JoypadState{}

	running := true
	for running {
		start := time.Now()

		for event := sdl.PollEvent(); event != nil; event = sdl.PollEvent() {
			switch t := event.(type) {
			case *sdl.QuitEvent:
				running = false
			case *sdl.KeyboardEvent:
				if t.Repeat != 0 {
					break
				}
				keyCode := t.Keysym.Sym
				switch keyCode {
				case sdl.K_q:
					running = false
				case sdl.K_F1, sdl.K_F2, sdl.K_F3, sdl.K_F4:
					if t.State == sdl.PRESSED && pl.serial == nil {
						n := int(keyCode - sdl.K_F1 + 1)
						err := saveState(rom, console, n)
						if err != nil {
							pl.DisplayNotification("error while saving state")
						} else {
							pl.DisplayNotification("state saved")
						}
					}
				case sdl.K_F5, sdl.K_F6, sdl.K_F7, sdl.K_F8:
					n := int(keyCode - sdl.K_F5 + 1)
					if t.State == sdl.PRESSED && pl.serial == nil {
						err := loadState(rom, console, n)
						if err != nil {
							log.Printf("ERROR LOADING STATE: %s\n", err)
							pl.DisplayNotification("error while loading state")
						} else {
							pl.DisplayNotification("state loaded")
						}
					}
				case sdl.K_f:
					if t.State == sdl.PRESSED && pl.serial == nil {
						console.CPUFreq = gbc.GBCPU_FREQ
						pl.fastMode = (pl.fastMode + 1) % 4
						console.CPUFreq = gbc.GBCPU_FREQ * pow2(pl.fastMode)
						pl.slowMode = false
						if pl.fastMode > 0 {
							pl.DisplayNotification(fmt.Sprintf("fast mode x%d", pow2(pl.fastMode)))
						} else {
							pl.DisplayNotification("normal mode")
						}
						pl.setTitle()
					}
				case sdl.K_g:
					if t.State == sdl.PRESSED && pl.serial == nil {
						console.CPUFreq = gbc.GBCPU_FREQ
						if !pl.slowMode {
							console.CPUFreq = gbc.GBCPU_FREQ / 2
						}
						pl.fastMode = 0
						pl.slowMode = !pl.slowMode
						if pl.slowMode {
							pl.DisplayNotification("slow mode")
						} else {
							pl.DisplayNotification("normal mode")
						}
						pl.setTitle()
					}
				case sdl.K_m:
					if t.State == sdl.PRESSED {
						console.APU.ToggleAudio()
						if console.APU.IsMuted() {
							pl.DisplayNotification("muted")
						} else {
							pl.DisplayNotification("unmuted")
						}
					}
				case sdl.K_PLUS:
					if t.State == sdl.PRESSED {
						console.APU.IncreaseAudio()
						pl.DisplayNotification(console.APU.GetVolumeString())
					}
				case sdl.K_MINUS:
					if t.State == sdl.PRESSED {
						console.APU.DecreaseAudio()
						pl.DisplayNotification(console.APU.GetVolumeString())
					}
				// Debug Flags
				// case sdl.K_1:
				// 	if t.State == sdl.PRESSED {
				// 		console.APU.ToggleSoundChannel(1)
				// 		if console.APU.IsChMuted(1) {
				// 			pl.DisplayNotification("ch1 muted")
				// 		} else {
				// 			pl.DisplayNotification("ch1 unmuted")
				// 		}
				// 	}
				// case sdl.K_2:
				// 	if t.State == sdl.PRESSED {
				// 		console.APU.ToggleSoundChannel(2)
				// 		if console.APU.IsChMuted(2) {
				// 			pl.DisplayNotification("ch2 muted")
				// 		} else {
				// 			pl.DisplayNotification("ch2 unmuted")
				// 		}
				// 	}
				// case sdl.K_3:
				// 	if t.State == sdl.PRESSED {
				// 		console.APU.ToggleSoundChannel(3)
				// 		if console.APU.IsChMuted(3) {
				// 			pl.DisplayNotification("ch3 muted")
				// 		} else {
				// 			pl.DisplayNotification("ch3 unmuted")
				// 		}
				// 	}
				// case sdl.K_4:
				// 	if t.State == sdl.PRESSED {
				// 		console.APU.ToggleSoundChannel(4)
				// 		if console.APU.IsChMuted(4) {
				// 			pl.DisplayNotification("ch4 muted")
				// 		} else {
				// 			pl.DisplayNotification("ch4 unmuted")
				// 		}
				// 	}
				// case sdl.K_b:
				// 	if t.State == sdl.PRESSED {
				// 		bgmap := console.GetBackgroundMapStr()
				// 		fmt.Println(bgmap)
				// 	}

				// GB Keys
				case sdl.K_z:
					currentInput.A = t.State == sdl.PRESSED
				case sdl.K_x:
					currentInput.B = t.State == sdl.PRESSED
				case sdl.K_RETURN:
					currentInput.START = t.State == sdl.PRESSED
				case sdl.K_BACKSPACE:
					currentInput.SELECT = t.State == sdl.PRESSED
				case sdl.K_UP:
					currentInput.UP = t.State == sdl.PRESSED
				case sdl.K_DOWN:
					currentInput.DOWN = t.State == sdl.PRESSED
				case sdl.K_LEFT:
					currentInput.LEFT = t.State == sdl.PRESSED
				case sdl.K_RIGHT:
					currentInput.RIGHT = t.State == sdl.PRESSED
				}
			}
		}

		if serialRemote.address != "" {
			if pl.serial.running {
				if syncCount == 0 {
					pl.serial.txInput <- currentInput.Serialize()
					freezedInput = currentInput
				} else if syncCount == SERIAL_FRAME_SYNC {
					syncCount = -1
					console.Input.BackState = freezedInput
				}
				syncCount += 1
			} else {
				// serial peer disconnected
				log.Printf("serial peer disconnected")

				close(pl.serial.txInput)
				close(pl.serial.txSB)
				close(pl.serial.txSC)
				serialRemote.address = ""
			}
		} else {
			console.Input.BackState = currentInput
		}
		ticks := console.Step()

		elapsed := time.Since(start)
		if int(elapsed.Milliseconds()) < console.GetMs(ticks) {
			sdl.Delay(uint32(console.GetMs(ticks) - int(elapsed.Milliseconds())))
			for sdl.GetQueuedAudioSize(pl.audioDevice) > uint32(pl.audioSpec.Freq/5) {
				// Wait for audio buffer to process remaining data (if any)
				sdl.Delay(80)
			}
		} else {
			if console.Verbose {
				fmt.Println("Emulation is too slow")
			}
		}
	}
	return nil
}

func main() {
	defaultRoomName := uuid.New().String()

	serialServer := flag.String("serialServer", "", "serial server address (host:port)")
	serialRoomName := flag.String("serialRoomName", "", "serial server room name")
	flag.Parse()

	if flag.NArg() != 1 {
		fmt.Printf("Usage: %s [options] <path/to/rom>\n", os.Args[0])
		return
	}

	if *serialRoomName == "" {
		serialRoomName = &defaultRoomName
	}
	remoteData := serialRemote{
		address:  *serialServer,
		roomName: *serialRoomName,
	}

	pl, err := MakeSDLPlugin( /* scaling factor */ 3)
	if err != nil {
		log.Printf("unable to create SDLPlugin: %s\n", err)
		return
	}
	defer pl.Destroy()

	romPath := flag.Args()[0]
	rom, err := os.ReadFile(romPath)
	if err != nil {
		log.Printf("invalid rom: %s\n", err)
		return
	}
	console, err := gbc.MakeConsole(rom, pl)
	if err != nil {
		log.Printf("unable to create the console: %s\n", err)
		return
	}
	savFile := fmt.Sprintf("%s.sav", romPath)
	sav, err := os.ReadFile(savFile)
	if err == nil {
		err = console.LoadSav(sav)
		if err != nil {
			log.Printf("unable to load sav: %s\n", err)
			return
		}
	}

	console.Verbose = false
	console.CPU.EnableDisas = false
	console.PrintDebug = false
	err = pl.Run(romPath, console, remoteData)
	if err != nil {
		log.Printf("unable to run the emulator: %s\n", err)
	}

	sav, err = console.StoreSav()
	if err != nil {
		log.Printf("unable to store sav: %s\n", err)
		return
	}
	err = os.WriteFile(savFile, sav, 0644)
	if err != nil {
		log.Printf("unable to store sav: %s\n", err)
		return
	}
}
