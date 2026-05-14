package gbc

import "encoding/gob"

type Timer struct {
	GBC            *Console
	DIV            uint16
	TIMA, TMA, TAC uint8

	overflowPending       bool
	divCounter, timaCounter int
}

func (t *Timer) Save(encoder *gob.Encoder) {
	panicIfErr(encoder.Encode(t.DIV))
	panicIfErr(encoder.Encode(t.TIMA))
	panicIfErr(encoder.Encode(t.TMA))
	panicIfErr(encoder.Encode(t.TAC))
	panicIfErr(encoder.Encode(t.overflowPending))
	panicIfErr(encoder.Encode(t.divCounter))
	panicIfErr(encoder.Encode(t.timaCounter))
}

func (t *Timer) Load(decoder *gob.Decoder) error {
	errs := []error{
		decoder.Decode(&t.DIV),
		decoder.Decode(&t.TIMA),
		decoder.Decode(&t.TMA),
		decoder.Decode(&t.TAC),
		decoder.Decode(&t.overflowPending),
		decoder.Decode(&t.divCounter),
		decoder.Decode(&t.timaCounter),
	}
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func MakeTimer(c *Console) *Timer {
	t := &Timer{
		GBC: c,
	}
	return t
}

func (t *Timer) timerBit() uint16 {
	switch t.TAC & 3 {
	case 0:
		return 1 << 9
	case 1:
		return 1 << 3
	case 2:
		return 1 << 5
	case 3:
		return 1 << 7
	default:
		return 1 << 9
	}
}

func (t *Timer) timerSignal() bool {
	return t.TAC&4 != 0 && t.DIV&t.timerBit() != 0
}

func (t *Timer) incTIMA() {
	if t.TIMA == 0xFF {
		t.TIMA = 0
		t.overflowPending = true
	} else {
		t.TIMA += 1
	}
}

func (t *Timer) writeTIMA(value uint8) {
	t.TIMA = value
	if t.overflowPending {
		t.overflowPending = false
	}
}

func (t *Timer) reset() {
	oldSignal := t.timerSignal()
	t.divCounter = 0
	t.timaCounter = 0
	t.DIV = 0
	if oldSignal && !t.timerSignal() {
		t.incTIMA()
	}
}

func (t *Timer) setTAC(value uint8) {
	oldSignal := t.timerSignal()
	t.TAC = value
	if oldSignal && !t.timerSignal() {
		t.incTIMA()
	}
}

func (t *Timer) Tick(ticks int) {
	for i := 0; i < ticks; i++ {
		if t.overflowPending {
			t.TIMA = t.TMA
			t.overflowPending = false
			t.GBC.CPU.SetInterrupt(InterruptTimer.Mask)
		}

		oldSignal := t.timerSignal()
		t.DIV += 4
		if oldSignal && !t.timerSignal() {
			t.incTIMA()
		}
	}
}
