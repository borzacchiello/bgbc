package z80cpu

import (
	"encoding/gob"
	"fmt"
)

type Memory interface {
	Read(uint16) uint8
	Write(uint16, uint8)
}

type Z80Interrupt struct {
	Name string
	Mask uint8
	Addr uint16
}

type Z80Cpu struct {
	Mem                 Memory
	A, B, C, D, E, H, L uint8
	SP, PC              uint16

	flagWasZero, flagWasSub, flagHalfCarry, flagCarry bool

	// InterruptEnable and InterruptFlag regs
	IE, IF uint8

	branchWasTaken                 bool
	IsHalted                       bool
	IsStopped                      bool
	UseExternalInterruptController bool
	interruptsEnabled              bool
	interruptEnableDelay           uint8

	Interrupts []Z80Interrupt

	// Used to hold "OUT" data
	OutBuffer []byte

	// Z80 disassembler, for debugging
	EnableDisas bool
	Disas       Z80Disas
}

func MakeZ80Cpu(mem Memory) *Z80Cpu {
	cpu := &Z80Cpu{
		Mem: mem,
	}
	cpu.Reset()
	return cpu
}

func (cpu *Z80Cpu) Reset() {
	cpu.SP = 0xff
	cpu.PC = 0
	cpu.OutBuffer = make([]byte, 0)
	cpu.IsHalted = false
	cpu.IsStopped = false
	cpu.interruptsEnabled = false
	cpu.interruptEnableDelay = 0
}

func panicIfErr(e error) {
	if e != nil {
		panic(e)
	}
}

func (cpu *Z80Cpu) Save(encoder *gob.Encoder) {
	panicIfErr(encoder.Encode(cpu.A))
	panicIfErr(encoder.Encode(cpu.B))
	panicIfErr(encoder.Encode(cpu.C))
	panicIfErr(encoder.Encode(cpu.D))
	panicIfErr(encoder.Encode(cpu.E))
	panicIfErr(encoder.Encode(cpu.H))
	panicIfErr(encoder.Encode(cpu.L))
	panicIfErr(encoder.Encode(cpu.SP))
	panicIfErr(encoder.Encode(cpu.PC))
	panicIfErr(encoder.Encode(cpu.IE))
	panicIfErr(encoder.Encode(cpu.IF))
	panicIfErr(encoder.Encode(cpu.flagWasZero))
	panicIfErr(encoder.Encode(cpu.flagWasSub))
	panicIfErr(encoder.Encode(cpu.flagHalfCarry))
	panicIfErr(encoder.Encode(cpu.flagCarry))
	panicIfErr(encoder.Encode(cpu.branchWasTaken))
	panicIfErr(encoder.Encode(cpu.IsHalted))
	panicIfErr(encoder.Encode(cpu.IsStopped))
	panicIfErr(encoder.Encode(cpu.interruptsEnabled))
	panicIfErr(encoder.Encode(cpu.interruptEnableDelay))
}

