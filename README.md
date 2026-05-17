# bgbc

Gameboy Color Emulator written in Go. Multiplatform and based in SDL.

The emulator is *not* cycle-accurate, but it can still run many GB and GBC roms.

### Compilation and Usage

To compile, just run `make` in the project directory. You need libsdl installed in your system.

To run the emulator, run:
```
$ ./bgbc /path/to/rom
```

### Tests

To run the test suite, pull the submodule:
```
$ git submodule update --init
```

then, run the tests:
```
$ go test
```

### Keymappings

The keymappings are the following:

| action                           | key            |
|----------------------------------|----------------|
| A                                | Z              |
| B                                | X              |
| Start                            | Enter          |
| Select                           | Backspace      |
| Up/Down/Left/Right               | ArrowKeys      |
| Save State (slot 1, 2, 3, 4)     | F1, F2, F3, F4 |
| Load State (slot 1, 2, 3, 4)     | F5, F6, F7, F8 |
| Fast Forward Mode (up to 8x)     | F              |
| Slow Mode (0.5x)                 | G              |
| Mute                             | M              |


### Documentation
- https://gbdev.io/pandocs
- https://www.zilog.com/docs/z80/um0080.pdf
- https://gbdev.gg8.se/wiki/articles/Serial_Communication_(Link_Cable)_Tutorial

### Credits
- The APU implementation is taken from [goboy](https://github.com/Humpheh/goboy) emulator
