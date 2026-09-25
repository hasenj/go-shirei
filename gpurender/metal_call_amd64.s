//go:build darwin && !ios

#include "textflag.h"

// func objcCall(id, sel, a0, a1, a2, a3, a4 uintptr) uintptr
//
// objc_msgSend's cache-miss slow path (_objc_msgSend_uncached) spills XMM
// registers with 16-byte-aligned MOVDQA relative to a frame it builds off
// the incoming RSP, per the System V AMD64 ABI's "RSP % 16 == 0 at CALL"
// requirement. Go's own stack layout does not maintain that invariant, so
// this trampoline must realign RSP itself before calling into ObjC, or a
// cache-miss send (e.g. the first send of any given class+selector pair)
// faults with SIGSEGV/EXC_I386_GPFLT inside the MOVDQA.
TEXT ·objcCall(SB), NOSPLIT, $24-64
	MOVQ R14, 16(SP) // save g
	MOVQ id+0(FP), DI
	MOVQ sel+8(FP), SI
	MOVQ a0+16(FP), DX
	MOVQ a1+24(FP), CX
	MOVQ a2+32(FP), R8
	MOVQ a3+40(FP), R9
	MOVQ a4+48(FP), AX
	MOVQ SP, R15   // save our frame pointer (R15 is callee-saved in SysV ABI)
	ANDQ $-16, SP  // align RSP to 16 bytes for the C/ObjC call
	SUBQ $16, SP   // reserve an aligned slot for the 7th (stack) argument
	MOVQ AX, 0(SP)
	MOVQ ·objcMsgSend(SB), AX
	CALL AX
	MOVQ R15, SP   // restore our frame pointer
	MOVQ 16(SP), R14
	MOVQ AX, ret+56(FP)
	RET
