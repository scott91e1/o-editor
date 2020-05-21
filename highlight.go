package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode"

	"github.com/xyproto/syntax"
	"github.com/xyproto/textoutput"
	"github.com/xyproto/vt100"
)

const (
	controlRuneReplacement = '¿' // for displaying control sequence characters
)

var (
	// Color scheme for the "text edit" mode
	defaultEditorForeground       = vt100.LightGreen // for when syntax highlighting is not in use
	defaultEditorBackground       = vt100.BackgroundDefault
	defaultStatusForeground       = vt100.White
	defaultStatusBackground       = vt100.BackgroundBlack
	defaultStatusErrorForeground  = vt100.LightRed
	defaultStatusErrorBackground  = vt100.BackgroundDefault
	defaultEditorSearchHighlight  = vt100.LightMagenta
	defaultEditorMultilineComment = vt100.Gray
	defaultEditorMultilineString  = vt100.Magenta
	defaultEditorHighlightTheme   = syntax.TextConfig{
		String:        "lightyellow",
		Keyword:       "lightred",
		Comment:       "gray",
		Type:          "lightblue",
		Literal:       "lightgreen",
		Punctuation:   "lightblue",
		Plaintext:     "lightgreen",
		Tag:           "lightgreen",
		TextTag:       "lightgreen",
		TextAttrName:  "lightgreen",
		TextAttrValue: "lightgreen",
		Decimal:       "white",
		AndOr:         "lightyellow",
		Star:          "lightyellow",
		Class:         "lightred",
		Private:       "darkred",
		Protected:     "darkyellow",
		Public:        "darkgreen",
		Whitespace:    "",
	}
)

