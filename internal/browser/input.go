package browser

import (
	"context"
	"errors"
	"fmt"
	"unicode/utf8"
)

// Input is one thing the user does while they control the browser.
type Input struct {
	// Type is "move", "down", "up", "click", "scroll", "key" or "text".
	Type string `json:"type"`
	// X and Y are page (CSS pixel) coordinates.
	X float64 `json:"x,omitempty"`
	Y float64 `json:"y,omitempty"`
	// Button is left (default), middle or right.
	Button string `json:"button,omitempty"`
	// DX and DY are the scroll distance in pixels.
	DX float64 `json:"dx,omitempty"`
	DY float64 `json:"dy,omitempty"`
	// Key and Code are a DOM KeyboardEvent's; Action is "down", "up" or
	// "press" (both, the default).
	Key    string `json:"key,omitempty"`
	Code   string `json:"code,omitempty"`
	Action string `json:"action,omitempty"`
	// Text is typed as it is, for "text".
	Text string `json:"text,omitempty"`
	// Modifiers is the CDP bit set: 1 Alt, 2 Ctrl, 4 Meta, 8 Shift.
	Modifiers int `json:"modifiers,omitempty"`
}

// MaxInputText caps one "text" input.
const MaxInputText = 4096

// keyCodes are the Windows virtual key codes of the keys that carry no text.
var keyCodes = map[string]int{
	"Backspace": 8, "Tab": 9, "Enter": 13, "Escape": 27, "PageUp": 33, "PageDown": 34,
	"End": 35, "Home": 36, "ArrowLeft": 37, "ArrowUp": 38, "ArrowRight": 39, "ArrowDown": 40,
	"Insert": 45, "Delete": 46, "F5": 116,
}

// commands holds the editing command Chrome runs for a shortcut. Headless
// Chrome runs no OS keybindings, so select-all and the like need to be asked.
var commands = map[string]string{
	"a": "selectAll", "c": "copy", "x": "cut", "v": "paste", "z": "undo", "y": "redo",
}

type cdpMouse struct {
	Type       string  `json:"type"`
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	Button     string  `json:"button,omitempty"`
	Buttons    int     `json:"buttons,omitempty"`
	ClickCount int     `json:"clickCount,omitempty"`
	DeltaX     float64 `json:"deltaX,omitempty"`
	DeltaY     float64 `json:"deltaY,omitempty"`
	Modifiers  int     `json:"modifiers,omitempty"`
}

type cdpKey struct {
	Type                  string   `json:"type"`
	Key                   string   `json:"key,omitempty"`
	Code                  string   `json:"code,omitempty"`
	Text                  string   `json:"text,omitempty"`
	WindowsVirtualKeyCode int      `json:"windowsVirtualKeyCode,omitempty"`
	Modifiers             int      `json:"modifiers,omitempty"`
	Commands              []string `json:"commands,omitempty"`
}

// ErrInput means an input event was not understood.
var ErrInput = errors.New("bad input")

func buttonBit(b string) (name string, bit int, err error) {
	switch b {
	case "", "left":
		return "left", 1, nil
	case "right":
		return "right", 2, nil
	case "middle":
		return "middle", 4, nil
	}
	return "", 0, fmt.Errorf("%w: button must be left, middle or right", ErrInput)
}

// dispatch sends the CDP calls for one input over c.
func (in Input) dispatch(ctx context.Context, c *Conn) error {
	call := func(method string, p any) error {
		_, err := c.Call(ctx, method, p)
		return err
	}
	switch in.Type {
	case "move":
		return call("Input.dispatchMouseEvent", cdpMouse{Type: "mouseMoved", X: in.X, Y: in.Y, Modifiers: in.Modifiers})
	case "down", "up", "click":
		name, bit, err := buttonBit(in.Button)
		if err != nil {
			return err
		}
		if in.Type != "up" {
			if err := call("Input.dispatchMouseEvent", cdpMouse{Type: "mousePressed", X: in.X, Y: in.Y, Button: name, Buttons: bit, ClickCount: 1, Modifiers: in.Modifiers}); err != nil {
				return err
			}
		}
		if in.Type != "down" {
			return call("Input.dispatchMouseEvent", cdpMouse{Type: "mouseReleased", X: in.X, Y: in.Y, Button: name, ClickCount: 1, Modifiers: in.Modifiers})
		}
		return nil
	case "scroll":
		return call("Input.dispatchMouseEvent", cdpMouse{Type: "mouseWheel", X: in.X, Y: in.Y, DeltaX: in.DX, DeltaY: in.DY, Modifiers: in.Modifiers})
	case "text":
		if in.Text == "" || len(in.Text) > MaxInputText || !utf8.ValidString(in.Text) {
			return fmt.Errorf("%w: text must be 1 to %d characters", ErrInput, MaxInputText)
		}
		return call("Input.insertText", map[string]string{"text": in.Text})
	case "key":
		return in.key(call)
	}
	return fmt.Errorf("%w: type must be move, down, up, click, scroll, key or text", ErrInput)
}

func (in Input) key(call func(string, any) error) error {
	if in.Key == "" {
		return fmt.Errorf("%w: a key event needs a key", ErrInput)
	}
	vk, special := keyCodes[in.Key]
	printable := !special && utf8.RuneCountInString(in.Key) == 1
	if !special && !printable {
		return fmt.Errorf("%w: unknown key %q", ErrInput, in.Key)
	}
	// Ctrl or Meta with a letter is a shortcut, not text.
	shortcut := in.Modifiers&(2|4) != 0
	down := cdpKey{Type: "rawKeyDown", Key: in.Key, Code: in.Code, WindowsVirtualKeyCode: vk, Modifiers: in.Modifiers}
	switch {
	case printable && !shortcut:
		down.Type, down.Text = "keyDown", in.Key
	case printable && shortcut:
		if c, ok := commands[in.Key]; ok {
			down.Commands = []string{c}
		}
	case in.Key == "Enter":
		down.Type, down.Text = "keyDown", "\r"
	}
	up := cdpKey{Type: "keyUp", Key: in.Key, Code: in.Code, WindowsVirtualKeyCode: vk, Modifiers: in.Modifiers}
	if in.Action != "up" {
		if err := call("Input.dispatchKeyEvent", down); err != nil {
			return err
		}
	}
	if in.Action != "down" {
		return call("Input.dispatchKeyEvent", up)
	}
	return nil
}
