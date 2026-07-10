package xkbcommon

// macOS Virtual Key Codes
// Reference: https://developer.apple.com/documentation/appkit/nsevent/specialkey
// and HIToolbox/Events.h

// Navigation keys
const KeyLeft = 0x7B  // kVK_LeftArrow
const KeyRight = 0x7C // kVK_RightArrow
const KeyUp = 0x7E    // kVK_UpArrow
const KeyDown = 0x7D  // kVK_DownArrow
const KeyHome = 0x73  // kVK_Home
const KeyEnd = 0x77   // kVK_End

// Special keys
const KeyKpEnter = 0x4C   // kVK_ANSI_KeypadEnter
const KeyReturn = 0x24    // kVK_Return
const KeyBackspace = 0x33 // kVK_Delete (backspace)
const KeyDelete = 0x75    // kVK_ForwardDelete

// Control keys
const KeyControl = 0x3B  // kVK_Control
const KeyControlL = 0x3B // kVK_Control (left)
const KeyControlR = 0x3E // kVK_RightControl

// Letter Q (both uppercase and lowercase use same virtual key code)
const KeyQ = 0x0C // kVK_ANSI_Q
const KEYq = 0x0C // kVK_ANSI_Q (same as uppercase)

// Additional common keys for reference
const KeyA = 0x00 // kVK_ANSI_A
const KeyS = 0x01 // kVK_ANSI_S
const KeyD = 0x02 // kVK_ANSI_D
const KeyF = 0x03 // kVK_ANSI_F
const KeyH = 0x04 // kVK_ANSI_H
const KeyG = 0x05 // kVK_ANSI_G
const KeyZ = 0x06 // kVK_ANSI_Z
const KeyX = 0x07 // kVK_ANSI_X
const KeyC = 0x08 // kVK_ANSI_C
const KeyV = 0x09 // kVK_ANSI_V
const KeyB = 0x0B // kVK_ANSI_B
const KeyW = 0x0D // kVK_ANSI_W
const KeyE = 0x0E // kVK_ANSI_E
const KeyR = 0x0F // kVK_ANSI_R
const KeyY = 0x10 // kVK_ANSI_Y
const KeyT = 0x11 // kVK_ANSI_T
const KeyO = 0x1F // kVK_ANSI_O
const KeyU = 0x20 // kVK_ANSI_U
const KeyI = 0x22 // kVK_ANSI_I
const KeyP = 0x23 // kVK_ANSI_P
const KeyL = 0x25 // kVK_ANSI_L
const KeyJ = 0x26 // kVK_ANSI_J
const KeyK = 0x28 // kVK_ANSI_K
const KeyN = 0x2D // kVK_ANSI_N
const KeyM = 0x2E // kVK_ANSI_M

// Lowercase versions (same as uppercase on macOS)
const KEYa = 0x00
const KEYs = 0x01
const KEYd = 0x02
const KEYf = 0x03
const KEYh = 0x04
const KEYg = 0x05
const KEYz = 0x06
const KEYx = 0x07
const KEYc = 0x08
const KEYv = 0x09
const KEYb = 0x0B
const KEYw = 0x0D
const KEYe = 0x0E
const KEYr = 0x0F
const KEYy = 0x10
const KEYt = 0x11
const KEYo = 0x1F
const KEYu = 0x20
const KEYi = 0x22
const KEYp = 0x23
const KEYl = 0x25
const KEYj = 0x26
const KEYk = 0x28
const KEYn = 0x2D
const KEYm = 0x2E

// Number keys
const Key1 = 0x12 // kVK_ANSI_1
const Key2 = 0x13 // kVK_ANSI_2
const Key3 = 0x14 // kVK_ANSI_3
const Key4 = 0x15 // kVK_ANSI_4
const Key6 = 0x16 // kVK_ANSI_6
const Key5 = 0x17 // kVK_ANSI_5
const Key9 = 0x19 // kVK_ANSI_9
const Key7 = 0x1A // kVK_ANSI_7
const Key8 = 0x1C // kVK_ANSI_8
const Key0 = 0x1D // kVK_ANSI_0

