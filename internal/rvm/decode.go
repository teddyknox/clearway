package rvm

// An op is one operation of the machine. A compressed instruction decodes to
// the op of the full instruction that it abbreviates.
const (
	opIllegal uint8 = iota // not an instruction of the machine

	opLui   // x[rd] = imm
	opAuipc // x[rd] = pc + imm
	opJal   // x[rd] = pc + size; pc += imm
	opJalr  // t = pc + size; pc = (x[rs1] + imm) &^ 1; x[rd] = t

	opBeq // if cond(x[rs1], x[rs2]) { pc += imm }
	opBne
	opBlt
	opBge
	opBltu
	opBgeu

	opLb // x[rd] = mem[x[rs1] + imm]
	opLh
	opLw
	opLbu
	opLhu

	opSb // mem[x[rs1] + imm] = x[rs2]
	opSh
	opSw

	opAddi // x[rd] = x[rs1] op imm
	opSlti
	opSltiu
	opXori
	opOri
	opAndi
	opSlli
	opSrli
	opSrai

	opAdd // x[rd] = x[rs1] op x[rs2]
	opSub
	opSll
	opSlt
	opSltu
	opXor
	opSrl
	opSra
	opOr
	opAnd

	opMul
	opMulh
	opMulhsu
	opMulhu
	opDiv
	opDivu
	opRem
	opRemu

	opFence // no operation
	opEcall
	opEbreak

	opLr // atomic operations on the word at x[rs1]
	opSc
	opAmoswap
	opAmoadd
	opAmoxor
	opAmoand
	opAmoor
	opAmomin
	opAmomax
	opAmominu
	opAmomaxu

	numOps
)

// An insn is one decoded instruction.
//
// The fields rd, rs1, and rs2 are less than 32. The field size is 2 for a
// compressed instruction and 4 for a full one. The field imm holds the
// immediate operand, sign-extended to 32 bits when the instruction defines it
// as signed. For a branch or a jump, imm is the offset from the address of the
// instruction.
type insn struct {
	raw  uint32 // the four bytes at pc from which decode made this value
	imm  uint32
	op   uint8
	rd   uint8
	rs1  uint8
	rs2  uint8
	size uint8
}

// sext returns the low n bits of v, sign-extended to 32 bits. The caller
// gives n in [1, 32].
func sext(v uint32, n uint) uint32 {
	s := 32 - n
	return uint32(int32(v<<s) >> s)
}

// decode decodes the instruction that starts with the low bits of raw.
//
// The caller gives the four bytes at pc as a little-endian word. If the two
// low bits of raw are not both 1, the instruction is the low 16 bits and
// decode ignores the high 16 bits. decode depends on raw alone, and it sets
// the field raw of the result to its argument. An encoding outside RV32IMAC
// gives opIllegal.
func decode(raw uint32) insn {
	var d insn
	if raw&3 == 3 {
		d = decode32(raw)
		d.size = 4
	} else {
		d = decode16(raw & 0xffff)
		d.size = 2
	}
	d.raw = raw
	return d
}

