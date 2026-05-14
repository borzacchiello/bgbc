package z80cpu

import (
	"testing"
)

type TestMemory struct {
	mem [0xffff]uint8
}

func (mem *TestMemory) Read(addr uint16) uint8 {
	return mem.mem[addr]
}

func (mem *TestMemory) Write(addr uint16, val uint8) {
	mem.mem[addr] = val
}

func (mem *TestMemory) WriteBuffer(addr uint16, data []uint8) {
	for i, b := range data {
		mem.Write(addr+uint16(i), b)
	}
}

func TestLoadA(t *testing.T) {
	var prog = []byte{
		0x3e, 0x0a, // ld  a, 10
		0x06, 0x14, // ld  b, 20
		0x80, //       add a, b
	}
	memory := &TestMemory{}
	memory.WriteBuffer(0, prog)

	cpu := Z80Cpu{Mem: memory}
	cpu.ExecOne()
	cpu.ExecOne()
	cpu.ExecOne()

	if cpu.A != 30 || cpu.B != 20 {
		t.Errorf("cpu.A=%d (exp: 30); cpu.B=%d (exp: 20)", cpu.A, cpu.B)
	}
}

func TestXor(t *testing.T) {
	var prog = []byte{
		0x3e, 0x10, // ld a, 0x10
		0x06, 0x20, // ld b, 0x20
		0xa8, //       xor b
	}

	memory := &TestMemory{}
	memory.WriteBuffer(0, prog)

	cpu := Z80Cpu{Mem: memory}
	cpu.ExecOne()
	cpu.ExecOne()
	cpu.ExecOne()

	if cpu.A != 0x30 {
		t.Errorf("cpu.A=%02x (exp: 0x30)", cpu.A)
	}
}

func TestJRNZ(t *testing.T) {
	var prog = []byte{
		0xcb, 0x47, //     bit 0, a
		0x20, 0x02, //     jr nz, T1
		0x06, 0x00, //     ld b, 0
		0x06, 0x01} // T1: ld b, 1

	memory := &TestMemory{}
	memory.WriteBuffer(0, prog)

	cpu := Z80Cpu{Mem: memory}
	cpu.A = 0
	cpu.ExecOne()
	cpu.ExecOne()
	cpu.ExecOne()

	if cpu.B != 0 {
		t.Errorf("cpu.B=%d (exp: 0)", cpu.B)
	}

	cpu.Reset()
	cpu.A = 1
	cpu.ExecOne()
	cpu.ExecOne()
	cpu.ExecOne()

	if cpu.B != 1 {
		t.Errorf("cpu.B=%d (exp: 1)", cpu.B)
	}
}

func TestRl(t *testing.T) {
	var prog = []byte{
		0x06, 0x0a, // ld b, 10
		0xcb, 0x10, // rl b
		0xcb, 0x10, // rl b
	}
	memory := &TestMemory{}
	memory.WriteBuffer(0, prog)

	cpu := Z80Cpu{Mem: memory}
	cpu.ExecOne()
	cpu.ExecOne()
	cpu.ExecOne()

	if cpu.B != 40 {
		t.Errorf("cpu.B=%d (exp: 40)", cpu.B)
	}
}

func TestCp(t *testing.T) {
	var prog = []byte{
		0xb8, // cp b
	}
	memory := &TestMemory{}
	memory.WriteBuffer(0, prog)

	cpu := Z80Cpu{Mem: memory}
	cpu.A = 10
	cpu.B = 20
	cpu.ExecOne()
	if cpu.flagWasZero {
		t.Errorf("flag zero should not be set")
	}

	cpu.Reset()
	cpu.A = 10
	cpu.B = 10
	cpu.ExecOne()
	if !cpu.flagWasZero {
		t.Errorf("flag zero should be set")
	}
}

func TestSBC(t *testing.T) {
	var prog = []byte{
		0xde, 0x80, // SBC A, 0x80
	}
	memory := &TestMemory{}
	memory.WriteBuffer(0, prog)

	cpu := Z80Cpu{Mem: memory}
	cpu.A = 0

	cpu.ExecOne()
	if cpu.A != 0x80 || !cpu.flagCarry {
		t.Errorf("cpu.A=%02x (exp 0x80), cpu.flagCarry=%v (exp: true)", cpu.A, cpu.flagCarry)
	}
}

func TestBit(t *testing.T) {
	var prog = []byte{
		0x3e, 0x10, // ld a, 0x10
		0xcb, 0x67, // bit 4, a
		0xcb, 0x47, // bit 0, a
	}

	memory := &TestMemory{}
	memory.WriteBuffer(0, prog)

	cpu := Z80Cpu{Mem: memory}
	cpu.ExecOne()
	cpu.ExecOne()
	if cpu.flagWasZero {
		t.Errorf("flagWasZero should be false")
	}

	cpu.ExecOne()
	if !cpu.flagWasZero {
		t.Errorf("flagWasZero should be true")
	}
}