// Function keys
const KeyF1 = 0x7A  // kVK_F1
const KeyF2 = 0x78  // kVK_F2
const KeyF3 = 0x63  // kVK_F3
const KeyF4 = 0x76  // kVK_F4
const KeyF5 = 0x60  // kVK_F5
const KeyF6 = 0x61  // kVK_F6
const KeyF7 = 0x62  // kVK_F7
const KeyF8 = 0x64  // kVK_F8
const KeyF9 = 0x65  // kVK_F9
const KeyF10 = 0x6D // kVK_F10
const KeyF11 = 0x67 // kVK_F11
const KeyF12 = 0x6F // kVK_F12

// Modifier keys
const KeyShift = 0x38    // kVK_Shift
const KeyShiftL = 0x38   // kVK_Shift (left)
const KeyShiftR = 0x3C   // kVK_RightShift
const KeyAlt = 0x3A      // kVK_Option
const KeyAltL = 0x3A     // kVK_Option (left)
const KeyAltR = 0x3D     // kVK_RightOption
const KeyCommand = 0x37  // kVK_Command
const KeyCommandL = 0x37 // kVK_Command (left)
const KeyCommandR = 0x36 // kVK_RightCommand
const KeyCapsLock = 0x39 // kVK_CapsLock

// Other common keys
const KeySpace = 0x31        // kVK_Space
const KeyTab = 0x30          // kVK_Tab
const KeyEscape = 0x35       // kVK_Escape
const KeyEqual = 0x18        // kVK_ANSI_Equal
const KeyMinus = 0x1B        // kVK_ANSI_Minus
const KeyLeftBracket = 0x21  // kVK_ANSI_LeftBracket
const KeyRightBracket = 0x1E // kVK_ANSI_RightBracket
const KeyQuote = 0x27        // kVK_ANSI_Quote
const KeySemicolon = 0x29    // kVK_ANSI_Semicolon
const KeyBackslash = 0x2A    // kVK_ANSI_Backslash
const KeyComma = 0x2B        // kVK_ANSI_Comma
const KeySlash = 0x2C        // kVK_ANSI_Slash
const KeyPeriod = 0x2F       // kVK_ANSI_Period
const KeyGrave = 0x32        // kVK_ANSI_Grave

// Keypad keys
const KeyKp0 = 0x52        // kVK_ANSI_Keypad0
const KeyKp1 = 0x53        // kVK_ANSI_Keypad1
const KeyKp2 = 0x54        // kVK_ANSI_Keypad2
const KeyKp3 = 0x55        // kVK_ANSI_Keypad3
const KeyKp4 = 0x56        // kVK_ANSI_Keypad4
const KeyKp5 = 0x57        // kVK_ANSI_Keypad5
const KeyKp6 = 0x58        // kVK_ANSI_Keypad6
const KeyKp7 = 0x59        // kVK_ANSI_Keypad7
const KeyKp8 = 0x5B        // kVK_ANSI_Keypad8
const KeyKp9 = 0x5C        // kVK_ANSI_Keypad9
const KeyKpDecimal = 0x41  // kVK_ANSI_KeypadDecimal
const KeyKpMultiply = 0x43 // kVK_ANSI_KeypadMultiply
const KeyKpPlus = 0x45     // kVK_ANSI_KeypadPlus
const KeyKpClear = 0x47    // kVK_ANSI_KeypadClear
const KeyKpDivide = 0x4B   // kVK_ANSI_KeypadDivide
const KeyKpMinus = 0x4E    // kVK_ANSI_KeypadMinus
const KeyKpEquals = 0x51   // kVK_ANSI_KeypadEquals

// Page navigation
const KeyPageUp = 0x74   // kVK_PageUp
const KeyPageDown = 0x79 // kVK_PageDown