// decode32 decodes one full instruction. It does not set raw or size.
func decode32(w uint32) insn {
	rd := uint8(w >> 7 & 31)
	rs1 := uint8(w >> 15 & 31)
	rs2 := uint8(w >> 20 & 31)
	f3 := w >> 12 & 7
	f7 := w >> 25

	switch w & 0x7f {
	case 0x37: // LUI
		return insn{op: opLui, rd: rd, imm: w &^ 0xfff}

	case 0x17: // AUIPC
		return insn{op: opAuipc, rd: rd, imm: w &^ 0xfff}

	case 0x6f: // JAL
		imm := uint32(int32(w)>>31<<20) | (w>>12&0xff)<<12 | (w>>20&1)<<11 | (w>>21&0x3ff)<<1
		return insn{op: opJal, rd: rd, imm: imm}

	case 0x67: // JALR
		if f3 != 0 {
			break
		}
		return insn{op: opJalr, rd: rd, rs1: rs1, imm: uint32(int32(w) >> 20)}

	case 0x63: // BRANCH
		var op uint8
		switch f3 {
		case 0:
			op = opBeq
		case 1:
			op = opBne
		case 4:
			op = opBlt
		case 5:
			op = opBge
		case 6:
			op = opBltu
		case 7:
			op = opBgeu
		default:
			return insn{}
		}
		imm := uint32(int32(w)>>31<<12) | (w>>7&1)<<11 | (w>>25&0x3f)<<5 | (w>>8&0xf)<<1
		return insn{op: op, rs1: rs1, rs2: rs2, imm: imm}

	case 0x03: // LOAD
		var op uint8
		switch f3 {
		case 0:
			op = opLb
		case 1:
			op = opLh
		case 2:
			op = opLw
		case 4:
			op = opLbu
		case 5:
			op = opLhu
		default:
			return insn{}
		}
		return insn{op: op, rd: rd, rs1: rs1, imm: uint32(int32(w) >> 20)}

	case 0x23: // STORE
		var op uint8
		switch f3 {
		case 0:
			op = opSb
		case 1:
			op = opSh
		case 2:
			op = opSw
		default:
			return insn{}
		}
		imm := uint32(int32(w)>>25<<5) | (w >> 7 & 31)
		return insn{op: op, rs1: rs1, rs2: rs2, imm: imm}

	case 0x13: // OP-IMM
		imm := uint32(int32(w) >> 20)
		var op uint8
		switch f3 {
		case 0:
			op = opAddi
		case 2:
			op = opSlti
		case 3:
			op = opSltiu
		case 4:
			op = opXori
		case 6:
			op = opOri
		case 7:
			op = opAndi
		case 1:
			if f7 != 0 {
				return insn{}
			}
			op, imm = opSlli, uint32(rs2)
		case 5:
			switch f7 {
			case 0x00:
				op = opSrli
			case 0x20:
				op = opSrai
			default:
				return insn{}
			}
			imm = uint32(rs2)
		}
		return insn{op: op, rd: rd, rs1: rs1, imm: imm}

	case 0x33: // OP
		var op uint8
		switch f7 {
		case 0x00:
			op = [8]uint8{opAdd, opSll, opSlt, opSltu, opXor, opSrl, opOr, opAnd}[f3]
		case 0x20:
			switch f3 {
			case 0:
				op = opSub
			case 5:
				op = opSra
			default:
				return insn{}
			}
		case 0x01:
			op = [8]uint8{opMul, opMulh, opMulhsu, opMulhu, opDiv, opDivu, opRem, opRemu}[f3]
		default:
			return insn{}
		}
		return insn{op: op, rd: rd, rs1: rs1, rs2: rs2}

	case 0x0f: // MISC-MEM
		// FENCE has funct3 0. A base implementation ignores its other
		// fields. FENCE.I (funct3 1) is not in the instruction set.
		if f3 != 0 {
			break
		}
		return insn{op: opFence}

	case 0x73: // SYSTEM
		switch w {
		case 0x00000073:
			return insn{op: opEcall}
		case 0x00100073:
			return insn{op: opEbreak}
		}

	case 0x2f: // AMO
		if f3 != 2 {
			break
		}
		// Bits 26 and 25 (aq and rl) order the operation among harts.
		// One hart ignores them.
		var op uint8
		switch w >> 27 {
		case 0x02:
			if rs2 != 0 {
				return insn{}
			}
			op = opLr
		case 0x03:
			op = opSc
		case 0x01:
			op = opAmoswap
		case 0x00:
			op = opAmoadd
		case 0x04:
			op = opAmoxor
		case 0x0c:
			op = opAmoand
		case 0x08:
			op = opAmoor
		case 0x10:
			op = opAmomin
		case 0x14:
			op = opAmomax
		case 0x18:
			op = opAmominu
		case 0x1c:
			op = opAmomaxu
		default:
			return insn{}
		}
		return insn{op: op, rd: rd, rs1: rs1, rs2: rs2}
	}
	return insn{}
}