func (cpu *Z80Cpu) Load(decoder *gob.Decoder) error {
	errs := []error{
		decoder.Decode(&cpu.A),
		decoder.Decode(&cpu.B),
		decoder.Decode(&cpu.C),
		decoder.Decode(&cpu.D),
		decoder.Decode(&cpu.E),
		decoder.Decode(&cpu.H),
		decoder.Decode(&cpu.L),
		decoder.Decode(&cpu.SP),
		decoder.Decode(&cpu.PC),
		decoder.Decode(&cpu.IE),
		decoder.Decode(&cpu.IF),
		decoder.Decode(&cpu.flagWasZero),
		decoder.Decode(&cpu.flagWasSub),
		decoder.Decode(&cpu.flagHalfCarry),
		decoder.Decode(&cpu.flagCarry),
		decoder.Decode(&cpu.branchWasTaken),
		decoder.Decode(&cpu.IsHalted),
		decoder.Decode(&cpu.IsStopped),
		decoder.Decode(&cpu.interruptsEnabled),
		decoder.Decode(&cpu.interruptEnableDelay),
	}

	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func (cpu *Z80Cpu) RegisterInterrupt(interrupt Z80Interrupt) {
	cpu.Interrupts = append(cpu.Interrupts, interrupt)
}

func (cpu *Z80Cpu) SetInterrupt(mask uint8) {
	cpu.IF |= mask
}

func (cpu *Z80Cpu) PendingInterrupts() uint8 {
	return cpu.IE & cpu.IF & 0x1F
}

func (cpu *Z80Cpu) InterruptsEnabled() bool {
	return cpu.interruptsEnabled
}

func (cpu *Z80Cpu) BeginInterruptDispatch() {
	cpu.IsHalted = false
	cpu.interruptsEnabled = false
}

func (cpu *Z80Cpu) SelectPendingInterrupt() (Z80Interrupt, bool) {
	interruptValue := cpu.PendingInterrupts()
	for _, interrupt := range cpu.Interrupts {
		if interruptValue&interrupt.Mask != 0 {
			return interrupt, true
		}
	}
	return Z80Interrupt{}, false
}

func (cpu *Z80Cpu) DispatchInterrupt(interrupt Z80Interrupt) {
	cpu.IF &= ^interrupt.Mask
	cpu.StackPush16(cpu.PC)
	cpu.PC = interrupt.Addr
}

func (cpu *Z80Cpu) fetchOpcode() uint8 {
	opcode := cpu.Mem.Read(cpu.PC)
	cpu.PC += 1
	return opcode
}

func (cpu *Z80Cpu) handleInterrupts() bool {
	if cpu.PendingInterrupts() != 0 {
		cpu.IsHalted = false
	}

	if !cpu.interruptsEnabled {
		return false
	}

	interrupt, ok := cpu.SelectPendingInterrupt()
	if !ok {
		return false
	}

	cpu.BeginInterruptDispatch()
	cpu.DispatchInterrupt(interrupt)
	return true
}

func (cpu *Z80Cpu) ExecOne() int {
	if !cpu.UseExternalInterruptController {
		inInterrupt := cpu.handleInterrupts()
		if inInterrupt {
			return 5
		}
	}

	if cpu.IsHalted {
		return 1
	}

	if cpu.EnableDisas {
		_, disas_str := cpu.Disas.DisassembleOneFromCPU(cpu)
		fmt.Println(disas_str)
	}

	isCBOpcode := false
	cb_opcode := uint8(0)
	opcode := cpu.fetchOpcode()
	if opcode == 0xcb {
		isCBOpcode = true
		cb_opcode = cpu.Mem.Read(cpu.PC)
	}

	cpu.branchWasTaken = false
	handler := handlers[opcode]
	handler(cpu)

	if cpu.interruptEnableDelay > 0 {
		cpu.interruptEnableDelay -= 1
		if cpu.interruptEnableDelay == 0 {
			cpu.interruptsEnabled = true
		}
	}

	ticks := 0
	if isCBOpcode {
		ticks += int(ticks_cb[cb_opcode])
	} else if cpu.branchWasTaken {
		ticks += int(ticks_branched[opcode])
	} else {
		ticks += int(ticks_opcode[opcode])
	}

	return ticks
}

func (cpu *Z80Cpu) PackFlags() uint8 {
	res := 0
	if cpu.flagWasZero {
		res |= 0x80
	}
	if cpu.flagWasSub {
		res |= 0x40
	}
	if cpu.flagHalfCarry {
		res |= 0x20
	}
	if cpu.flagCarry {
		res |= 0x10
	}
	return uint8(res)
}

func (cpu *Z80Cpu) UnpackFlags(f uint8) {
	cpu.flagWasZero = f&0x80 != 0
	cpu.flagWasSub = f&0x40 != 0
	cpu.flagHalfCarry = f&0x20 != 0
	cpu.flagCarry = f&0x10 != 0
}

func (cpu *Z80Cpu) getPC8() uint8 {
	v := cpu.Mem.Read(cpu.PC)

	cpu.PC += 1
	return v
}

func (cpu *Z80Cpu) getPC16() uint16 {
	l := cpu.Mem.Read(cpu.PC)
	h := cpu.Mem.Read(cpu.PC + 1)

	cpu.PC += 2
	return (uint16(h) << 8) | uint16(l)
}

func (cpu *Z80Cpu) StackPush16(val uint16) {
	cpu.SP -= 1
	cpu.Mem.Write(cpu.SP, uint8(val>>8))

	cpu.SP -= 1
	cpu.Mem.Write(cpu.SP, uint8(val&0xff))
}

func (cpu *Z80Cpu) StackPop16() uint16 {
	low := cpu.Mem.Read(cpu.SP)
	cpu.SP += 1
	high := cpu.Mem.Read(cpu.SP)
	cpu.SP += 1

	return uint16(high)<<8 | uint16(low)
}

func pack_regcouple(b, a uint8) uint16 {
	return (uint16(b) << 8) | uint16(a)
}

func unpack_regcouple(a uint16) (uint8, uint8) {
	return uint8(a >> 8 & 0xff), uint8(a & 0xff)
}

/*
 *  OPCODE HANDLERS
 */

// LD
func handler_ld_R_16(cpu *Z80Cpu, dst1, dst2 *uint8, value uint16) {
	*dst1, *dst2 = unpack_regcouple(value)
}

func handler_ld_R_16_2(cpu *Z80Cpu, dst *uint16, value uint16) {
	*dst = value
}

func handler_ld_R_8(cpu *Z80Cpu, dst *uint8, value uint8) {
	*dst = value
}

func handler_ld_MEM_8(cpu *Z80Cpu, dst_addr uint16, src uint8) {
	cpu.Mem.Write(dst_addr, src)
}

func handler_ld_MEM_16(cpu *Z80Cpu, dst_addr uint16, src uint16) {
	cpu.Mem.Write(dst_addr, uint8(src&0xff))
	cpu.Mem.Write(dst_addr+1, uint8((src>>8)&0xff))
}

func handler_ld_R_MEM_8(cpu *Z80Cpu, dst *uint8, addr uint16) {
	*dst = cpu.Mem.Read(addr)
}

// LDI
func handler_ldi_R_MEM(cpu *Z80Cpu, dst *uint8, addr uint16) {
	*dst = cpu.Mem.Read(addr)
	cpu.H, cpu.L = unpack_regcouple(pack_regcouple(cpu.H, cpu.L) + 1)
}

func handler_ldi_MEM_R(cpu *Z80Cpu, addr uint16, val uint8) {
	cpu.Mem.Write(addr, val)
	cpu.H, cpu.L = unpack_regcouple(pack_regcouple(cpu.H, cpu.L) + 1)
}

// LDD
func handler_ldd_R_MEM(cpu *Z80Cpu, dst *uint8, addr uint16) {
	*dst = cpu.Mem.Read(addr)
	cpu.H, cpu.L = unpack_regcouple(pack_regcouple(cpu.H, cpu.L) - 1)
}

func handler_ldd_MEM_R(cpu *Z80Cpu, addr uint16, val uint8) {
	cpu.Mem.Write(addr, val)
	cpu.H, cpu.L = unpack_regcouple(pack_regcouple(cpu.H, cpu.L) - 1)
}

// LDHL
func handler_ldhl(cpu *Z80Cpu) {
	v1 := int(cpu.SP)
	v2 := int(int8(cpu.getPC8()))
	r := v1 + v2

	cpu.H, cpu.L = unpack_regcouple(uint16(r))

	cpu.flagWasZero = false
	cpu.flagWasSub = false
	cpu.flagHalfCarry = (v1^v2^(r&0xFFFF))&0x10 == 0x10
	cpu.flagCarry = (v1^v2^(r&0xFFFF))&0x100 == 0x100
}

// INC
func handler_inc_R_8(cpu *Z80Cpu, dst *uint8) {
	*dst = *dst + 1

	cpu.flagWasZero = *dst == 0
	cpu.flagWasSub = false
	cpu.flagHalfCarry = *dst&0xf == 0
}

func handler_inc_R_16(cpu *Z80Cpu, dst1, dst2 *uint8) {
	v := pack_regcouple(*dst1, *dst2)
	v += 1
	*dst1, *dst2 = unpack_regcouple(v)
}

func handler_inc_R_16_2(cpu *Z80Cpu, dst *uint16) {
	*dst += 1
}

func handler_inc_MEM(cpu *Z80Cpu, addr uint16) {
	val := cpu.Mem.Read(addr) + 1
	cpu.Mem.Write(addr, val)

	cpu.flagWasZero = val == 0
	cpu.flagWasSub = false
	cpu.flagHalfCarry = val&0xf == 0
}

// DEC
func handler_dec_R_8(cpu *Z80Cpu, dst *uint8) {
	*dst = *dst - 1

	cpu.flagWasZero = *dst == 0
	cpu.flagWasSub = true
	cpu.flagHalfCarry = *dst&0xf == 0xf
}

func handler_dec_R_16(cpu *Z80Cpu, dst1, dst2 *uint8) {
	dst := pack_regcouple(*dst1, *dst2)
	dst -= 1
	*dst1, *dst2 = unpack_regcouple(dst)
}

func handler_dec_R_16_2(cpu *Z80Cpu, dst *uint16) {
	*dst -= 1
}

func handler_dec_MEM(cpu *Z80Cpu, addr uint16) {
	val := cpu.Mem.Read(addr) - 1
	cpu.Mem.Write(addr, val)

	cpu.flagWasZero = val == 0
	cpu.flagWasSub = true
	cpu.flagHalfCarry = val&0xf == 0xf
}

// ADD
func handler_add_R_8(cpu *Z80Cpu, dst *uint8, src uint8) {
	old_dst := *dst
	res := uint16(old_dst) + uint16(src)
	*dst = uint8(res & 0xff)

	cpu.flagWasZero = *dst == 0
	cpu.flagWasSub = false
	cpu.flagHalfCarry = (old_dst&0xf)+(src&0xf) > 0xf
	cpu.flagCarry = res&0x100 != 0
}

func handler_add_R_16(cpu *Z80Cpu, dst1, dst2 *uint8, src uint16) {
	dst := pack_regcouple(*dst1, *dst2)
	res := dst + src
	*dst1, *dst2 = unpack_regcouple(res)

	cpu.flagWasSub = false
	cpu.flagHalfCarry = ((dst&0xfff)+(src&0xfff) > 0xfff)
	cpu.flagCarry = int(dst)+int(src) > 0xffff
}

func handler_add_sp(cpu *Z80Cpu) {
	val := int(cpu.SP)
	mod := int(int8(cpu.getPC8()))
	res := val + mod
	cpu.SP = uint16(res)

	cpu.flagWasZero = false
	cpu.flagWasSub = false
	cpu.flagHalfCarry = (val^mod^(res&0xFFFF))&0x10 == 0x10
	cpu.flagCarry = (val^mod^(res&0xFFFF))&0x100 == 0x100
}

// ADC
func handler_adc_R_8(cpu *Z80Cpu, dst *uint8, val uint8) {
	res := *dst
	var carry uint8 = 0
	if cpu.flagCarry {
		carry = 1
	}

	var result uint16 = uint16(res) + uint16(val) + uint16(carry)

	cpu.flagWasZero = uint8(result) == 0
	cpu.flagWasSub = false
	cpu.flagHalfCarry = (*dst&0xf)+(val&0xf)+carry > 0xf
	cpu.flagCarry = result > 0xff

	*dst = uint8(result)
}

// SUB
func handler_sub_R_8(cpu *Z80Cpu, dst *uint8, val uint8) {
	res := *dst - val

	cpu.flagWasZero = res == 0
	cpu.flagWasSub = true
	cpu.flagCarry = *dst < val
	cpu.flagHalfCarry = ((*dst&0xf)-(val&0xf))>>7 != 0

	*dst = res
}

// SBC
func handler_sbc_R_8(cpu *Z80Cpu, dst *uint8, val uint8) {
	var carry uint8 = 0
	if cpu.flagCarry {
		carry = 1
	}

	a := int(*dst)
	b := int(val)
	c := int(carry)

	result := a - b - c

	*dst = uint8(result)

	cpu.flagWasZero = uint8(result) == 0
	cpu.flagWasSub = true
	cpu.flagHalfCarry = (a&0xf)-(b&0xf)-c < 0
	cpu.flagCarry = result < 0
}

// AND
func handler_and_R_8(cpu *Z80Cpu, dst *uint8, value uint8) {
	*dst = *dst & value

	cpu.flagWasZero = *dst == 0
	cpu.flagHalfCarry = true
	cpu.flagCarry = false
	cpu.flagWasSub = false
}

// XOR
func handler_xor_R_8(cpu *Z80Cpu, dst *uint8, value uint8) {
	*dst = *dst ^ value

	cpu.flagWasZero = *dst == 0
	cpu.flagHalfCarry = false
	cpu.flagCarry = false
	cpu.flagWasSub = false
}

// OR
func handler_or_R_8(cpu *Z80Cpu, dst *uint8, value uint8) {
	*dst = *dst | value

	cpu.flagWasZero = *dst == 0
	cpu.flagHalfCarry = false
	cpu.flagCarry = false
	cpu.flagWasSub = false
}

// CP
func handler_cp(cpu *Z80Cpu, v1, v2 uint8) {
	res := v1 - v2

	cpu.flagWasZero = res == 0
	cpu.flagWasSub = true
	cpu.flagCarry = v1 < v2
	cpu.flagHalfCarry = ((v1&0xf)-(v2&0xf))>>7 != 0
}

// CPL
func handler_cpl(cpu *Z80Cpu) {
	cpu.A = ^cpu.A

	cpu.flagWasSub = true
	cpu.flagHalfCarry = true
}

// DAA
func handler_daa(cpu *Z80Cpu) {
	val := cpu.A

	var inc uint8 = 0
	if cpu.flagCarry {
		inc = 0x60
	}

	if cpu.flagHalfCarry || (!cpu.flagWasSub && (val&0xf > 9)) {
		inc |= 0x6
	}
	if cpu.flagCarry || (!cpu.flagWasSub && val > 0x99) {
		inc |= 0x60
	}
	if cpu.flagWasSub {
		val -= inc
	} else {
		val += inc
	}

	if (uint16(inc)<<2)&0x100 != 0 {
		cpu.flagCarry = true
	}

	cpu.flagHalfCarry = false
	cpu.flagWasZero = val == 0

	cpu.A = val
}

// SRL
func handler_srl_R(cpu *Z80Cpu, dst *uint8) {
	c := *dst & 1
	*dst >>= 1

	cpu.flagCarry = c != 0
	cpu.flagWasZero = *dst == 0
	cpu.flagHalfCarry = false
	cpu.flagWasSub = false
}

func handler_srl_MEM(cpu *Z80Cpu, addr uint16) {
	val := cpu.Mem.Read(addr)
	c := val & 1
	val >>= 1
	cpu.Mem.Write(addr, val)

	cpu.flagCarry = c != 0
	cpu.flagWasZero = val == 0
	cpu.flagHalfCarry = false
	cpu.flagWasSub = false
}

// RL
func handler_rlc_R(cpu *Z80Cpu, dst *uint8) {
	carry := *dst >> 7
	res := *dst<<1 | carry
	*dst = res

	cpu.flagWasZero = res == 0
	cpu.flagWasSub = false
	cpu.flagHalfCarry = false
	cpu.flagCarry = carry != 0
}

func handler_rlc_MEM(cpu *Z80Cpu, addr uint16) {
	val := cpu.Mem.Read(addr)
	carry := val >> 7
	res := val<<1 | carry
	cpu.Mem.Write(addr, res)

	cpu.flagWasZero = res == 0
	cpu.flagWasSub = false
	cpu.flagHalfCarry = false
	cpu.flagCarry = carry != 0
}

func handler_rl_R(cpu *Z80Cpu, dst *uint8) {
	carry := *dst >> 7

	*dst = *dst << 1
	if cpu.flagCarry {
		*dst |= 1
	}

	cpu.flagWasZero = *dst == 0
	cpu.flagWasSub = false
	cpu.flagHalfCarry = false
	cpu.flagCarry = carry != 0
}

func handler_rl_MEM(cpu *Z80Cpu, addr uint16) {
	val := cpu.Mem.Read(addr)
	carry := val >> 7

	val = val << 1
	if cpu.flagCarry {
		val |= 1
	}

	cpu.Mem.Write(addr, val)

	cpu.flagWasZero = val == 0
	cpu.flagWasSub = false
	cpu.flagHalfCarry = false
	cpu.flagCarry = carry != 0
}

// RR
func handler_rrc_R(cpu *Z80Cpu, dst *uint8) {
	carry := *dst & 1
	res := carry<<7 | *dst>>1
	*dst = res

	cpu.flagWasZero = res == 0
	cpu.flagWasSub = false
	cpu.flagHalfCarry = false
	cpu.flagCarry = carry != 0
}

func handler_rrc_MEM(cpu *Z80Cpu, addr uint16) {
	val := cpu.Mem.Read(addr)
	carry := val & 1
	res := carry<<7 | val>>1
	cpu.Mem.Write(addr, res)

	cpu.flagWasZero = res == 0
	cpu.flagWasSub = false
	cpu.flagHalfCarry = false
	cpu.flagCarry = carry != 0
}

func handler_rr_R(cpu *Z80Cpu, dst *uint8) {
	carry := *dst & 1

	*dst >>= 1
	if cpu.flagCarry {
		*dst |= 1 << 7
	}

	cpu.flagWasZero = *dst == 0
	cpu.flagWasSub = false
	cpu.flagHalfCarry = false
	cpu.flagCarry = carry != 0
}

func handler_rr_MEM(cpu *Z80Cpu, addr uint16) {
	val := cpu.Mem.Read(addr)
	carry := val & 1

	val >>= 1
	if cpu.flagCarry {
		val |= 1 << 7
	}
	cpu.Mem.Write(addr, val)

	cpu.flagWasZero = val == 0
	cpu.flagWasSub = false
	cpu.flagHalfCarry = false
	cpu.flagCarry = carry != 0
}

// SLA
func handler_sla_R(cpu *Z80Cpu, dst *uint8) {
	carry := *dst >> 7
	*dst <<= 1

	cpu.flagWasZero = *dst == 0
	cpu.flagCarry = carry != 0
	cpu.flagHalfCarry = false
	cpu.flagWasSub = false
}

func handler_sla_MEM(cpu *Z80Cpu, addr uint16) {
	val := cpu.Mem.Read(addr)
	carry := val >> 7
	val <<= 1
	cpu.Mem.Write(addr, val)

	cpu.flagWasZero = val == 0
	cpu.flagCarry = carry != 0
	cpu.flagHalfCarry = false
	cpu.flagWasSub = false
}

// SRA
func handler_sra_R(cpu *Z80Cpu, dst *uint8) {
	carry := *dst & 1
	signbit := *dst >> 7
	*dst >>= 1
	*dst |= signbit << 7

	cpu.flagWasZero = *dst == 0
	cpu.flagCarry = carry != 0
	cpu.flagHalfCarry = false
	cpu.flagWasSub = false
}

func handler_sra_MEM(cpu *Z80Cpu, addr uint16) {
	val := cpu.Mem.Read(addr)
	carry := val & 1
	signbit := val >> 7
	val >>= 1
	val |= signbit << 7
	cpu.Mem.Write(addr, val)

	cpu.flagWasZero = val == 0
	cpu.flagCarry = carry != 0
	cpu.flagHalfCarry = false
	cpu.flagWasSub = false
}

// BIT
func handler_bit(cpu *Z80Cpu, bit int, value uint8) {
	cpu.flagWasZero = ((value >> bit) & 1) == 0
	cpu.flagHalfCarry = true
	cpu.flagWasSub = false
}

// RES
func handler_res_R(cpu *Z80Cpu, bit int, dst *uint8) {
	var mask uint8 = ^(1 << bit)
	*dst &= mask
}

func handler_res_MEM(cpu *Z80Cpu, bit int, addr uint16) {
	var mask uint8 = ^(1 << bit)

	val := cpu.Mem.Read(addr)
	cpu.Mem.Write(addr, val&mask)
}

// SET
func handler_set_R(cpu *Z80Cpu, bit int, dst *uint8) {
	*dst |= 1 << bit
}

func handler_set_MEM(cpu *Z80Cpu, bit int, addr uint16) {
	val := cpu.Mem.Read(addr)
	val |= 1 << bit
	cpu.Mem.Write(addr, val)
}

// SWAP
func handler_swap_R(cpu *Z80Cpu, dst *uint8) {
	low := *dst & 0xF
	hig := *dst >> 4
	*dst = (low << 4) | hig

	cpu.flagWasZero = *dst == 0
	cpu.flagCarry = false
	cpu.flagHalfCarry = false
	cpu.flagWasSub = false
}

func handler_swap_MEM(cpu *Z80Cpu, addr uint16) {
	val := cpu.Mem.Read(addr)
	low := val & 0xF
	hig := val >> 4
	val = (low << 4) | hig
	cpu.Mem.Write(addr, val)

	cpu.flagWasZero = val == 0
	cpu.flagCarry = false
	cpu.flagHalfCarry = false
	cpu.flagWasSub = false
}

// RET
func handler_ret(cpu *Z80Cpu) {
	cpu.PC = cpu.StackPop16()
}

func handler_ret_IF(cpu *Z80Cpu, cond bool) {
	if cond {
		cpu.branchWasTaken = true
		cpu.PC = cpu.StackPop16()
	}
}

// CALL
func handler_call(cpu *Z80Cpu) {
	addr := cpu.getPC16()
	cpu.StackPush16(cpu.PC)
	cpu.PC = addr
}

func handler_call_IF(cpu *Z80Cpu, cond bool) {
	addr := cpu.getPC16()
	if cond {
		cpu.branchWasTaken = true
		cpu.StackPush16(cpu.PC)
		cpu.PC = addr
	}
}

// JR
func handler_jr(cpu *Z80Cpu, off int8) {
	cpu.PC = uint16(int(cpu.PC) + 1 + int(off))
}

func handler_jr_IF(cpu *Z80Cpu, off int8, cond bool) {
	if cond {
		cpu.branchWasTaken = true
		cpu.PC = uint16(int(cpu.PC) + 1 + int(off))
	} else {
		cpu.PC += 1
	}
}

// JP
func handler_jp(cpu *Z80Cpu, dst uint16) {
	cpu.PC = dst
}

func handler_jp_IF(cpu *Z80Cpu, dst uint16, cond bool) {
	if cond {
		cpu.branchWasTaken = true
		cpu.PC = dst
	}
}

// OTHER
func handler_di(cpu *Z80Cpu) {
	cpu.interruptsEnabled = false
	cpu.interruptEnableDelay = 0
}

func handler_ei(cpu *Z80Cpu) {
	cpu.interruptEnableDelay = 2
}

func handler_reti(cpu *Z80Cpu) {
	handler_ret(cpu)
	cpu.interruptsEnabled = true
	cpu.interruptEnableDelay = 0
}

func handler_rst(cpu *Z80Cpu, val uint16) {
	cpu.StackPush16(cpu.PC)
	cpu.PC = val
}

func handler_scf(cpu *Z80Cpu) {
	cpu.flagCarry = true
	cpu.flagHalfCarry = false
	cpu.flagWasSub = false
}

func handler_ccf(cpu *Z80Cpu) {
	cpu.flagCarry = !cpu.flagCarry
	cpu.flagHalfCarry = false
	cpu.flagWasSub = false
}

func handler_out(cpu *Z80Cpu, value uint8) {
	// FIXME: do not ignore the port
	_ = cpu.getPC8()
	cpu.OutBuffer = append(cpu.OutBuffer, value)
}

func handler_halt(cpu *Z80Cpu) {
	cpu.IsHalted = true
}

func handler_nop() {}

func handler_stop(cpu *Z80Cpu) {
	// It should be a two byte opcode
	_ = cpu.getPC8()
	cpu.IsStopped = true
}

func handler_undefined(cpu *Z80Cpu, opcode uint8) {
	fmt.Printf("Executing an undefined opcode: %02x, halting\n", opcode)
	cpu.IsHalted = true
}

var handlers = [256]func(*Z80Cpu){
	func(cpu *Z80Cpu) { handler_nop() },                                                             // 00
	func(cpu *Z80Cpu) { handler_ld_R_16(cpu, &cpu.B, &cpu.C, cpu.getPC16()) },                       // 01
	func(cpu *Z80Cpu) { handler_ld_MEM_8(cpu, pack_regcouple(cpu.B, cpu.C), cpu.A) },                // 02
	func(cpu *Z80Cpu) { handler_inc_R_16(cpu, &cpu.B, &cpu.C) },                                     // 03
	func(cpu *Z80Cpu) { handler_inc_R_8(cpu, &cpu.B) },                                              // 04
	func(cpu *Z80Cpu) { handler_dec_R_8(cpu, &cpu.B) },                                              // 05
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.B, cpu.getPC8()) },                                 // 06
	func(cpu *Z80Cpu) { handler_rlc_R(cpu, &cpu.A); cpu.flagWasZero = false /* rlca */ },            // 07
	func(cpu *Z80Cpu) { handler_ld_MEM_16(cpu, cpu.getPC16(), cpu.SP) },                             // 08
	func(cpu *Z80Cpu) { handler_add_R_16(cpu, &cpu.H, &cpu.L, pack_regcouple(cpu.B, cpu.C)) },       // 09
	func(cpu *Z80Cpu) { handler_ld_R_MEM_8(cpu, &cpu.A, pack_regcouple(cpu.B, cpu.C)) },             // 0A
	func(cpu *Z80Cpu) { handler_dec_R_16(cpu, &cpu.B, &cpu.C) },                                     // 0B
	func(cpu *Z80Cpu) { handler_inc_R_8(cpu, &cpu.C) },                                              // 0C
	func(cpu *Z80Cpu) { handler_dec_R_8(cpu, &cpu.C) },                                              // 0D
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.C, cpu.getPC8()) },                                 // 0E
	func(cpu *Z80Cpu) { handler_rrc_R(cpu, &cpu.A); cpu.flagWasZero = false /* rrca */ },            // 0F
	func(cpu *Z80Cpu) { handler_stop(cpu) },                                                         // 10
	func(cpu *Z80Cpu) { handler_ld_R_16(cpu, &cpu.D, &cpu.E, cpu.getPC16()) },                       // 11
	func(cpu *Z80Cpu) { handler_ld_MEM_8(cpu, pack_regcouple(cpu.D, cpu.E), cpu.A) },                // 12
	func(cpu *Z80Cpu) { handler_inc_R_16(cpu, &cpu.D, &cpu.E) },                                     // 13
	func(cpu *Z80Cpu) { handler_inc_R_8(cpu, &cpu.D) },                                              // 14
	func(cpu *Z80Cpu) { handler_dec_R_8(cpu, &cpu.D) },                                              // 15
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.D, cpu.getPC8()) },                                 // 16
	func(cpu *Z80Cpu) { handler_rl_R(cpu, &cpu.A); cpu.flagWasZero = false /* rla */ },              // 17
	func(cpu *Z80Cpu) { handler_jr(cpu, int8(cpu.Mem.Read(cpu.PC))) },                               // 18
	func(cpu *Z80Cpu) { handler_add_R_16(cpu, &cpu.H, &cpu.L, pack_regcouple(cpu.D, cpu.E)) },       // 19
	func(cpu *Z80Cpu) { handler_ld_R_MEM_8(cpu, &cpu.A, pack_regcouple(cpu.D, cpu.E)) },             // 1A
	func(cpu *Z80Cpu) { handler_dec_R_16(cpu, &cpu.D, &cpu.E) },                                     // 1B
	func(cpu *Z80Cpu) { handler_inc_R_8(cpu, &cpu.E) },                                              // 1C
	func(cpu *Z80Cpu) { handler_dec_R_8(cpu, &cpu.E) },                                              // 1D
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.E, cpu.getPC8()) },                                 // 1E
	func(cpu *Z80Cpu) { handler_rr_R(cpu, &cpu.A); cpu.flagWasZero = false /* rra */ },              // 1F
	func(cpu *Z80Cpu) { handler_jr_IF(cpu, int8(cpu.Mem.Read(cpu.PC)), !cpu.flagWasZero) },          // 20
	func(cpu *Z80Cpu) { handler_ld_R_16(cpu, &cpu.H, &cpu.L, cpu.getPC16()) },                       // 21
	func(cpu *Z80Cpu) { handler_ldi_MEM_R(cpu, pack_regcouple(cpu.H, cpu.L), cpu.A) },               // 22
	func(cpu *Z80Cpu) { handler_inc_R_16(cpu, &cpu.H, &cpu.L) },                                     // 23
	func(cpu *Z80Cpu) { handler_inc_R_8(cpu, &cpu.H) },                                              // 24
	func(cpu *Z80Cpu) { handler_dec_R_8(cpu, &cpu.H) },                                              // 25
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.H, cpu.getPC8()) },                                 // 26
	func(cpu *Z80Cpu) { handler_daa(cpu) },                                                          // 27
	func(cpu *Z80Cpu) { handler_jr_IF(cpu, int8(cpu.Mem.Read(cpu.PC)), cpu.flagWasZero) },           // 28
	func(cpu *Z80Cpu) { handler_add_R_16(cpu, &cpu.H, &cpu.L, pack_regcouple(cpu.H, cpu.L)) },       // 29
	func(cpu *Z80Cpu) { handler_ldi_R_MEM(cpu, &cpu.A, pack_regcouple(cpu.H, cpu.L)) },              // 2A
	func(cpu *Z80Cpu) { handler_dec_R_16(cpu, &cpu.H, &cpu.L) },                                     // 2B
	func(cpu *Z80Cpu) { handler_inc_R_8(cpu, &cpu.L) },                                              // 2C
	func(cpu *Z80Cpu) { handler_dec_R_8(cpu, &cpu.L) },                                              // 2D
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.L, cpu.getPC8()) },                                 // 2E
	func(cpu *Z80Cpu) { handler_cpl(cpu) },                                                          // 2F
	func(cpu *Z80Cpu) { handler_jr_IF(cpu, int8(cpu.Mem.Read(cpu.PC)), !cpu.flagCarry) },            // 30
	func(cpu *Z80Cpu) { handler_ld_R_16_2(cpu, &cpu.SP, cpu.getPC16()) },                            // 31
	func(cpu *Z80Cpu) { handler_ldd_MEM_R(cpu, pack_regcouple(cpu.H, cpu.L), cpu.A) },               // 32
	func(cpu *Z80Cpu) { handler_inc_R_16_2(cpu, &cpu.SP) },                                          // 33
	func(cpu *Z80Cpu) { handler_inc_MEM(cpu, pack_regcouple(cpu.H, cpu.L)) },                        // 34
	func(cpu *Z80Cpu) { handler_dec_MEM(cpu, pack_regcouple(cpu.H, cpu.L)) },                        // 35
	func(cpu *Z80Cpu) { handler_ld_MEM_8(cpu, pack_regcouple(cpu.H, cpu.L), cpu.getPC8()) },         // 36
	func(cpu *Z80Cpu) { handler_scf(cpu) },                                                          // 37
	func(cpu *Z80Cpu) { handler_jr_IF(cpu, int8(cpu.Mem.Read(cpu.PC)), cpu.flagCarry) },             // 38
	func(cpu *Z80Cpu) { handler_add_R_16(cpu, &cpu.H, &cpu.L, cpu.SP) },                             // 39
	func(cpu *Z80Cpu) { handler_ldd_R_MEM(cpu, &cpu.A, pack_regcouple(cpu.H, cpu.L)) },              // 3A
	func(cpu *Z80Cpu) { handler_dec_R_16_2(cpu, &cpu.SP) },                                          // 3B
	func(cpu *Z80Cpu) { handler_inc_R_8(cpu, &cpu.A) },                                              // 3C
	func(cpu *Z80Cpu) { handler_dec_R_8(cpu, &cpu.A) },                                              // 3D
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.A, cpu.getPC8()) },                                 // 3E
	func(cpu *Z80Cpu) { handler_ccf(cpu) },                                                          // 3F
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.B, cpu.B) },                                        // 40
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.B, cpu.C) },                                        // 41
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.B, cpu.D) },                                        // 42
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.B, cpu.E) },                                        // 43
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.B, cpu.H) },                                        // 44
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.B, cpu.L) },                                        // 45
	func(cpu *Z80Cpu) { handler_ld_R_MEM_8(cpu, &cpu.B, pack_regcouple(cpu.H, cpu.L)) },             // 46
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.B, cpu.A) },                                        // 47
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.C, cpu.B) },                                        // 48
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.C, cpu.C) },                                        // 49
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.C, cpu.D) },                                        // 4A
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.C, cpu.E) },                                        // 4B
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.C, cpu.H) },                                        // 4C
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.C, cpu.L) },                                        // 4D
	func(cpu *Z80Cpu) { handler_ld_R_MEM_8(cpu, &cpu.C, pack_regcouple(cpu.H, cpu.L)) },             // 4E
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.C, cpu.A) },                                        // 4F
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.D, cpu.B) },                                        // 50
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.D, cpu.C) },                                        // 51
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.D, cpu.D) },                                        // 52
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.D, cpu.E) },                                        // 53
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.D, cpu.H) },                                        // 54
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.D, cpu.L) },                                        // 55
	func(cpu *Z80Cpu) { handler_ld_R_MEM_8(cpu, &cpu.D, pack_regcouple(cpu.H, cpu.L)) },             // 56
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.D, cpu.A) },                                        // 57
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.E, cpu.B) },                                        // 58
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.E, cpu.C) },                                        // 59
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.E, cpu.D) },                                        // 5A
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.E, cpu.E) },                                        // 5B
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.E, cpu.H) },                                        // 5C
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.E, cpu.L) },                                        // 5D
	func(cpu *Z80Cpu) { handler_ld_R_MEM_8(cpu, &cpu.E, pack_regcouple(cpu.H, cpu.L)) },             // 5E
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.E, cpu.A) },                                        // 5F
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.H, cpu.B) },                                        // 60
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.H, cpu.C) },                                        // 61
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.H, cpu.D) },                                        // 62
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.H, cpu.E) },                                        // 63
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.H, cpu.H) },                                        // 64
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.H, cpu.L) },                                        // 65
	func(cpu *Z80Cpu) { handler_ld_R_MEM_8(cpu, &cpu.H, pack_regcouple(cpu.H, cpu.L)) },             // 66
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.H, cpu.A) },                                        // 67
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.L, cpu.B) },                                        // 68
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.L, cpu.C) },                                        // 69
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.L, cpu.D) },                                        // 6A
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.L, cpu.E) },                                        // 6B
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.L, cpu.H) },                                        // 6C
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.L, cpu.L) },                                        // 6D
	func(cpu *Z80Cpu) { handler_ld_R_MEM_8(cpu, &cpu.L, pack_regcouple(cpu.H, cpu.L)) },             // 6E
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.L, cpu.A) },                                        // 6F
	func(cpu *Z80Cpu) { handler_ld_MEM_8(cpu, pack_regcouple(cpu.H, cpu.L), cpu.B) },                // 70
	func(cpu *Z80Cpu) { handler_ld_MEM_8(cpu, pack_regcouple(cpu.H, cpu.L), cpu.C) },                // 71
	func(cpu *Z80Cpu) { handler_ld_MEM_8(cpu, pack_regcouple(cpu.H, cpu.L), cpu.D) },                // 72
	func(cpu *Z80Cpu) { handler_ld_MEM_8(cpu, pack_regcouple(cpu.H, cpu.L), cpu.E) },                // 73
	func(cpu *Z80Cpu) { handler_ld_MEM_8(cpu, pack_regcouple(cpu.H, cpu.L), cpu.H) },                // 74
	func(cpu *Z80Cpu) { handler_ld_MEM_8(cpu, pack_regcouple(cpu.H, cpu.L), cpu.L) },                // 75
	func(cpu *Z80Cpu) { handler_halt(cpu) },                                                         // 76
	func(cpu *Z80Cpu) { handler_ld_MEM_8(cpu, pack_regcouple(cpu.H, cpu.L), cpu.A) },                // 77
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.A, cpu.B) },                                        // 78
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.A, cpu.C) },                                        // 79
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.A, cpu.D) },                                        // 7A
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.A, cpu.E) },                                        // 7B
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.A, cpu.H) },                                        // 7C
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.A, cpu.L) },                                        // 7D
	func(cpu *Z80Cpu) { handler_ld_R_MEM_8(cpu, &cpu.A, pack_regcouple(cpu.H, cpu.L)) },             // 7E
	func(cpu *Z80Cpu) { handler_ld_R_8(cpu, &cpu.A, cpu.A) },                                        // 7F
	func(cpu *Z80Cpu) { handler_add_R_8(cpu, &cpu.A, cpu.B) },                                       // 80
	func(cpu *Z80Cpu) { handler_add_R_8(cpu, &cpu.A, cpu.C) },                                       // 81
	func(cpu *Z80Cpu) { handler_add_R_8(cpu, &cpu.A, cpu.D) },                                       // 82
	func(cpu *Z80Cpu) { handler_add_R_8(cpu, &cpu.A, cpu.E) },                                       // 83
	func(cpu *Z80Cpu) { handler_add_R_8(cpu, &cpu.A, cpu.H) },                                       // 84
	func(cpu *Z80Cpu) { handler_add_R_8(cpu, &cpu.A, cpu.L) },                                       // 85
	func(cpu *Z80Cpu) { handler_add_R_8(cpu, &cpu.A, cpu.Mem.Read(pack_regcouple(cpu.H, cpu.L))) },  // 86
	func(cpu *Z80Cpu) { handler_add_R_8(cpu, &cpu.A, cpu.A) },                                       // 87
	func(cpu *Z80Cpu) { handler_adc_R_8(cpu, &cpu.A, cpu.B) },                                       // 88
	func(cpu *Z80Cpu) { handler_adc_R_8(cpu, &cpu.A, cpu.C) },                                       // 89
	func(cpu *Z80Cpu) { handler_adc_R_8(cpu, &cpu.A, cpu.D) },                                       // 8A
	func(cpu *Z80Cpu) { handler_adc_R_8(cpu, &cpu.A, cpu.E) },                                       // 8B
	func(cpu *Z80Cpu) { handler_adc_R_8(cpu, &cpu.A, cpu.H) },                                       // 8C
	func(cpu *Z80Cpu) { handler_adc_R_8(cpu, &cpu.A, cpu.L) },                                       // 8D
	func(cpu *Z80Cpu) { handler_adc_R_8(cpu, &cpu.A, cpu.Mem.Read(pack_regcouple(cpu.H, cpu.L))) },  // 8E
	func(cpu *Z80Cpu) { handler_adc_R_8(cpu, &cpu.A, cpu.A) },                                       // 8F
	func(cpu *Z80Cpu) { handler_sub_R_8(cpu, &cpu.A, cpu.B) },                                       // 90
	func(cpu *Z80Cpu) { handler_sub_R_8(cpu, &cpu.A, cpu.C) },                                       // 91
	func(cpu *Z80Cpu) { handler_sub_R_8(cpu, &cpu.A, cpu.D) },                                       // 92
	func(cpu *Z80Cpu) { handler_sub_R_8(cpu, &cpu.A, cpu.E) },                                       // 93
	func(cpu *Z80Cpu) { handler_sub_R_8(cpu, &cpu.A, cpu.H) },                                       // 94
	func(cpu *Z80Cpu) { handler_sub_R_8(cpu, &cpu.A, cpu.L) },                                       // 95
	func(cpu *Z80Cpu) { handler_sub_R_8(cpu, &cpu.A, cpu.Mem.Read(pack_regcouple(cpu.H, cpu.L))) },  // 96
	func(cpu *Z80Cpu) { handler_sub_R_8(cpu, &cpu.A, cpu.A) },                                       // 97
	func(cpu *Z80Cpu) { handler_sbc_R_8(cpu, &cpu.A, cpu.B) },                                       // 98
	func(cpu *Z80Cpu) { handler_sbc_R_8(cpu, &cpu.A, cpu.C) },                                       // 99
	func(cpu *Z80Cpu) { handler_sbc_R_8(cpu, &cpu.A, cpu.D) },                                       // 9A
	func(cpu *Z80Cpu) { handler_sbc_R_8(cpu, &cpu.A, cpu.E) },                                       // 9B
	func(cpu *Z80Cpu) { handler_sbc_R_8(cpu, &cpu.A, cpu.H) },                                       // 9C
	func(cpu *Z80Cpu) { handler_sbc_R_8(cpu, &cpu.A, cpu.L) },                                       // 9D
	func(cpu *Z80Cpu) { handler_sbc_R_8(cpu, &cpu.A, cpu.Mem.Read(pack_regcouple(cpu.H, cpu.L))) },  // 9E
	func(cpu *Z80Cpu) { handler_sbc_R_8(cpu, &cpu.A, cpu.A) },                                       // 9F
	func(cpu *Z80Cpu) { handler_and_R_8(cpu, &cpu.A, cpu.B) },                                       // A0
	func(cpu *Z80Cpu) { handler_and_R_8(cpu, &cpu.A, cpu.C) },                                       // A1
	func(cpu *Z80Cpu) { handler_and_R_8(cpu, &cpu.A, cpu.D) },                                       // A2
	func(cpu *Z80Cpu) { handler_and_R_8(cpu, &cpu.A, cpu.E) },                                       // A3
	func(cpu *Z80Cpu) { handler_and_R_8(cpu, &cpu.A, cpu.H) },                                       // A4
	func(cpu *Z80Cpu) { handler_and_R_8(cpu, &cpu.A, cpu.L) },                                       // A5
	func(cpu *Z80Cpu) { handler_and_R_8(cpu, &cpu.A, cpu.Mem.Read(pack_regcouple(cpu.H, cpu.L))) },  // A6
	func(cpu *Z80Cpu) { handler_and_R_8(cpu, &cpu.A, cpu.A) },                                       // A7
	func(cpu *Z80Cpu) { handler_xor_R_8(cpu, &cpu.A, cpu.B) },                                       // A8
	func(cpu *Z80Cpu) { handler_xor_R_8(cpu, &cpu.A, cpu.C) },                                       // A9
	func(cpu *Z80Cpu) { handler_xor_R_8(cpu, &cpu.A, cpu.D) },                                       // AA
	func(cpu *Z80Cpu) { handler_xor_R_8(cpu, &cpu.A, cpu.E) },                                       // AB
	func(cpu *Z80Cpu) { handler_xor_R_8(cpu, &cpu.A, cpu.H) },                                       // AC
	func(cpu *Z80Cpu) { handler_xor_R_8(cpu, &cpu.A, cpu.L) },                                       // AD
	func(cpu *Z80Cpu) { handler_xor_R_8(cpu, &cpu.A, cpu.Mem.Read(pack_regcouple(cpu.H, cpu.L))) },  // AE
	func(cpu *Z80Cpu) { handler_xor_R_8(cpu, &cpu.A, cpu.A) },                                       // AF
	func(cpu *Z80Cpu) { handler_or_R_8(cpu, &cpu.A, cpu.B) },                                        // B0
	func(cpu *Z80Cpu) { handler_or_R_8(cpu, &cpu.A, cpu.C) },                                        // B1
	func(cpu *Z80Cpu) { handler_or_R_8(cpu, &cpu.A, cpu.D) },                                        // B2
	func(cpu *Z80Cpu) { handler_or_R_8(cpu, &cpu.A, cpu.E) },                                        // B3
	func(cpu *Z80Cpu) { handler_or_R_8(cpu, &cpu.A, cpu.H) },                                        // B4
	func(cpu *Z80Cpu) { handler_or_R_8(cpu, &cpu.A, cpu.L) },                                        // B5
	func(cpu *Z80Cpu) { handler_or_R_8(cpu, &cpu.A, cpu.Mem.Read(pack_regcouple(cpu.H, cpu.L))) },   // B6
	func(cpu *Z80Cpu) { handler_or_R_8(cpu, &cpu.A, cpu.A) },                                        // B7
	func(cpu *Z80Cpu) { handler_cp(cpu, cpu.A, cpu.B) },                                             // B8
	func(cpu *Z80Cpu) { handler_cp(cpu, cpu.A, cpu.C) },                                             // B9
	func(cpu *Z80Cpu) { handler_cp(cpu, cpu.A, cpu.D) },                                             // BA
	func(cpu *Z80Cpu) { handler_cp(cpu, cpu.A, cpu.E) },                                             // BB
	func(cpu *Z80Cpu) { handler_cp(cpu, cpu.A, cpu.H) },                                             // BC
	func(cpu *Z80Cpu) { handler_cp(cpu, cpu.A, cpu.L) },                                             // BD
	func(cpu *Z80Cpu) { handler_cp(cpu, cpu.A, cpu.Mem.Read(pack_regcouple(cpu.H, cpu.L))) },        // BE
	func(cpu *Z80Cpu) { handler_cp(cpu, cpu.A, cpu.A) },                                             // BF
	func(cpu *Z80Cpu) { handler_ret_IF(cpu, !cpu.flagWasZero) },                                     // C0
	func(cpu *Z80Cpu) { cpu.B, cpu.C = unpack_regcouple(cpu.StackPop16()) },                         // C1
	func(cpu *Z80Cpu) { handler_jp_IF(cpu, cpu.getPC16(), !cpu.flagWasZero) },                       // C2
	func(cpu *Z80Cpu) { handler_jp(cpu, cpu.getPC16()) },                                            // C3
	func(cpu *Z80Cpu) { handler_call_IF(cpu, !cpu.flagWasZero) },                                    // C4
	func(cpu *Z80Cpu) { cpu.StackPush16(pack_regcouple(cpu.B, cpu.C)) },                             // C5
	func(cpu *Z80Cpu) { handler_add_R_8(cpu, &cpu.A, cpu.getPC8()) },                                // C6
	func(cpu *Z80Cpu) { handler_rst(cpu, 0) },                                                       // C7
	func(cpu *Z80Cpu) { handler_ret_IF(cpu, cpu.flagWasZero) },                                      // C8
	func(cpu *Z80Cpu) { handler_ret(cpu) },                                                          // C9
	func(cpu *Z80Cpu) { handler_jp_IF(cpu, cpu.getPC16(), cpu.flagWasZero) },                        // CA
	func(cpu *Z80Cpu) { cb_handlers[cpu.getPC8()](cpu) },                                            // CB
	func(cpu *Z80Cpu) { handler_call_IF(cpu, cpu.flagWasZero) },                                     // CC
	func(cpu *Z80Cpu) { handler_call(cpu) },                                                         // CD
	func(cpu *Z80Cpu) { handler_adc_R_8(cpu, &cpu.A, cpu.getPC8()) },                                // CE
	func(cpu *Z80Cpu) { handler_rst(cpu, 8) },                                                       // CF
	func(cpu *Z80Cpu) { handler_ret_IF(cpu, !cpu.flagCarry) },                                       // D0
	func(cpu *Z80Cpu) { cpu.D, cpu.E = unpack_regcouple(cpu.StackPop16()) },                         // D1
	func(cpu *Z80Cpu) { handler_jp_IF(cpu, cpu.getPC16(), !cpu.flagCarry) },                         // D2
	func(cpu *Z80Cpu) { handler_out(cpu, cpu.A) },                                                   // D3
	func(cpu *Z80Cpu) { handler_call_IF(cpu, !cpu.flagCarry) },                                      // D4
	func(cpu *Z80Cpu) { cpu.StackPush16(pack_regcouple(cpu.D, cpu.E)) },                             // D5
	func(cpu *Z80Cpu) { handler_sub_R_8(cpu, &cpu.A, cpu.getPC8()) },                                // D6
	func(cpu *Z80Cpu) { handler_rst(cpu, 16) },                                                      // D7
	func(cpu *Z80Cpu) { handler_ret_IF(cpu, cpu.flagCarry) },                                        // D8
	func(cpu *Z80Cpu) { handler_reti(cpu) },                                                         // D9
	func(cpu *Z80Cpu) { handler_jp_IF(cpu, cpu.getPC16(), cpu.flagCarry) },                          // DA
	func(cpu *Z80Cpu) { handler_undefined(cpu, 0xdb) },                                              // DB
	func(cpu *Z80Cpu) { handler_call_IF(cpu, cpu.flagCarry) },                                       // DC
	func(cpu *Z80Cpu) { handler_undefined(cpu, 0xdd) },                                              // DD
	func(cpu *Z80Cpu) { handler_sbc_R_8(cpu, &cpu.A, cpu.getPC8()) },                                // DE
	func(cpu *Z80Cpu) { handler_rst(cpu, 24) },                                                      // DF
	func(cpu *Z80Cpu) { handler_ld_MEM_8(cpu, uint16(cpu.getPC8())+0xFF00, cpu.A) },                 // E0
	func(cpu *Z80Cpu) { cpu.H, cpu.L = unpack_regcouple(cpu.StackPop16()) },                         // E1
	func(cpu *Z80Cpu) { handler_ld_MEM_8(cpu, uint16(cpu.C)+0xFF00, cpu.A) },                        // E2
	func(cpu *Z80Cpu) { handler_undefined(cpu, 0xe3) },                                              // E3
	func(cpu *Z80Cpu) { handler_undefined(cpu, 0xe4) },                                              // E4
	func(cpu *Z80Cpu) { cpu.StackPush16(pack_regcouple(cpu.H, cpu.L)) },                             // E5
	func(cpu *Z80Cpu) { handler_and_R_8(cpu, &cpu.A, cpu.getPC8()) },                                // E6
	func(cpu *Z80Cpu) { handler_rst(cpu, 32) },                                                      // E7
	func(cpu *Z80Cpu) { handler_add_sp(cpu) },                                                       // E8
	func(cpu *Z80Cpu) { handler_jp(cpu, pack_regcouple(cpu.H, cpu.L)) },                             // E9
	func(cpu *Z80Cpu) { handler_ld_MEM_8(cpu, cpu.getPC16(), cpu.A) },                               // EA
	func(cpu *Z80Cpu) { handler_undefined(cpu, 0xeb) },                                              // EB
	func(cpu *Z80Cpu) { handler_undefined(cpu, 0xec) },                                              // EC
	func(cpu *Z80Cpu) { handler_undefined(cpu, 0xed) },                                              // ED
	func(cpu *Z80Cpu) { handler_xor_R_8(cpu, &cpu.A, cpu.getPC8()) },                                // EE
	func(cpu *Z80Cpu) { handler_rst(cpu, 40) },                                                      // EF
	func(cpu *Z80Cpu) { handler_ld_R_MEM_8(cpu, &cpu.A, uint16(cpu.getPC8())+0xFF00) },              // F0
	func(cpu *Z80Cpu) { a, f := unpack_regcouple(cpu.StackPop16()); cpu.A = a; cpu.UnpackFlags(f) }, // F1
	func(cpu *Z80Cpu) { handler_ld_R_MEM_8(cpu, &cpu.A, uint16(cpu.C)+0xFF00) },                     // F2
	func(cpu *Z80Cpu) { handler_di(cpu) },                                                           // F3
	func(cpu *Z80Cpu) { handler_undefined(cpu, 0xf4) },                                              // F4
	func(cpu *Z80Cpu) { cpu.StackPush16(pack_regcouple(cpu.A, cpu.PackFlags())) },                   // F5
	func(cpu *Z80Cpu) { handler_or_R_8(cpu, &cpu.A, cpu.getPC8()) },                                 // F6
	func(cpu *Z80Cpu) { handler_rst(cpu, 48) },                                                      // F7
	func(cpu *Z80Cpu) { handler_ldhl(cpu) },                                                         // F8
	func(cpu *Z80Cpu) { handler_ld_R_16_2(cpu, &cpu.SP, pack_regcouple(cpu.H, cpu.L)) },             // F9
	func(cpu *Z80Cpu) { handler_ld_R_MEM_8(cpu, &cpu.A, cpu.getPC16()) },                            // FA
	func(cpu *Z80Cpu) { handler_ei(cpu) },                                                           // FB
	func(cpu *Z80Cpu) { handler_undefined(cpu, 0xfc) },                                              // FC
	func(cpu *Z80Cpu) { handler_undefined(cpu, 0xfd) },                                              // FD
	func(cpu *Z80Cpu) { handler_cp(cpu, cpu.A, cpu.getPC8()) },                                      // FE
	func(cpu *Z80Cpu) { handler_rst(cpu, 56) },                                                      // FF
}