func TestPushPop(t *testing.T) {
	var prog = []byte{
		0xe5, // push hl
		0xe1, // pop hl
	}

	memory := &TestMemory{}
	memory.WriteBuffer(0, prog)

	cpu := Z80Cpu{Mem: memory}
	cpu.Reset()
	cpu.H = 33
	cpu.L = 22

	cpu.ExecOne()
	cpu.ExecOne()

	if cpu.H != 33 || cpu.L != 22 {
		t.Errorf("cpu.H=%d (exp: 33); cpu.L=%d (exp: 2)", cpu.H, cpu.L)
	}
}

func TestPushPopFlags(t *testing.T) {
	var prog = []byte{
		0xf5, // push af
		0xf1, // pop af
	}

	memory := &TestMemory{}
	memory.WriteBuffer(0, prog)

	cpu := Z80Cpu{Mem: memory}
	cpu.Reset()
	cpu.A = 33
	cpu.flagCarry = true
	cpu.flagHalfCarry = false
	cpu.flagWasSub = true
	cpu.flagWasZero = false

	cpu.ExecOne()
	cpu.ExecOne()
	if cpu.A != 33 || !cpu.flagCarry || cpu.flagHalfCarry || !cpu.flagWasSub || cpu.flagWasZero {
		t.Errorf(
			"cpu.A=%d (exp: 33); "+
				"cpu.flagCarry=%v (exp: true); "+
				"cpu.flagHalfCarry=%v (exp: false); "+
				"cpu.flagWasSub=%v (exp: true); "+
				"cpu.flagWasZero=%v (exp: false)",
			cpu.A, cpu.flagCarry, cpu.flagHalfCarry, cpu.flagWasSub, cpu.flagWasZero)
	}
}

func TestRegcoupleInc(t *testing.T) {
	var prog = []byte{
		0x03, // inc bc
		0x03, // inc bc
		0x03, // inc bc
		0x03, // inc bc
	}
	memory := &TestMemory{}
	memory.WriteBuffer(0, prog)

	cpu := Z80Cpu{Mem: memory}
	cpu.ExecOne()
	cpu.ExecOne()
	cpu.ExecOne()

	if cpu.B != 0 || cpu.C != 3 {
		t.Errorf("cpu.B=%d (exp: 0); cpu.C=%d (exp: 3)", cpu.B, cpu.C)
	}

	cpu.B = 1
	cpu.C = 255
	cpu.ExecOne()

	if cpu.B != 2 || cpu.C != 0 {
		t.Errorf("cpu.B=%d (exp: 2); cpu.C=%d (exp: 0)", cpu.B, cpu.C)
	}
}

func TestCallRet(t *testing.T) {
	var prog = []byte{
		0xcd, 0x04, 0x11, // 00: call 0x1104
		0x76, //             03: halt
		0xc9, //             04: ret
	}
	memory := &TestMemory{}
	memory.WriteBuffer(0x1100, prog)

	cpu := MakeZ80Cpu(memory)
	cpu.PC = 0x1100
	cpu.ExecOne()
	if cpu.Mem.Read(cpu.SP) != 0x03 || cpu.Mem.Read(cpu.SP+1) != 0x11 {
		t.Errorf("Invalid address on stack %02x%02x", cpu.Mem.Read(cpu.SP), cpu.Mem.Read(cpu.SP+1))
	}
	cpu.ExecOne()
	if cpu.PC != 0x1103 {
		t.Errorf("Invalid ret address %04x", cpu.PC)
	}
	cpu.ExecOne()
	if !cpu.IsHalted {
		t.Errorf("CPU is not Halted")
	}
}

func TestEIIsDeferred(t *testing.T) {
	memory := &TestMemory{}
	memory.WriteBuffer(0, []uint8{
		0xfb, // ei
		0x00, // nop
		0x76, // halt
	})

	cpu := MakeZ80Cpu(memory)
	cpu.RegisterInterrupt(Z80Interrupt{Name: "vblank", Mask: 1, Addr: 0x40})
	cpu.IE = 1
	cpu.IF = 1

	cpu.ExecOne()
	if cpu.PC != 1 || cpu.interruptsEnabled || cpu.interruptEnableDelay != 1 {
		t.Fatalf("after EI: PC=%04x IME=%v delay=%d", cpu.PC, cpu.interruptsEnabled, cpu.interruptEnableDelay)
	}

	cpu.ExecOne()
	if cpu.PC != 2 || !cpu.interruptsEnabled || cpu.interruptEnableDelay != 0 {
		t.Fatalf("after instruction following EI: PC=%04x IME=%v delay=%d", cpu.PC, cpu.interruptsEnabled, cpu.interruptEnableDelay)
	}

	cpu.ExecOne()
	if cpu.PC != 0x40 {
		t.Fatalf("interrupt was not taken after deferred EI, PC=%04x", cpu.PC)
	}
	if cpu.SP != 0xfd || memory.Read(0xfd) != 0x02 || memory.Read(0xfe) != 0x00 {
		t.Fatalf("unexpected stack after interrupt: SP=%04x [%02x %02x]", cpu.SP, memory.Read(0xfd), memory.Read(0xfe))
	}
}