// decode16 decodes one compressed instruction, given in the low 16 bits of
// w. It does not set raw or size.
func decode16(w uint32) insn {
	const sp = 2
	rdc := uint8(w>>2&7) + 8  // the register in bits 4:2
	rsc := uint8(w>>7&7) + 8  // the register in bits 9:7
	rdf := uint8(w >> 7 & 31) // the register in bits 11:7
	rsf := uint8(w >> 2 & 31) // the register in bits 6:2
	imm6 := sext((w>>12&1)<<5|w>>2&31, 6)
	f3 := w >> 13

	switch w & 3 {
	case 0:
		switch f3 {
		case 0: // C.ADDI4SPN
			imm := (w>>7&15)<<6 | (w>>11&3)<<4 | (w>>5&1)<<3 | (w>>6&1)<<2
			if imm == 0 {
				break
			}
			return insn{op: opAddi, rd: rdc, rs1: sp, imm: imm}
		case 2: // C.LW
			imm := (w>>10&7)<<3 | (w>>6&1)<<2 | (w>>5&1)<<6
			return insn{op: opLw, rd: rdc, rs1: rsc, imm: imm}
		case 6: // C.SW
			imm := (w>>10&7)<<3 | (w>>6&1)<<2 | (w>>5&1)<<6
			return insn{op: opSw, rs1: rsc, rs2: rdc, imm: imm}
		}
		// The others are C.FLD, C.FLW, C.FSD, C.FSW, and a reserved code.

	case 1:
		switch f3 {
		case 0: // C.ADDI, C.NOP
			return insn{op: opAddi, rd: rdf, rs1: rdf, imm: imm6}
		case 1, 5: // C.JAL, C.J
			imm := sext((w>>12&1)<<11|(w>>11&1)<<4|(w>>9&3)<<8|(w>>8&1)<<10|
				(w>>7&1)<<6|(w>>6&1)<<7|(w>>3&7)<<1|(w>>2&1)<<5, 12)
			rd := uint8(1)
			if f3 == 5 {
				rd = 0
			}
			return insn{op: opJal, rd: rd, imm: imm}
		case 2: // C.LI
			return insn{op: opAddi, rd: rdf, imm: imm6}
		case 3:
			if imm6 == 0 {
				break
			}
			if rdf == sp { // C.ADDI16SP
				imm := sext((w>>12&1)<<9|(w>>6&1)<<4|(w>>5&1)<<6|(w>>3&3)<<7|(w>>2&1)<<5, 10)
				return insn{op: opAddi, rd: sp, rs1: sp, imm: imm}
			}
			return insn{op: opLui, rd: rdf, imm: imm6 << 12} // C.LUI
		case 4:
			switch w >> 10 & 3 {
			case 0, 1: // C.SRLI, C.SRAI
				// A shift amount above 31 is not an instruction of RV32C.
				if w>>12&1 != 0 {
					break
				}
				op := opSrli
				if w>>10&1 != 0 {
					op = opSrai
				}
				return insn{op: op, rd: rsc, rs1: rsc, imm: uint32(rsf)}
			case 2: // C.ANDI
				return insn{op: opAndi, rd: rsc, rs1: rsc, imm: imm6}
			case 3:
				// With bit 12 set, the codes are C.SUBW and C.ADDW of
				// RV64C and two reserved codes.
				if w>>12&1 != 0 {
					break
				}
				op := [4]uint8{opSub, opXor, opOr, opAnd}[w>>5&3]
				return insn{op: op, rd: rsc, rs1: rsc, rs2: rdc}
			}
		case 6, 7: // C.BEQZ, C.BNEZ
			imm := sext((w>>12&1)<<8|(w>>10&3)<<3|(w>>5&3)<<6|(w>>3&3)<<1|(w>>2&1)<<5, 9)
			op := opBeq
			if f3 == 7 {
				op = opBne
			}
			return insn{op: op, rs1: rsc, imm: imm}
		}

	case 2:
		switch f3 {
		case 0: // C.SLLI
			if w>>12&1 != 0 {
				break
			}
			return insn{op: opSlli, rd: rdf, rs1: rdf, imm: uint32(rsf)}
		case 2: // C.LWSP
			if rdf == 0 {
				break
			}
			imm := (w>>12&1)<<5 | (w>>4&7)<<2 | (w>>2&3)<<6
			return insn{op: opLw, rd: rdf, rs1: sp, imm: imm}
		case 4:
			if w>>12&1 == 0 {
				if rsf != 0 { // C.MV
					return insn{op: opAdd, rd: rdf, rs2: rsf}
				}
				if rdf == 0 {
					break
				}
				return insn{op: opJalr, rs1: rdf} // C.JR
			}
			if rsf != 0 { // C.ADD
				return insn{op: opAdd, rd: rdf, rs1: rdf, rs2: rsf}
			}
			if rdf == 0 {
				return insn{op: opEbreak} // C.EBREAK
			}
			return insn{op: opJalr, rd: 1, rs1: rdf} // C.JALR
		case 6: // C.SWSP
			imm := (w>>9&15)<<2 | (w>>7&3)<<6
			return insn{op: opSw, rs1: sp, rs2: rsf, imm: imm}
		}
		// The others are C.FLDSP, C.FLWSP, C.FSDSP, and C.FSWSP.
	}
	return insn{}
}