var cb_handlers = [256]func(*Z80Cpu){
	func(cpu *Z80Cpu) { handler_rlc_R(cpu, &cpu.B) },                                      // 00
	func(cpu *Z80Cpu) { handler_rlc_R(cpu, &cpu.C) },                                      // 01
	func(cpu *Z80Cpu) { handler_rlc_R(cpu, &cpu.D) },                                      // 02
	func(cpu *Z80Cpu) { handler_rlc_R(cpu, &cpu.E) },                                      // 03
	func(cpu *Z80Cpu) { handler_rlc_R(cpu, &cpu.H) },                                      // 04
	func(cpu *Z80Cpu) { handler_rlc_R(cpu, &cpu.L) },                                      // 05
	func(cpu *Z80Cpu) { handler_rlc_MEM(cpu, pack_regcouple(cpu.H, cpu.L)) },              // 06
	func(cpu *Z80Cpu) { handler_rlc_R(cpu, &cpu.A) },                                      // 07
	func(cpu *Z80Cpu) { handler_rrc_R(cpu, &cpu.B) },                                      // 08
	func(cpu *Z80Cpu) { handler_rrc_R(cpu, &cpu.C) },                                      // 09
	func(cpu *Z80Cpu) { handler_rrc_R(cpu, &cpu.D) },                                      // 0A
	func(cpu *Z80Cpu) { handler_rrc_R(cpu, &cpu.E) },                                      // 0B
	func(cpu *Z80Cpu) { handler_rrc_R(cpu, &cpu.H) },                                      // 0C
	func(cpu *Z80Cpu) { handler_rrc_R(cpu, &cpu.L) },                                      // 0D
	func(cpu *Z80Cpu) { handler_rrc_MEM(cpu, pack_regcouple(cpu.H, cpu.L)) },              // 0E
	func(cpu *Z80Cpu) { handler_rrc_R(cpu, &cpu.A) },                                      // 0F
	func(cpu *Z80Cpu) { handler_rl_R(cpu, &cpu.B) },                                       // 10
	func(cpu *Z80Cpu) { handler_rl_R(cpu, &cpu.C) },                                       // 11
	func(cpu *Z80Cpu) { handler_rl_R(cpu, &cpu.D) },                                       // 12
	func(cpu *Z80Cpu) { handler_rl_R(cpu, &cpu.E) },                                       // 13
	func(cpu *Z80Cpu) { handler_rl_R(cpu, &cpu.H) },                                       // 14
	func(cpu *Z80Cpu) { handler_rl_R(cpu, &cpu.L) },                                       // 15
	func(cpu *Z80Cpu) { handler_rl_MEM(cpu, pack_regcouple(cpu.H, cpu.L)) },               // 16
	func(cpu *Z80Cpu) { handler_rl_R(cpu, &cpu.A) },                                       // 17
	func(cpu *Z80Cpu) { handler_rr_R(cpu, &cpu.B) },                                       // 18
	func(cpu *Z80Cpu) { handler_rr_R(cpu, &cpu.C) },                                       // 19
	func(cpu *Z80Cpu) { handler_rr_R(cpu, &cpu.D) },                                       // 1A
	func(cpu *Z80Cpu) { handler_rr_R(cpu, &cpu.E) },                                       // 1B
	func(cpu *Z80Cpu) { handler_rr_R(cpu, &cpu.H) },                                       // 1C
	func(cpu *Z80Cpu) { handler_rr_R(cpu, &cpu.L) },                                       // 1D
	func(cpu *Z80Cpu) { handler_rr_MEM(cpu, pack_regcouple(cpu.H, cpu.L)) },               // 1E
	func(cpu *Z80Cpu) { handler_rr_R(cpu, &cpu.A) },                                       // 1F
	func(cpu *Z80Cpu) { handler_sla_R(cpu, &cpu.B) },                                      // 20
	func(cpu *Z80Cpu) { handler_sla_R(cpu, &cpu.C) },                                      // 21
	func(cpu *Z80Cpu) { handler_sla_R(cpu, &cpu.D) },                                      // 22
	func(cpu *Z80Cpu) { handler_sla_R(cpu, &cpu.E) },                                      // 23
	func(cpu *Z80Cpu) { handler_sla_R(cpu, &cpu.H) },                                      // 24
	func(cpu *Z80Cpu) { handler_sla_R(cpu, &cpu.L) },                                      // 25
	func(cpu *Z80Cpu) { handler_sla_MEM(cpu, pack_regcouple(cpu.H, cpu.L)) },              // 26
	func(cpu *Z80Cpu) { handler_sla_R(cpu, &cpu.A) },                                      // 27
	func(cpu *Z80Cpu) { handler_sra_R(cpu, &cpu.B) },                                      // 28
	func(cpu *Z80Cpu) { handler_sra_R(cpu, &cpu.C) },                                      // 29
	func(cpu *Z80Cpu) { handler_sra_R(cpu, &cpu.D) },                                      // 2A
	func(cpu *Z80Cpu) { handler_sra_R(cpu, &cpu.E) },                                      // 2B
	func(cpu *Z80Cpu) { handler_sra_R(cpu, &cpu.H) },                                      // 2C
	func(cpu *Z80Cpu) { handler_sra_R(cpu, &cpu.L) },                                      // 2D
	func(cpu *Z80Cpu) { handler_sra_MEM(cpu, pack_regcouple(cpu.H, cpu.L)) },              // 2E
	func(cpu *Z80Cpu) { handler_sra_R(cpu, &cpu.A) },                                      // 2F
	func(cpu *Z80Cpu) { handler_swap_R(cpu, &cpu.B) },                                     // 30
	func(cpu *Z80Cpu) { handler_swap_R(cpu, &cpu.C) },                                     // 31
	func(cpu *Z80Cpu) { handler_swap_R(cpu, &cpu.D) },                                     // 32
	func(cpu *Z80Cpu) { handler_swap_R(cpu, &cpu.E) },                                     // 33
	func(cpu *Z80Cpu) { handler_swap_R(cpu, &cpu.H) },                                     // 34
	func(cpu *Z80Cpu) { handler_swap_R(cpu, &cpu.L) },                                     // 35
	func(cpu *Z80Cpu) { handler_swap_MEM(cpu, pack_regcouple(cpu.H, cpu.L)) },             // 36
	func(cpu *Z80Cpu) { handler_swap_R(cpu, &cpu.A) },                                     // 37
	func(cpu *Z80Cpu) { handler_srl_R(cpu, &cpu.B) },                                      // 38
	func(cpu *Z80Cpu) { handler_srl_R(cpu, &cpu.C) },                                      // 39
	func(cpu *Z80Cpu) { handler_srl_R(cpu, &cpu.D) },                                      // 3A
	func(cpu *Z80Cpu) { handler_srl_R(cpu, &cpu.E) },                                      // 3B
	func(cpu *Z80Cpu) { handler_srl_R(cpu, &cpu.H) },                                      // 3C
	func(cpu *Z80Cpu) { handler_srl_R(cpu, &cpu.L) },                                      // 3D
	func(cpu *Z80Cpu) { handler_srl_MEM(cpu, pack_regcouple(cpu.H, cpu.L)) },              // 3E
	func(cpu *Z80Cpu) { handler_srl_R(cpu, &cpu.A) },                                      // 3F
	func(cpu *Z80Cpu) { handler_bit(cpu, 0, cpu.B) },                                      // 40
	func(cpu *Z80Cpu) { handler_bit(cpu, 0, cpu.C) },                                      // 41
	func(cpu *Z80Cpu) { handler_bit(cpu, 0, cpu.D) },                                      // 42
	func(cpu *Z80Cpu) { handler_bit(cpu, 0, cpu.E) },                                      // 43
	func(cpu *Z80Cpu) { handler_bit(cpu, 0, cpu.H) },                                      // 44
	func(cpu *Z80Cpu) { handler_bit(cpu, 0, cpu.L) },                                      // 45
	func(cpu *Z80Cpu) { handler_bit(cpu, 0, cpu.Mem.Read(pack_regcouple(cpu.H, cpu.L))) }, // 46
	func(cpu *Z80Cpu) { handler_bit(cpu, 0, cpu.A) },                                      // 47
	func(cpu *Z80Cpu) { handler_bit(cpu, 1, cpu.B) },                                      // 48
	func(cpu *Z80Cpu) { handler_bit(cpu, 1, cpu.C) },                                      // 49
	func(cpu *Z80Cpu) { handler_bit(cpu, 1, cpu.D) },                                      // 4A
	func(cpu *Z80Cpu) { handler_bit(cpu, 1, cpu.E) },                                      // 4B
	func(cpu *Z80Cpu) { handler_bit(cpu, 1, cpu.H) },                                      // 4C
	func(cpu *Z80Cpu) { handler_bit(cpu, 1, cpu.L) },                                      // 4D
	func(cpu *Z80Cpu) { handler_bit(cpu, 1, cpu.Mem.Read(pack_regcouple(cpu.H, cpu.L))) }, // 4E
	func(cpu *Z80Cpu) { handler_bit(cpu, 1, cpu.A) },                                      // 4F
	func(cpu *Z80Cpu) { handler_bit(cpu, 2, cpu.B) },                                      // 50
	func(cpu *Z80Cpu) { handler_bit(cpu, 2, cpu.C) },                                      // 51
	func(cpu *Z80Cpu) { handler_bit(cpu, 2, cpu.D) },                                      // 52
	func(cpu *Z80Cpu) { handler_bit(cpu, 2, cpu.E) },                                      // 53
	func(cpu *Z80Cpu) { handler_bit(cpu, 2, cpu.H) },                                      // 54
	func(cpu *Z80Cpu) { handler_bit(cpu, 2, cpu.L) },                                      // 55
	func(cpu *Z80Cpu) { handler_bit(cpu, 2, cpu.Mem.Read(pack_regcouple(cpu.H, cpu.L))) }, // 56
	func(cpu *Z80Cpu) { handler_bit(cpu, 2, cpu.A) },                                      // 57
	func(cpu *Z80Cpu) { handler_bit(cpu, 3, cpu.B) },                                      // 58
	func(cpu *Z80Cpu) { handler_bit(cpu, 3, cpu.C) },                                      // 59
	func(cpu *Z80Cpu) { handler_bit(cpu, 3, cpu.D) },                                      // 5A
	func(cpu *Z80Cpu) { handler_bit(cpu, 3, cpu.E) },                                      // 5B
	func(cpu *Z80Cpu) { handler_bit(cpu, 3, cpu.H) },                                      // 5C
	func(cpu *Z80Cpu) { handler_bit(cpu, 3, cpu.L) },                                      // 5D
	func(cpu *Z80Cpu) { handler_bit(cpu, 3, cpu.Mem.Read(pack_regcouple(cpu.H, cpu.L))) }, // 5E
	func(cpu *Z80Cpu) { handler_bit(cpu, 3, cpu.A) },                                      // 5F
	func(cpu *Z80Cpu) { handler_bit(cpu, 4, cpu.B) },                                      // 60
	func(cpu *Z80Cpu) { handler_bit(cpu, 4, cpu.C) },                                      // 61
	func(cpu *Z80Cpu) { handler_bit(cpu, 4, cpu.D) },                                      // 62
	func(cpu *Z80Cpu) { handler_bit(cpu, 4, cpu.E) },                                      // 63
	func(cpu *Z80Cpu) { handler_bit(cpu, 4, cpu.H) },                                      // 64
	func(cpu *Z80Cpu) { handler_bit(cpu, 4, cpu.L) },                                      // 65
	func(cpu *Z80Cpu) { handler_bit(cpu, 4, cpu.Mem.Read(pack_regcouple(cpu.H, cpu.L))) }, // 66
	func(cpu *Z80Cpu) { handler_bit(cpu, 4, cpu.A) },                                      // 67
	func(cpu *Z80Cpu) { handler_bit(cpu, 5, cpu.B) },                                      // 68
	func(cpu *Z80Cpu) { handler_bit(cpu, 5, cpu.C) },                                      // 69
	func(cpu *Z80Cpu) { handler_bit(cpu, 5, cpu.D) },                                      // 6A
	func(cpu *Z80Cpu) { handler_bit(cpu, 5, cpu.E) },                                      // 6B
	func(cpu *Z80Cpu) { handler_bit(cpu, 5, cpu.H) },                                      // 6C
	func(cpu *Z80Cpu) { handler_bit(cpu, 5, cpu.L) },                                      // 6D
	func(cpu *Z80Cpu) { handler_bit(cpu, 5, cpu.Mem.Read(pack_regcouple(cpu.H, cpu.L))) }, // 6E
	func(cpu *Z80Cpu) { handler_bit(cpu, 5, cpu.A) },                                      // 6F
	func(cpu *Z80Cpu) { handler_bit(cpu, 6, cpu.B) },                                      // 70
	func(cpu *Z80Cpu) { handler_bit(cpu, 6, cpu.C) },                                      // 71
	func(cpu *Z80Cpu) { handler_bit(cpu, 6, cpu.D) },                                      // 72
	func(cpu *Z80Cpu) { handler_bit(cpu, 6, cpu.E) },                                      // 73
	func(cpu *Z80Cpu) { handler_bit(cpu, 6, cpu.H) },                                      // 74
	func(cpu *Z80Cpu) { handler_bit(cpu, 6, cpu.L) },                                      // 75
	func(cpu *Z80Cpu) { handler_bit(cpu, 6, cpu.Mem.Read(pack_regcouple(cpu.H, cpu.L))) }, // 76
	func(cpu *Z80Cpu) { handler_bit(cpu, 6, cpu.A) },                                      // 77
	func(cpu *Z80Cpu) { handler_bit(cpu, 7, cpu.B) },                                      // 78
	func(cpu *Z80Cpu) { handler_bit(cpu, 7, cpu.C) },                                      // 79
	func(cpu *Z80Cpu) { handler_bit(cpu, 7, cpu.D) },                                      // 7A
	func(cpu *Z80Cpu) { handler_bit(cpu, 7, cpu.E) },                                      // 7B
	func(cpu *Z80Cpu) { handler_bit(cpu, 7, cpu.H) },                                      // 7C
	func(cpu *Z80Cpu) { handler_bit(cpu, 7, cpu.L) },                                      // 7D
	func(cpu *Z80Cpu) { handler_bit(cpu, 7, cpu.Mem.Read(pack_regcouple(cpu.H, cpu.L))) }, // 7E
	func(cpu *Z80Cpu) { handler_bit(cpu, 7, cpu.A) },                                      // 7F
	func(cpu *Z80Cpu) { handler_res_R(cpu, 0, &cpu.B) },                                   // 80
	func(cpu *Z80Cpu) { handler_res_R(cpu, 0, &cpu.C) },                                   // 81
	func(cpu *Z80Cpu) { handler_res_R(cpu, 0, &cpu.D) },                                   // 82
	func(cpu *Z80Cpu) { handler_res_R(cpu, 0, &cpu.E) },                                   // 83
	func(cpu *Z80Cpu) { handler_res_R(cpu, 0, &cpu.H) },                                   // 84
	func(cpu *Z80Cpu) { handler_res_R(cpu, 0, &cpu.L) },                                   // 85
	func(cpu *Z80Cpu) { handler_res_MEM(cpu, 0, pack_regcouple(cpu.H, cpu.L)) },           // 86
	func(cpu *Z80Cpu) { handler_res_R(cpu, 0, &cpu.A) },                                   // 87
	func(cpu *Z80Cpu) { handler_res_R(cpu, 1, &cpu.B) },                                   // 88
	func(cpu *Z80Cpu) { handler_res_R(cpu, 1, &cpu.C) },                                   // 89
	func(cpu *Z80Cpu) { handler_res_R(cpu, 1, &cpu.D) },                                   // 8A
	func(cpu *Z80Cpu) { handler_res_R(cpu, 1, &cpu.E) },                                   // 8B
	func(cpu *Z80Cpu) { handler_res_R(cpu, 1, &cpu.H) },                                   // 8C
	func(cpu *Z80Cpu) { handler_res_R(cpu, 1, &cpu.L) },                                   // 8D
	func(cpu *Z80Cpu) { handler_res_MEM(cpu, 1, pack_regcouple(cpu.H, cpu.L)) },           // 8E
	func(cpu *Z80Cpu) { handler_res_R(cpu, 1, &cpu.A) },                                   // 8F
	func(cpu *Z80Cpu) { handler_res_R(cpu, 2, &cpu.B) },                                   // 90
	func(cpu *Z80Cpu) { handler_res_R(cpu, 2, &cpu.C) },                                   // 91
	func(cpu *Z80Cpu) { handler_res_R(cpu, 2, &cpu.D) },                                   // 92
	func(cpu *Z80Cpu) { handler_res_R(cpu, 2, &cpu.E) },                                   // 93
	func(cpu *Z80Cpu) { handler_res_R(cpu, 2, &cpu.H) },                                   // 94
	func(cpu *Z80Cpu) { handler_res_R(cpu, 2, &cpu.L) },                                   // 95
	func(cpu *Z80Cpu) { handler_res_MEM(cpu, 2, pack_regcouple(cpu.H, cpu.L)) },           // 96
	func(cpu *Z80Cpu) { handler_res_R(cpu, 2, &cpu.A) },                                   // 97
	func(cpu *Z80Cpu) { handler_res_R(cpu, 3, &cpu.B) },                                   // 98
	func(cpu *Z80Cpu) { handler_res_R(cpu, 3, &cpu.C) },                                   // 99
	func(cpu *Z80Cpu) { handler_res_R(cpu, 3, &cpu.D) },                                   // 9A
	func(cpu *Z80Cpu) { handler_res_R(cpu, 3, &cpu.E) },                                   // 9B
	func(cpu *Z80Cpu) { handler_res_R(cpu, 3, &cpu.H) },                                   // 9C
	func(cpu *Z80Cpu) { handler_res_R(cpu, 3, &cpu.L) },                                   // 9D
	func(cpu *Z80Cpu) { handler_res_MEM(cpu, 3, pack_regcouple(cpu.H, cpu.L)) },           // 9E
	func(cpu *Z80Cpu) { handler_res_R(cpu, 3, &cpu.A) },                                   // 9F
	func(cpu *Z80Cpu) { handler_res_R(cpu, 4, &cpu.B) },                                   // A0
	func(cpu *Z80Cpu) { handler_res_R(cpu, 4, &cpu.C) },                                   // A1
	func(cpu *Z80Cpu) { handler_res_R(cpu, 4, &cpu.D) },                                   // A2
	func(cpu *Z80Cpu) { handler_res_R(cpu, 4, &cpu.E) },                                   // A3
	func(cpu *Z80Cpu) { handler_res_R(cpu, 4, &cpu.H) },                                   // A4
	func(cpu *Z80Cpu) { handler_res_R(cpu, 4, &cpu.L) },                                   // A5
	func(cpu *Z80Cpu) { handler_res_MEM(cpu, 4, pack_regcouple(cpu.H, cpu.L)) },           // A6
	func(cpu *Z80Cpu) { handler_res_R(cpu, 4, &cpu.A) },                                   // A7
	func(cpu *Z80Cpu) { handler_res_R(cpu, 5, &cpu.B) },                                   // A8
	func(cpu *Z80Cpu) { handler_res_R(cpu, 5, &cpu.C) },                                   // A9
	func(cpu *Z80Cpu) { handler_res_R(cpu, 5, &cpu.D) },                                   // AA
	func(cpu *Z80Cpu) { handler_res_R(cpu, 5, &cpu.E) },                                   // AB
	func(cpu *Z80Cpu) { handler_res_R(cpu, 5, &cpu.H) },                                   // AC
	func(cpu *Z80Cpu) { handler_res_R(cpu, 5, &cpu.L) },                                   // AD
	func(cpu *Z80Cpu) { handler_res_MEM(cpu, 5, pack_regcouple(cpu.H, cpu.L)) },           // AE
	func(cpu *Z80Cpu) { handler_res_R(cpu, 5, &cpu.A) },                                   // AF
	func(cpu *Z80Cpu) { handler_res_R(cpu, 6, &cpu.B) },                                   // B0
	func(cpu *Z80Cpu) { handler_res_R(cpu, 6, &cpu.C) },                                   // B1
	func(cpu *Z80Cpu) { handler_res_R(cpu, 6, &cpu.D) },                                   // B2
	func(cpu *Z80Cpu) { handler_res_R(cpu, 6, &cpu.E) },                                   // B3
	func(cpu *Z80Cpu) { handler_res_R(cpu, 6, &cpu.H) },                                   // B4
	func(cpu *Z80Cpu) { handler_res_R(cpu, 6, &cpu.L) },                                   // B5
	func(cpu *Z80Cpu) { handler_res_MEM(cpu, 6, pack_regcouple(cpu.H, cpu.L)) },           // B6
	func(cpu *Z80Cpu) { handler_res_R(cpu, 6, &cpu.A) },                                   // B7
	func(cpu *Z80Cpu) { handler_res_R(cpu, 7, &cpu.B) },                                   // B8
	func(cpu *Z80Cpu) { handler_res_R(cpu, 7, &cpu.C) },                                   // B9
	func(cpu *Z80Cpu) { handler_res_R(cpu, 7, &cpu.D) },                                   // BA
	func(cpu *Z80Cpu) { handler_res_R(cpu, 7, &cpu.E) },                                   // BB
	func(cpu *Z80Cpu) { handler_res_R(cpu, 7, &cpu.H) },                                   // BC
	func(cpu *Z80Cpu) { handler_res_R(cpu, 7, &cpu.L) },                                   // BD
	func(cpu *Z80Cpu) { handler_res_MEM(cpu, 7, pack_regcouple(cpu.H, cpu.L)) },           // BE
	func(cpu *Z80Cpu) { handler_res_R(cpu, 7, &cpu.A) },                                   // BF
	func(cpu *Z80Cpu) { handler_set_R(cpu, 0, &cpu.B) },                                   // C0
	func(cpu *Z80Cpu) { handler_set_R(cpu, 0, &cpu.C) },                                   // C1
	func(cpu *Z80Cpu) { handler_set_R(cpu, 0, &cpu.D) },                                   // C2
	func(cpu *Z80Cpu) { handler_set_R(cpu, 0, &cpu.E) },                                   // C3
	func(cpu *Z80Cpu) { handler_set_R(cpu, 0, &cpu.H) },                                   // C4
	func(cpu *Z80Cpu) { handler_set_R(cpu, 0, &cpu.L) },                                   // C5
	func(cpu *Z80Cpu) { handler_set_MEM(cpu, 0, pack_regcouple(cpu.H, cpu.L)) },           // C6
	func(cpu *Z80Cpu) { handler_set_R(cpu, 0, &cpu.A) },                                   // C7
	func(cpu *Z80Cpu) { handler_set_R(cpu, 1, &cpu.B) },                                   // C8
	func(cpu *Z80Cpu) { handler_set_R(cpu, 1, &cpu.C) },                                   // C9
	func(cpu *Z80Cpu) { handler_set_R(cpu, 1, &cpu.D) },                                   // CA
	func(cpu *Z80Cpu) { handler_set_R(cpu, 1, &cpu.E) },                                   // CB
	func(cpu *Z80Cpu) { handler_set_R(cpu, 1, &cpu.H) },                                   // CC
	func(cpu *Z80Cpu) { handler_set_R(cpu, 1, &cpu.L) },                                   // CD
	func(cpu *Z80Cpu) { handler_set_MEM(cpu, 1, pack_regcouple(cpu.H, cpu.L)) },           // CE
	func(cpu *Z80Cpu) { handler_set_R(cpu, 1, &cpu.A) },                                   // CF
	func(cpu *Z80Cpu) { handler_set_R(cpu, 2, &cpu.B) },                                   // D0
	func(cpu *Z80Cpu) { handler_set_R(cpu, 2, &cpu.C) },                                   // D1
	func(cpu *Z80Cpu) { handler_set_R(cpu, 2, &cpu.D) },                                   // D2
	func(cpu *Z80Cpu) { handler_set_R(cpu, 2, &cpu.E) },                                   // D3
	func(cpu *Z80Cpu) { handler_set_R(cpu, 2, &cpu.H) },                                   // D4
	func(cpu *Z80Cpu) { handler_set_R(cpu, 2, &cpu.L) },                                   // D5
	func(cpu *Z80Cpu) { handler_set_MEM(cpu, 2, pack_regcouple(cpu.H, cpu.L)) },           // D6
	func(cpu *Z80Cpu) { handler_set_R(cpu, 2, &cpu.A) },                                   // D7
	func(cpu *Z80Cpu) { handler_set_R(cpu, 3, &cpu.B) },                                   // D8
	func(cpu *Z80Cpu) { handler_set_R(cpu, 3, &cpu.C) },                                   // D9
	func(cpu *Z80Cpu) { handler_set_R(cpu, 3, &cpu.D) },                                   // DA
	func(cpu *Z80Cpu) { handler_set_R(cpu, 3, &cpu.E) },                                   // DB
	func(cpu *Z80Cpu) { handler_set_R(cpu, 3, &cpu.H) },                                   // DC
	func(cpu *Z80Cpu) { handler_set_R(cpu, 3, &cpu.L) },                                   // DD
	func(cpu *Z80Cpu) { handler_set_MEM(cpu, 3, pack_regcouple(cpu.H, cpu.L)) },           // DE
	func(cpu *Z80Cpu) { handler_set_R(cpu, 3, &cpu.A) },                                   // DF
	func(cpu *Z80Cpu) { handler_set_R(cpu, 4, &cpu.B) },                                   // E0
	func(cpu *Z80Cpu) { handler_set_R(cpu, 4, &cpu.C) },                                   // E1
	func(cpu *Z80Cpu) { handler_set_R(cpu, 4, &cpu.D) },                                   // E2
	func(cpu *Z80Cpu) { handler_set_R(cpu, 4, &cpu.E) },                                   // E3
	func(cpu *Z80Cpu) { handler_set_R(cpu, 4, &cpu.H) },                                   // E4
	func(cpu *Z80Cpu) { handler_set_R(cpu, 4, &cpu.L) },                                   // E5
	func(cpu *Z80Cpu) { handler_set_MEM(cpu, 4, pack_regcouple(cpu.H, cpu.L)) },           // E6
	func(cpu *Z80Cpu) { handler_set_R(cpu, 4, &cpu.A) },                                   // E7
	func(cpu *Z80Cpu) { handler_set_R(cpu, 5, &cpu.B) },                                   // E8
	func(cpu *Z80Cpu) { handler_set_R(cpu, 5, &cpu.C) },                                   // E9
	func(cpu *Z80Cpu) { handler_set_R(cpu, 5, &cpu.D) },                                   // EA
	func(cpu *Z80Cpu) { handler_set_R(cpu, 5, &cpu.E) },                                   // EB
	func(cpu *Z80Cpu) { handler_set_R(cpu, 5, &cpu.H) },                                   // EC
	func(cpu *Z80Cpu) { handler_set_R(cpu, 5, &cpu.L) },                                   // ED
	func(cpu *Z80Cpu) { handler_set_MEM(cpu, 5, pack_regcouple(cpu.H, cpu.L)) },           // EE
	func(cpu *Z80Cpu) { handler_set_R(cpu, 5, &cpu.A) },                                   // EF
	func(cpu *Z80Cpu) { handler_set_R(cpu, 6, &cpu.B) },                                   // F0
	func(cpu *Z80Cpu) { handler_set_R(cpu, 6, &cpu.C) },                                   // F1
	func(cpu *Z80Cpu) { handler_set_R(cpu, 6, &cpu.D) },                                   // F2
	func(cpu *Z80Cpu) { handler_set_R(cpu, 6, &cpu.E) },                                   // F3
	func(cpu *Z80Cpu) { handler_set_R(cpu, 6, &cpu.H) },                                   // F4
	func(cpu *Z80Cpu) { handler_set_R(cpu, 6, &cpu.L) },                                   // F5
	func(cpu *Z80Cpu) { handler_set_MEM(cpu, 6, pack_regcouple(cpu.H, cpu.L)) },           // F6
	func(cpu *Z80Cpu) { handler_set_R(cpu, 6, &cpu.A) },                                   // F7
	func(cpu *Z80Cpu) { handler_set_R(cpu, 7, &cpu.B) },                                   // F8
	func(cpu *Z80Cpu) { handler_set_R(cpu, 7, &cpu.C) },                                   // F9
	func(cpu *Z80Cpu) { handler_set_R(cpu, 7, &cpu.D) },                                   // FA
	func(cpu *Z80Cpu) { handler_set_R(cpu, 7, &cpu.E) },                                   // FB
	func(cpu *Z80Cpu) { handler_set_R(cpu, 7, &cpu.H) },                                   // FC
	func(cpu *Z80Cpu) { handler_set_R(cpu, 7, &cpu.L) },                                   // FD
	func(cpu *Z80Cpu) { handler_set_MEM(cpu, 7, pack_regcouple(cpu.H, cpu.L)) },           // FE
	func(cpu *Z80Cpu) { handler_set_R(cpu, 7, &cpu.A) },                                   // FF
}