func TestDICancelsDeferredEI(t *testing.T) {
	memory := &TestMemory{}
	memory.WriteBuffer(0, []uint8{
		0xfb, // ei
		0xf3, // di
		0x76, // halt
	})

	cpu := MakeZ80Cpu(memory)
	cpu.RegisterInterrupt(Z80Interrupt{Name: "vblank", Mask: 1, Addr: 0x40})
	cpu.IE = 1
	cpu.IF = 1

	cpu.ExecOne()
	cpu.ExecOne()
	if cpu.interruptsEnabled || cpu.interruptEnableDelay != 0 {
		t.Fatalf("DI did not cancel deferred EI: IME=%v delay=%d", cpu.interruptsEnabled, cpu.interruptEnableDelay)
	}

	cpu.ExecOne()
	if !cpu.IsHalted || cpu.PC != 3 {
		t.Fatalf("expected HALT to execute instead of taking interrupt: halted=%v PC=%04x", cpu.IsHalted, cpu.PC)
	}
}

func TestRETIEnablesInterruptsImmediately(t *testing.T) {
	memory := &TestMemory{}
	memory.WriteBuffer(0, []uint8{
		0x76, // halt
	})
	memory.WriteBuffer(0x40, []uint8{
		0xd9, // reti
	})

	cpu := MakeZ80Cpu(memory)
	cpu.RegisterInterrupt(Z80Interrupt{Name: "vblank", Mask: 1, Addr: 0x40})
	cpu.interruptsEnabled = true
	cpu.IE = 1
	cpu.IF = 1

	cpu.ExecOne()
	if cpu.PC != 0x40 || cpu.interruptsEnabled {
		t.Fatalf("expected interrupt dispatch to disable IME and jump to vector, PC=%04x IME=%v", cpu.PC, cpu.interruptsEnabled)
	}

	cpu.ExecOne()
	if cpu.PC != 0 || !cpu.interruptsEnabled || cpu.interruptEnableDelay != 0 {
		t.Fatalf("RETI did not restore PC/IME immediately: PC=%04x IME=%v delay=%d", cpu.PC, cpu.interruptsEnabled, cpu.interruptEnableDelay)
	}

	cpu.IF = 1
	cpu.ExecOne()
	if cpu.PC != 0x40 {
		t.Fatalf("interrupt was not taken immediately after RETI, PC=%04x", cpu.PC)
	}
}

func TestProgHLToHex(t *testing.T) {
	var prog = []byte{
		0x00, 0x00, 0x00, // 00: nop (x3)
		0x00,             // 03: nop
		0xcd, 0x08, 0x00, // 04: call 0x08
		0x76,       //       07: halt
		0x3e, 0x30, //       08: ld a, 0x30
		0xd3, 0x01, //       0a: out (0x01), a
		0x3e, 0x78, //       0c: ld a, 0x78
		0xd3, 0x01, //       0e: out (0x01), a
		0x4c,             // 10: ld c, h
		0xcd, 0x19, 0x00, // 11: call 0x19
		0x4d,             // 14: ld c, l
		0xcd, 0x19, 0x00, // 15: call 0x19
		0xc9,             // 18: ret
		0x79,             // 19: ld a, c
		0x1f,             // 1a: rra
		0x1f,             // 1b: rra
		0x1f,             // 1c: rra
		0x1f,             // 1d: rra
		0xcd, 0x22, 0x00, // 1e: call 0x22
		0x79,       //       21: ld a, c
		0xe6, 0x0f, //       22: and 0x0f
		0xc6, 0x90, //       24: add a, 0x90
		0x27,       //       26: daa
		0xce, 0x40, //       27: adc a,0x40
		0x27,       //       29: daa
		0xd3, 0x01, //       2a: out (0x01), a
		0xc9} //             2c: ret

	memory := &TestMemory{}
	memory.WriteBuffer(0, prog)

	cpu := MakeZ80Cpu(memory)

	runProg := func(inp uint16) string {
		cpu.Reset()

		cpu.H = uint8(inp >> 8)
		cpu.L = uint8(inp & 0xff)

		for !cpu.IsHalted {
			cpu.ExecOne()
		}
		return string(cpu.OutBuffer)
	}

	out := runProg(0xdead)
	if out != "0xDEAD" {
		t.Errorf("output=%s, expected 0xDEAD", out)
	}

	out = runProg(0xbeef)
	if out != "0xBEEF" {
		t.Errorf("output=%s, expected 0xBEEF", out)
	}
}
