package cgolib

// int answer(void) { return 42; }
import "C"

func Answer() int { return int(C.answer()) }