var ticks_opcode = []uint8{
	1, 3, 2, 2, 1, 1, 2, 1, 5, 2, 2, 2, 1, 1, 2, 1,
	1, 3, 2, 2, 1, 1, 2, 1, 3, 2, 2, 2, 1, 1, 2, 1,
	2, 3, 2, 2, 1, 1, 2, 1, 2, 2, 2, 2, 1, 1, 2, 1,
	2, 3, 2, 2, 3, 3, 3, 1, 2, 2, 2, 2, 1, 1, 2, 1,
	1, 1, 1, 1, 1, 1, 2, 1, 1, 1, 1, 1, 1, 1, 2, 1,
	1, 1, 1, 1, 1, 1, 2, 1, 1, 1, 1, 1, 1, 1, 2, 1,
	1, 1, 1, 1, 1, 1, 2, 1, 1, 1, 1, 1, 1, 1, 2, 1,
	2, 2, 2, 2, 2, 2, 1, 2, 1, 1, 1, 1, 1, 1, 2, 1,
	1, 1, 1, 1, 1, 1, 2, 1, 1, 1, 1, 1, 1, 1, 2, 1,
	1, 1, 1, 1, 1, 1, 2, 1, 1, 1, 1, 1, 1, 1, 2, 1,
	1, 1, 1, 1, 1, 1, 2, 1, 1, 1, 1, 1, 1, 1, 2, 1,
	1, 1, 1, 1, 1, 1, 2, 1, 1, 1, 1, 1, 1, 1, 2, 1,
	2, 3, 3, 4, 3, 4, 2, 4, 2, 4, 3, 0, 3, 6, 2, 4,
	2, 3, 3, 0, 3, 4, 2, 4, 2, 4, 3, 0, 3, 0, 2, 4,
	3, 3, 2, 0, 0, 4, 2, 4, 4, 1, 4, 0, 0, 0, 2, 4,
	3, 3, 2, 1, 0, 4, 2, 4, 3, 2, 4, 1, 0, 0, 2, 4,
}

