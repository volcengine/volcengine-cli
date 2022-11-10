package util

import "fmt"

const (
	_BLACK   = "\033[30M"
	_RED     = "\033[31m"
	_GREEN   = "\033[32m"
	_YELLOW  = "\033[33m"
	_BLUE    = "\033[34m"
	_MAGENTA = "\033[35m"
	_CYAN    = "\033[36m"
	_WHITE   = "\033[37m"
	_DEFAULT = "\033[0m"
)

type colorPrinter struct {
	currentColor string
}

var cp colorPrinter

func setColor() {
	fmt.Print(cp.currentColor)
}

func resetColor() {
	fmt.Print(_DEFAULT)
}

func Black() *colorPrinter {
	cp.currentColor = _BLACK
	return &cp
}

func (cp *colorPrinter) Black() *colorPrinter {
	cp.currentColor = _BLACK
	return cp
}

func Red() *colorPrinter {
	cp.currentColor = _RED
	return &cp
}

func (cp *colorPrinter) Red() *colorPrinter {
	cp.currentColor = _RED
	return cp
}

func Green() *colorPrinter {
	cp.currentColor = _GREEN
	return &cp
}

func (cp *colorPrinter) Green() *colorPrinter {
	cp.currentColor = _GREEN
	return cp
}

func Yellow() *colorPrinter {
	cp.currentColor = _YELLOW
	return &cp
}

func (cp *colorPrinter) Yellow() *colorPrinter {
	cp.currentColor = _YELLOW
	return cp
}

func Blue() *colorPrinter {
	cp.currentColor = _BLUE
	return &cp
}

func (cp *colorPrinter) Blue() *colorPrinter {
	cp.currentColor = _BLUE
	return cp
}

func Magenta() *colorPrinter {
	cp.currentColor = _MAGENTA
	return &cp
}

func (cp *colorPrinter) Magenta() *colorPrinter {
	cp.currentColor = _MAGENTA
	return cp
}

func Cyan() *colorPrinter {
	cp.currentColor = _CYAN
	return &cp
}

func (cp *colorPrinter) Cyan() *colorPrinter {
	cp.currentColor = _CYAN
	return cp
}

func White() *colorPrinter {
	cp.currentColor = _WHITE
	return &cp
}

func (cp *colorPrinter) White() *colorPrinter {
	cp.currentColor = _WHITE
	return cp
}

func (cp *colorPrinter) Println(a ...interface{}) *colorPrinter {
	setColor()
	defer resetColor()
	fmt.Println(a...)
	return cp
}

func (cp *colorPrinter) Printf(format string, a ...interface{}) *colorPrinter {
	setColor()
	defer resetColor()
	fmt.Printf(format, a...)
	return cp
}

func (cp *colorPrinter) Print(a ...interface{}) *colorPrinter {
	fmt.Print(a...)
	return cp
}

func Println(a ...interface{}) *colorPrinter {
	fmt.Println(a...)
	return &cp
}

func Printf(format string, a ...interface{}) *colorPrinter {
	fmt.Printf(format, a...)
	return &cp
}

func Print(a ...interface{}) *colorPrinter {
	fmt.Print(a...)
	return &cp
}
