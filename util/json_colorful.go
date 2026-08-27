package util

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// ShowJson prints data as JSON to stdout.
// Errors are ignored for backward compatibility with existing configure output.
func ShowJson(data interface{}, color bool) {
	_ = WriteJson(os.Stdout, data, color)
}

// WriteJson writes indented JSON and optionally adds ANSI token colors.
func WriteJson(w io.Writer, data interface{}, color bool) error {
	if w == nil {
		return fmt.Errorf("json writer is nil")
	}
	buf := bytes.NewBuffer(nil)
	encoder := json.NewEncoder(buf)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "    ")
	if err := encoder.Encode(data); err != nil {
		return fmt.Errorf("json encode: %w", err)
	}
	content := buf.Bytes()
	if color {
		content = ColorizeJSON(content)
	}
	n, err := w.Write(content)
	if n < len(content) && err == nil {
		return io.ErrShortWrite
	}
	return err
}

// ColorizeJSON adds ANSI colors to already-encoded JSON tokens.
func ColorizeJSON(content []byte) []byte {
	var colored bytes.Buffer
	for i := 0; i < len(content); {
		switch {
		case content[i] == '"':
			end := jsonStringEnd(content, i)
			color := "\033[1;32m"
			if nextJSONByte(content, end) == ':' {
				color = "\033[1;35m"
			}
			colored.WriteString(color)
			colored.Write(content[i:end])
			colored.WriteString("\033[0m")
			i = end
		case isJSONNumberStart(content[i]):
			end := jsonNumberEnd(content, i)
			colored.WriteString("\033[1;94m")
			colored.Write(content[i:end])
			colored.WriteString("\033[0m")
			i = end
		case bytes.HasPrefix(content[i:], []byte("true")):
			colored.WriteString("\033[1;91mtrue\033[0m")
			i += len("true")
		case bytes.HasPrefix(content[i:], []byte("false")):
			colored.WriteString("\033[1;91mfalse\033[0m")
			i += len("false")
		case bytes.HasPrefix(content[i:], []byte("null")):
			colored.WriteString("\033[1;33mnull\033[0m")
			i += len("null")
		default:
			colored.WriteByte(content[i])
			i++
		}
	}
	return colored.Bytes()
}

func jsonStringEnd(content []byte, start int) int {
	escaped := false
	for i := start + 1; i < len(content); i++ {
		if escaped {
			escaped = false
			continue
		}
		if content[i] == '\\' {
			escaped = true
			continue
		}
		if content[i] == '"' {
			return i + 1
		}
	}
	return len(content)
}

func nextJSONByte(content []byte, start int) byte {
	for i := start; i < len(content); i++ {
		switch content[i] {
		case ' ', '\t', '\r', '\n':
			continue
		default:
			return content[i]
		}
	}
	return 0
}

func isJSONNumberStart(b byte) bool {
	return b == '-' || b >= '0' && b <= '9'
}

func jsonNumberEnd(content []byte, start int) int {
	i := start
	for i < len(content) {
		b := content[i]
		if b == '-' || b == '+' || b == '.' || b == 'e' || b == 'E' || b >= '0' && b <= '9' {
			i++
			continue
		}
		break
	}
	return i
}

func printWithIndent(indent int, a ...interface{}) {
	for i := 0; i < 4*indent; i++ {
		fmt.Print(" ")
	}
	fmt.Print(a...)
}

func printlnWithIndent(indent int, a ...interface{}) {
	for i := 0; i < 4*indent; i++ {
		fmt.Print(" ")
	}
	fmt.Println(a...)
}

func printfWithIndent(indent int, format string, a ...interface{}) {
	for i := 0; i < 4*indent; i++ {
		fmt.Print(" ")
	}
	fmt.Printf(format, a...)
}
