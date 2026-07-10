These are some ideas that I want to discuss for future work. Unlike the "TODO" files I would create in some other projects, IDEAS are not necessarily "TODO" items. They are ideas that I want to discuss with you (the AI) before arriving to a decision on any of them.

We can use tags to designate state, like [idea] [rejected] [applied] etc

I'll use [idea] for new ideas that have not been discussed yet. Feel free to apply any tag you want that would make sense, not just the three examples I gave above.


--------------------------------------------------------------------------------

[idea] Function to store a PNG file at any time.

I am not sure if my reading is correct, but it kinda feels like in order to save
a png file of a shirei app, it must run in "headless mode" and bypass the usual
OS window.

As far as I can tell, we should be able to take a "screenshot" of the application
at any point in time. For example, just run the image software renderer on the
list of surfaces from the last frame and save to a file.

This means an automated test can run even in windowed mode, and not only that, but
synthesize events to make the program do things while the user is watching, and
take several screenshots, etc.

[applied] Brighter colors theme

Current "style" for inputbox and buttons is trying too hard to be too skeumorphic.
I think this came as an overreaction to flat UI design where I can't tell whether
something is a clickable button or an input box or just a non-interactive label.

However, after trying to design several good looking UIs, I've come to the
conclusion that the flat UI people have a point: too many 3d buttons make the
screen look cluttered. So as a compromise I designed 'ctrl' style button: almost
flat, but still pressible.

Now I am thinking about it more, and I think in general, if we have a good way to
distinguish buttons from input boxes on an instictive level, we don't need to be
too conerned with the 3D-ness of it.

a subtle outwards vs inward bevel might be all that is needed. Maybe along with
some other queues, such as buttons having a small 4px radius led-like light, or
input boxex having a strong underline along with the subtle inset bevel effect.

So I want to consider redesigning the look and feel of these two widget types.

In general, I am thinking XP like bright colors might be nice to have: light
aqua, light yellow, light green.

Applied: added a Vec4-based Accent system (widgets/theme.go) with presets
(blue/steel/sunshine/meadow), and redesigned Button (solid accent fill,
white text, top highlight, colored elevation lip instead of a hard shadow),
TextInput (thin border + accent underline on focus), CheckBox/OptionButton
(accent-filled + white glyph when set, accent-outlined ring when not),
SegmentedControl (colored borders/dividers, edge-to-edge accent fill, bold
white label), a new ToggleSwitch, ScrollBars (white track, accent thumb),
and the menu hover highlight. All wired through demo_theme as a living
reference. Remaining corners (DirectoryInput's suggestion dropdown, Table's
sort-column chip, LogView's copy button, core text SelectionColor) are
minor and will get picked up as they come up rather than as a batch.

[idea] Styling spans on text to support inline styles
→ spec written: notes/stylespans-plan.md (2026-07-07, Fable) — option
2 below (string + optional []StyleSpan on TextAttrs), with the added
two-tier rule: color/underline/background apply per-glyph at layout
(never split shaping — keeps Arabic joined and ligatures whole);
font/aspect/size split segments (and join the shape-cache key).
Ready to hand to an implementer.

Currently text rendering does not support inline styles. We can render various
document elements with different styles, but the style applies to the whole
element, e.g. a title with a big size in a certain font, a body in a certain font,
a block quote with a style, a code sample with a style.

But we cannot do a paragraph with a certain word in a different color or font,
for example.

The problem is not that hard. I just never bothered with it because it was lower
priority.

I had an experiment a few years back where I did exactly that: multiple styles
inside the same text. It was my first attempt at immediate mode text rendering.
The API I came up with was quite awful to be honest. It was a pain to use.

BUT, the important thing is *how* to specify inline spans.

-> Do we turn text from a string to a series of "Styled Text"?

-> Do we keep text as a string, keep the default style, but attach a list of
`StyleSpan` objects that apply a different style to different spans of text?

I lean towards the second. The style spans can remain optional.

Constructing the styling spans in code *might* be painful. It will not be very
practicaly to hardcode one. But it does allow styles to be *added* on top of text.

Text shapping will have to be updated, of course. Namely in how segments are split.
Right now, segments are decided by script, is_space, direction, etc.

We will need to incorporate the fontId into this. Why? Because shaping depends on
the font. The point of shaping is to find the glyph id from the font. If the font
changes mid-word, that should affect the shaping.

Color and size do not affect the shaping logic, so they would not factor in.

They will need to be applied however when it comes time to render the text, and
also when it comes to computing the sizes of various segments. We'll need to be
careful about vertical alignment when several segments in the same horizontal
run have different sizes. I think they need to be aligned at the "baseline",
assuming our text rendering has that concept.

[applied] Stabilize before rendering

When a container wants to know its size but can't find it, the rendering loop
should notice that, and then perform another iteration of the UI builder render
function before presenting the image to the screen, because the first iteration
was incomplete.

Similar to how we have RequestNextFrame, we can have something like RequestSize
and if the size of the current contaienr is not yet known, that means we need to
run it again, so a flag is set somewhere in order to trigger the "render again"
process.

However, we should set a max hard limit on how many times this can repeat. N=2
would probably be a good default.

Applied: no explicit RequestSize needed — the public geometry accessors
(GetResolvedSize, GetScreenRect, GetResolvedRectOf, ...) already know when
they miss, and now flag the frame as incomplete. RunFrameFn loops the frame
build (capped at 2 passes, per the above) before harvesting output, so
backends never present a frame built from unanswered geometry queries; all
backends got this for free. Longer dependency chains still converge across
presented frames via FrameHasChanges. One subtlety surfaced: nodes born
during the call must skip animation sourcing (they'd otherwise animate from
the discarded incomplete pass at ~zero timeDelta, freezing at the wrong
rect) — handled with a bornFrame stamp on identity nodes. Pinned by
settle_test.go; headless snapshots were unaffected since RenderToImage
already settled via its outer loop.

[discussed] exploring mobile device support

What would it take to support building for iOS? for Android? How hard would it
be to create a backend for them that routes events to shirei core?
If we add touch event support, will this complicate the regular desktop oriented
input? e.g. mouse position, scrolling, etc.

Discussed 2026-07-07, distilled to notes/mobile-feasibility.md (deliberation,
not a plan). Short version: both platforms feasible from macOS; the software
renderer makes backends thin shells (CALayer blit / ANativeWindow_lock);
touch stays out of core — a backend gesture synthesizer maps taps/drags to
existing pointer+Scroll events so mouse-only programs work unmodified;
mobile IME is the long pole and phases in last. Cheapest next step if/when
we want one: a desktop --touch simulator to validate the synthesizer design.

--------------------------------------------------------------------------------