var ticks_branched = []uint8{
	1, 3, 2, 2, 1, 1, 2, 1, 5, 2, 2, 2, 1, 1, 2, 1,
	1, 3, 2, 2, 1, 1, 2, 1, 3, 2, 2, 2, 1, 1, 2, 1,
	3, 3, 2, 2, 1, 1, 2, 1, 3, 2, 2, 2, 1, 1, 2, 1,
	3, 3, 2, 2, 3, 3, 3, 1, 3, 2, 2, 2, 1, 1, 2, 1,
	1, 1, 1, 1, 1, 1, 2, 1, 1, 1, 1, 1, 1, 1, 2, 1,
	1, 1, 1, 1, 1, 1, 2, 1, 1, 1, 1, 1, 1, 1, 2, 1,
	1, 1, 1, 1, 1, 1, 2, 1, 1, 1, 1, 1, 1, 1, 2, 1,
	2, 2, 2, 2, 2, 2, 1, 2, 1, 1, 1, 1, 1, 1, 2, 1,
	1, 1, 1, 1, 1, 1, 2, 1, 1, 1, 1, 1, 1, 1, 2, 1,
	1, 1, 1, 1, 1, 1, 2, 1, 1, 1, 1, 1, 1, 1, 2, 1,
	1, 1, 1, 1, 1, 1, 2, 1, 1, 1, 1, 1, 1, 1, 2, 1,
	1, 1, 1, 1, 1, 1, 2, 1, 1, 1, 1, 1, 1, 1, 2, 1,
	5, 3, 4, 4, 6, 4, 2, 4, 5, 4, 4, 0, 6, 6, 2, 4,
	5, 3, 4, 0, 6, 4, 2, 4, 5, 4, 4, 0, 6, 0, 2, 4,
	3, 3, 2, 0, 0, 4, 2, 4, 4, 1, 4, 0, 0, 0, 2, 4,
	3, 3, 2, 1, 0, 4, 2, 4, 3, 2, 4, 1, 0, 0, 2, 4,
}

