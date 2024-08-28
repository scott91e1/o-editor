package main

import "github.com/xyproto/vt100"

// Backspace tries todelete characters to the left and move the cursor accordingly. Also supports block mode.
func (e *Editor) Backspace(c *vt100.Canvas, bookmark *Position) {
	doBackspace := func() bool {
		// Delete the character to the left
		if e.EmptyLine() {
			if e.blockMode {
				return false // break
			}
			e.DeleteCurrentLineMoveBookmark(bookmark)
			e.pos.Up()
			e.TrimRight(e.DataY())
			e.End(c)
		} else if e.AtStartOfTheLine() { // at the start of the screen line, the line may be scrolled
			if e.blockMode {
				return false // break
			}
			// remove the rest of the current line and move to the last letter of the line above
			// before deleting it
			if e.DataY() > 0 {
				e.pos.Up()
				e.TrimRight(e.DataY())
				e.End(c)
				e.Delete(c)
			}
		} else if (e.EmptyLine() || e.AtStartOfTheLine()) && e.indentation.Spaces && e.indentation.WSLen(e.LeadingWhitespace()) >= e.indentation.PerTab {
			// Delete several spaces
			for i := 0; i < e.indentation.PerTab; i++ {
				// Move back
				e.Prev(c)
				// Type a blank
				e.SetRune(' ')
				e.WriteRune(c)
				e.Delete(c)
			}
		} else {
			// Move back
			e.Prev(c)
			// Type a blank
			e.SetRune(' ')
			e.WriteRune(c)
			if !e.AtOrAfterEndOfLine() {
				// Delete the blank
				e.Delete(c)
				// scroll left instead of moving the cursor left, if possible
				e.pos.mut.Lock()
				if e.pos.offsetX > 0 {
					e.pos.offsetX--
					e.pos.sx++
				}
				e.pos.mut.Unlock()
			}
		}
		return true // success
	}
	if !e.blockMode {
		doBackspace()
		return
	}
	e.ForEachLineInBlock(c, doBackspace)
}