// WriteLines will draw editor lines from "fromline" to and up to "toline" to the canvas, at cx, cy
func (e *Editor) WriteLines(c *vt100.Canvas, fromline, toline, cx, cy int) error {
	o := textoutput.NewTextOutput(true, true)
	tabString := " "
	if !e.DrawMode() {
		tabString = strings.Repeat(" ", e.spacesPerTab)
	}
	w := int(c.Width())
	if fromline >= toline {
		return errors.New("fromline >= toline in WriteLines")
	}
	numlines := toline - fromline
	offset := fromline
	inCodeBlock := false // used when highlighting Markdown
	// If in Markdown mode, figure out the current state of block quotes
	if e.mode == modeMarkdown {
		// Figure out if "fromline" is within a markdown code block or not
		for i := 0; i < fromline; i++ {
			// Check if the untrimmed line starts with ~~~ or ```
			contents := e.Line(LineIndex(i))
			if strings.HasPrefix(contents, "~~~") || strings.HasPrefix(contents, "```") {
				// Toggle the flag for if we're in a code block or not
				inCodeBlock = !inCodeBlock
			}
		}
	}
	var (
		noColor                 bool = os.Getenv("NO_COLOR") != ""
		trimmedLine             string
		singleLineCommentMarker      = e.SingleLineCommentMarker()
		q                            = NewQuoteState(singleLineCommentMarker)
		prevRune, prevPrevRune  rune = '\n', '\n'
		ignoreSingleQuotes      bool = e.mode == modeLisp
	)
	// First loop from 0 to offset to figure out if we are already in a multiLine comment or a multiLine string at the current line
	for i := 0; i < offset; i++ {
		trimmedLine = strings.TrimSpace(e.Line(LineIndex(i)))

		// Special case for ViM
		if e.mode == modeVim && strings.HasPrefix(trimmedLine, "\"") {
			q.singleLineComment = true
			q.startedMultiLineString = false
			q.backtick = 0
			q.doubleQuote = 0
			q.singleQuote = 0
			continue
		}

		// Have a trimmed line. Want to know: the current state of which quotes, comments or strings we are in.
		// Solution, have a state struct!
		prevRune, prevPrevRune = q.Process(trimmedLine, prevRune, prevPrevRune, ignoreSingleQuotes)
	}
	// q should now contain the current quote state
	var (
		counter               int
		line                  string
		screenLine            string
		y                     int
		assemblyStyleComments = (e.mode == modeAssembly) || (e.mode == modeLisp)
		prevLineIsListItem    bool
	)
	// Then loop from 0 to numlines (used as y+offset in the loop) to draw the text
	for y = 0; y < numlines; y++ {
		counter = 0
		line = e.Line(LineIndex(y + offset))
		if strings.Contains(line, "\t") {
			line = strings.Replace(line, "\t", tabString, -1)
		}
		screenLine = strings.TrimRightFunc(line, unicode.IsSpace)
		if len([]rune(screenLine)) >= w {
			screenLine = screenLine[:w]
		}
		if e.syntaxHighlight && !noColor {
			// Output a syntax highlighted line. Escape any tags in the input line.
			// textWithTags must be unescaped if there is not an error.
			if textWithTags, err := syntax.AsText([]byte(Escape(line)), assemblyStyleComments); err != nil {
				// Only output the line up to the width of the canvas
				fmt.Println(screenLine)
				counter += len([]rune(screenLine))
			} else {
				var (
					// Color and unescape
					coloredString string
					addedPar      = 0 // Added parenthesis to the parenthesis count when processing this line
					runDefault    = false
				)
				switch e.mode {
				case modeGit:
					coloredString = e.gitHighlight(line)
				case modeMarkdown:
					if highlighted, ok, codeBlockFound := markdownHighlight(line, inCodeBlock, prevLineIsListItem); ok {
						coloredString = highlighted
						if codeBlockFound {
							inCodeBlock = !inCodeBlock
						}
					} else {
						// Syntax highlight the line if it's not picked up by the markdownHighlight function
						coloredString = UnEscape(o.DarkTags(string(textWithTags)))
					}
					// If this is a list item, store true in "prevLineIsListItem"
					prevLineIsListItem = isListItem(line)
				case modeConfig, modeShell, modeCMake:
					if strings.Contains(line, "/*") || strings.Contains(line, "*/") {
						// No highlight
						coloredString = line
					} else {
						// Regular highlight
						coloredString = UnEscape(o.DarkTags(string(textWithTags)))
					}
				case modeStandardML, modeOCaml:
					// Handle single line comments starting with (* and ending with *)
					trimmedLine = strings.TrimSpace(line)
					if strings.HasPrefix(trimmedLine, "(*") && strings.HasSuffix(trimmedLine, "*)") {
						coloredString = UnEscape(e.multiLineComment.Get(trimmedLine))
					} else {
						// Regular highlight
						coloredString = UnEscape(o.DarkTags(string(textWithTags)))
					}
				case modeLisp:
					q.singleQuote = 0

					// Special case for Lisp single-line comments
					trimmedLine = strings.TrimSpace(line)
					if strings.Count(trimmedLine, ";;") == 1 {
						// Color the line with the same color as for multiLine comments
						if strings.HasPrefix(trimmedLine, ";") {
							coloredString = UnEscape(e.multiLineComment.Get(line))
						} else if strings.Count(trimmedLine, ";;") == 1 {

							parts := strings.SplitN(line, ";;", 2)
							if newTextWithTags, err := syntax.AsText([]byte(Escape(parts[0])), false); err != nil {
								coloredString = UnEscape(o.DarkTags(string(textWithTags)))
							} else {
								coloredString = UnEscape(o.DarkTags(string(newTextWithTags)) + e.multiLineComment.Get(";;"+parts[1]))
							}

						} else if strings.Count(trimmedLine, ";") == 1 {

							parts := strings.SplitN(line, ";", 2)
							if newTextWithTags, err := syntax.AsText([]byte(Escape(parts[0])), false); err != nil {
								coloredString = UnEscape(o.DarkTags(string(textWithTags)))
							} else {
								coloredString = UnEscape(o.DarkTags(string(newTextWithTags)) + e.multiLineComment.Get(";"+parts[1]))
							}

						}
						break
					}
					runDefault = true

				case modeVim:

					// Special case for ViM single-line comments
					trimmedLine = strings.TrimSpace(line)
					if strings.Count(trimmedLine, "\"") == 1 {
						// Color the line with the same color as for multiLine comments
						if strings.HasPrefix(trimmedLine, "\"") {
							coloredString = UnEscape(e.multiLineComment.Get(line))
						} else {
							parts := strings.SplitN(line, "\"", 2)
							if newTextWithTags, err := syntax.AsText([]byte(Escape(parts[0])), false); err != nil {
								coloredString = UnEscape(o.DarkTags(string(textWithTags)))
							} else {
								coloredString = UnEscape(o.DarkTags(string(newTextWithTags)) + e.multiLineComment.Get("\""+parts[1]))
							}
						}
						break
					}
					runDefault = true

				default:
					runDefault = true
				}
				if runDefault {
					trimmedLine = strings.TrimSpace(line)
					previousParCount := q.parCount
					prevRune, prevPrevRune = q.Process(trimmedLine, prevRune, prevPrevRune, ignoreSingleQuotes)
					addedPar = q.parCount - previousParCount // can be negative

					//logf("%s -[ %d ]-->\n\t%s\n", trimmedLine, addedPar, q.String())

					switch {
					case q.singleLineComment:
						// A single line comment (the syntax module did the highlighting)
						coloredString = UnEscape(o.DarkTags(string(textWithTags)))
					case q.multiLineComment:
						// A multi-line comment
						coloredString = UnEscape(e.multiLineComment.Get(line))
					case !q.startedMultiLineString && q.backtick > 0:
						// A multi-line string
						coloredString = UnEscape(e.multiLineString.Get(line))
					default:
						// Regular code
						coloredString = UnEscape(o.DarkTags(string(textWithTags)))
					}
				}

				// Slice of runes and color attributes, while at the same time highlighting search terms
				charactersAndAttributes := o.Extract(coloredString)

				// If e.rainbowParenthesis is true and we're not in a comment or a string, enable rainbow parenthesis
				if e.rainbowParenthesis && q.None() {
					if addedPar != 0 {
						parCount := q.parCount + (addedPar - 1)
						rainbowParen(&parCount, &charactersAndAttributes, singleLineCommentMarker, ignoreSingleQuotes)
					} else {
						rainbowParen(&(q.parCount), &charactersAndAttributes, singleLineCommentMarker, ignoreSingleQuotes)
					}
				}

				searchTermRunes := []rune(e.searchTerm)
				matchForAnotherN := 0
				for characterIndex, ca := range charactersAndAttributes {
					letter := ca.R
					fg := ca.A
					if letter == ' ' {
						fg = e.fg
					}
					if matchForAnotherN > 0 {
						// Coloring an already found match
						fg = e.searchFg
						matchForAnotherN--
					} else if len(e.searchTerm) > 0 && letter == searchTermRunes[0] {
						// Potential search highlight match
						length := len([]rune(e.searchTerm))
						counter := 0
						match := true
						for i := characterIndex; i < (characterIndex + length); i++ {
							if i >= len(charactersAndAttributes) {
								match = false
								break
							}
							ca2 := charactersAndAttributes[i]
							if ca2.R != []rune(e.searchTerm)[counter] {
								// mismatch, not a hit
								match = false
								break
							}
							counter++
						}
						// match?
						if match {
							fg = e.searchFg
							matchForAnotherN = length - 1
						}
					}
					if letter == '\t' {
						c.Write(uint(cx+counter), uint(cy+y), fg, e.bg, tabString)
						if e.DrawMode() {
							counter++
						} else {
							counter += e.spacesPerTab
						}
					} else {
						if unicode.IsControl(letter) { // letter < ' ' && letter != '\t' && letter != '\n' {
							letter = controlRuneReplacement
						}
						c.WriteRune(uint(cx+counter), uint(cy+y), fg, e.bg, letter)
						counter++
					}
				}
			}
		} else {
			// Output a regular line
			c.Write(uint(cx+counter), uint(cy+y), e.fg, e.bg, screenLine)
			counter += len([]rune(screenLine))
		}
		//length := len([]rune(screenLine)) + strings.Count(screenLine, "\t")*(e.spacesPerTab-1)
		// Fill the rest of the line on the canvas with "blanks"
		for x := counter; x < w; x++ {
			c.WriteRune(uint(cx+x), uint(cy+y), e.fg, e.bg, ' ')
		}
	}
	return nil
}