var ticks_cb = []uint8{
	2, 2, 2, 2, 2, 2, 4, 2, 2, 2, 2, 2, 2, 2, 4, 2,
	2, 2, 2, 2, 2, 2, 4, 2, 2, 2, 2, 2, 2, 2, 4, 2,
	2, 2, 2, 2, 2, 2, 4, 2, 2, 2, 2, 2, 2, 2, 4, 2,
	2, 2, 2, 2, 2, 2, 4, 2, 2, 2, 2, 2, 2, 2, 4, 2,
	2, 2, 2, 2, 2, 2, 3, 2, 2, 2, 2, 2, 2, 2, 3, 2,
	2, 2, 2, 2, 2, 2, 3, 2, 2, 2, 2, 2, 2, 2, 3, 2,
	2, 2, 2, 2, 2, 2, 3, 2, 2, 2, 2, 2, 2, 2, 3, 2,
	2, 2, 2, 2, 2, 2, 3, 2, 2, 2, 2, 2, 2, 2, 3, 2,
	2, 2, 2, 2, 2, 2, 4, 2, 2, 2, 2, 2, 2, 2, 4, 2,
	2, 2, 2, 2, 2, 2, 4, 2, 2, 2, 2, 2, 2, 2, 4, 2,
	2, 2, 2, 2, 2, 2, 4, 2, 2, 2, 2, 2, 2, 2, 4, 2,
	2, 2, 2, 2, 2, 2, 4, 2, 2, 2, 2, 2, 2, 2, 4, 2,
	2, 2, 2, 2, 2, 2, 4, 2, 2, 2, 2, 2, 2, 2, 4, 2,
	2, 2, 2, 2, 2, 2, 4, 2, 2, 2, 2, 2, 2, 2, 4, 2,
	2, 2, 2, 2, 2, 2, 4, 2, 2, 2, 2, 2, 2, 2, 4, 2,
	2, 2, 2, 2, 2, 2, 4, 2, 2, 2, 2, 2, 2, 2, 4, 2,
}
