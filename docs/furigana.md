# Furigana

Shirei displays Unicode interlinear annotations as furigana above the base text.
The same marked string works in labels, `Text`, text inputs, and text areas.
No style flag is required.

```go
Label(Ruby("日本", "にほん") + "語")
```

`Ruby(base, reading)` constructs a string with three Unicode markers:

```go
"\uFFF9日本\uFFFAにほん\uFFFB語"
```

`RubyAnchor` (U+FFF9) starts the base, `RubySeparator` (U+FFFA) starts the
reading, and `RubyTerminator` (U+FFFB) ends the group. Both base and reading
must be nonempty. Each group stays within one paragraph.

Readings use half the applicable font size and sit centered above interior bases.
At the start of a left-to-right line, the base sits flush with the margin;
a longer reading starts at the same edge and extends toward following text.
A reading can extend above adjacent unannotated kana, borrowing at most one
reading em on each side. Directly adjacent annotations can have touching
readings, so per-character annotations retain natural spacing within words.
Readings separated by unannotated text keep a minimum gap of half the larger
reading em, adding only the missing space before the later group. This applies
even when the intervening text has zero width:

```go
Label(Ruby("方", "ほう") + Ruby("角", "がく"))
Label(Ruby("山", "やま") + "\u200B" + Ruby("川", "かわ"))
```

The zero-width space is ordinary intervening text; it supplies no overhang
space and remains part of the source string for copying and editing.

Excess width that cannot overhang remains reserved around the base. Groups stay together
when text wraps; readings stay inside the line's measured bounds. Long groups
can exceed the available width. Several groups express individual character
readings; one group expresses a reading for a whole word.

Style spans use rune positions in the complete source string, including the
markers and reading. Font and color spans can address base and reading independently.
Changing font size, weight, or color does not change marker interpretation.

Text boxes treat a complete group as one caret step. Selecting, copying,
cutting, or deleting it includes its base, reading, and markers. Paste and undo
preserve the marked string. Default field height includes reading space;
custom field chrome needs to reserve that space too. Clicking an overhanging
reading follows the base text beneath the click. `ShapedText.RubyHeight()`
returns the maximum additional height above the base lines.

Overhang uses one forward placement scan with one shaped segment of lookahead.
It borrows from following kana only when that segment fits on the same line.
It can retain extra spacing near line breaks. Hiragana, katakana, small kana,
and prolonged sound marks are eligible; larger neighboring text, punctuation,
Latin text, kanji, and annotated neighbors do not provide overhang space.
Text containing right-to-left runs uses fully reserved reading widths.

Incomplete, empty, nested, multipart, or paragraph-spanning annotation blocks
remain literal text. Password fields mask their input. Vertical writing and
advanced ruby alignment are outside this horizontal display format.

Run `go run ./demos/furigana` for an editable playground, or
`go -C behavior_test run ./furigana --close` for the windowed behavior check.
