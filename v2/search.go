package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xyproto/clip"
	"github.com/xyproto/vt100"
)

var (
	searchHistoryFilename = filepath.Join(userCacheDir, "o", "search.txt")
	searchHistory         *[]string
	errNoSearchMatch      = errors.New("no search match")
)

// SetSearchTerm will set the current search term. This initializes a new search.
func (e *Editor) SetSearchTerm(c *vt100.Canvas, status *StatusBar, s string, spellCheckMode bool) bool {
	foundMatch := false
	// set the search term
	e.searchTerm = s
	// set the sticky search term (used by ctrl-n, cleared by Esc only)
	e.stickySearchTerm = s
	// set spellcheck mode
	e.spellCheckMode = spellCheckMode
	// Go to the first instance after the current line, if found
	e.lineBeforeSearch = e.DataY()
	for y := e.DataY(); y < LineIndex(e.Len()); y++ {
		if strings.Contains(e.Line(y), s) {
			// Found an instance, scroll there
			// GoTo returns true if the screen should be redrawn
			redraw, _ := e.GoTo(y, c, status)
			if redraw {
				foundMatch = true
				e.Center(c)
			}
			break
		}
	}
	// draw the lines to the canvas
	e.DrawLines(c, true, false)
	return foundMatch
}

// SetSearchTermWithTimeout will set the current search term. This initializes a new search.
func (e *Editor) SetSearchTermWithTimeout(c *vt100.Canvas, status *StatusBar, s string, spellCheckMode bool, timeout time.Duration) bool {
	// set the search term
	e.searchTerm = s
	// set the sticky search term (used by ctrl-n, cleared by Esc only)
	e.stickySearchTerm = s
	// set spellcheck mode
	e.spellCheckMode = spellCheckMode
	// Go to the first instance after the current line, if found
	e.lineBeforeSearch = e.DataY()

	// create a channel to signal when a match is found
	matchFound := make(chan bool)

	foundMatch := LineIndex(-1)

	// run the search in a separate goroutine
	go func() {
		for y := e.DataY(); y < LineIndex(e.Len()); y++ {
			if strings.Contains(e.Line(y), s) {
				matchFound <- true
				foundMatch = y
				return
			}
		}
		matchFound <- false
	}()

	// wait for either a match to be found or timeout
	select {
	case <-matchFound:
	case <-time.After(timeout):
	}

	if foundMatch != -1 {
		// Found an instance, scroll there
		// GoTo returns true if the screen should be redrawn
		redraw, _ := e.GoTo(foundMatch, c, status)
		if redraw {
			e.Center(c)
			// Draw the lines to the canvas
			e.DrawLines(c, true, false)
			e.redraw = true
		}
		return true
	}

	return false
}

// SearchTerm will return the current search term
func (e *Editor) SearchTerm() string {
	return e.searchTerm
}

// ClearSearch will clear the current search term
func (e *Editor) ClearSearch() {
	e.searchTerm = ""
}

// UseStickySearchTerm will use the sticky search term as the current search term,
// which is not cleared by Esc, but by ctrl-p.
func (e *Editor) UseStickySearchTerm() {
	if e.stickySearchTerm != "" {
		e.searchTerm = e.stickySearchTerm
	}
}

// ClearStickySearchTerm will clear the sticky search term, for when ctrl-n is pressed.
func (e *Editor) ClearStickySearchTerm() {
	e.stickySearchTerm = ""
}

// forwardSearch is a helper function for searching for a string from the given startIndex,
// up to the given stopIndex. -1
// -1 is returned if there are no matches.
// startIndex is expected to be smaller than stopIndex
// x, y is returned.
func (e *Editor) forwardSearch(startIndex, stopIndex LineIndex) (int, LineIndex) {
	var (
		s      = e.SearchTerm()
		foundX = -1
		foundY = LineIndex(-1)
	)
	if s == "" {
		// Return -1, -1 if no search term is set
		return foundX, foundY
	}
	currentIndex := e.DataY()
	// Search from the given startIndex up to the given stopIndex
	for y := startIndex; y < stopIndex; y++ {
		lineContents := e.Line(y)
		if y == currentIndex {
			x, err := e.DataX()
			if err != nil {
				continue
			}
			// Search from the next byte (not rune) position on this line
			// TODO: Move forward one rune instead of one byte
			x++
			if x >= len(lineContents) {
				continue
			}
			if strings.Contains(lineContents[x:], s) {
				foundX = x + strings.Index(lineContents[x:], s)
				foundY = y
				break
			}
		} else {
			if strings.Contains(lineContents, s) {
				foundX = strings.Index(lineContents, s)
				foundY = y
				break
			}
		}
	}
	return foundX, LineIndex(foundY)
}

// backwardSearch is a helper function for searching for a string from the given startIndex,
// backwards to the given stopIndex. -1, -1 is returned if there are no matches.
// startIndex is expected to be larger than stopIndex
func (e *Editor) backwardSearch(startIndex, stopIndex LineIndex) (int, LineIndex) {
	var (
		s      = e.SearchTerm()
		foundX = -1
		foundY = LineIndex(-1)
	)
	if len(s) == 0 {
		// Return -1, -1 if no search term is set
		return foundX, foundY
	}
	currentIndex := e.DataY()
	// Search from the given startIndex backwards up to the given stopIndex
	for y := startIndex; y >= stopIndex; y-- {
		lineContents := e.Line(y)
		if y == currentIndex {
			x, err := e.DataX()
			if err != nil {
				continue
			}
			// Search from the next byte (not rune) position on this line
			// TODO: Move forward one rune instead of one byte
			x++
			if x >= len(lineContents) {
				continue
			}
			if strings.Contains(lineContents[x:], s) {
				foundX = x + strings.Index(lineContents[x:], s)
				foundY = y
				break
			}
		} else {
			if strings.Contains(lineContents, s) {
				foundX = strings.Index(lineContents, s)
				foundY = y
				break
			}
		}
	}
	return foundX, LineIndex(foundY)
}

// GoToNextMatch will go to the next match, searching for "e.SearchTerm()".
// * The search wraps around if wrap is true.
// * The search is backawards if forward is false.
// * The search is case-sensitive.
// Returns an error if the search was successful but no match was found.
func (e *Editor) GoToNextMatch(c *vt100.Canvas, status *StatusBar, wrap, forward bool) error {
	var (
		foundX int
		foundY LineIndex
		s      = e.SearchTerm()
	)

	// Check if there's something to search for
	if s == "" {
		return nil
	}

	// Search forward or backward
	if forward {
		// Forward search from the current location
		startIndex := e.DataY()
		stopIndex := LineIndex(e.Len())
		foundX, foundY = e.forwardSearch(startIndex, stopIndex)
	} else {
		// Backward search form the current location
		startIndex := e.DataY()
		stopIndex := LineIndex(0)
		foundX, foundY = e.backwardSearch(startIndex, stopIndex)
	}

	if foundY == -1 && wrap {
		if forward {
			// Do a search from the top if a match was not found
			startIndex := LineIndex(0)
			stopIndex := LineIndex(e.Len())
			foundX, foundY = e.forwardSearch(startIndex, stopIndex)
		} else {
			// Do a search from the bottom if a match was not found
			startIndex := LineIndex(e.Len())
			stopIndex := LineIndex(0)
			foundX, foundY = e.backwardSearch(startIndex, stopIndex)
		}
	}

	// Check if a match was found
	if foundY == -1 {
		// Not found
		e.GoTo(e.lineBeforeSearch, c, status)
		return errNoSearchMatch
	}

	// Go to the found match
	e.redraw, _ = e.GoTo(foundY, c, status)
	if foundX != -1 {
		tabs := strings.Count(e.Line(foundY), "\t")
		e.pos.sx = foundX + (tabs * (e.indentation.PerTab - 1))
		e.HorizontalScrollIfNeeded(c)
	}

	// Center and prepare to redraw
	e.Center(c)
	e.redraw = true
	e.redrawCursor = e.redraw

	return nil
}

// SearchMode will enter the interactive "search mode" where the user can type in a string and then press return to search
func (e *Editor) SearchMode(c *vt100.Canvas, status *StatusBar, tty *vt100.TTY, clearSearch, searchForward bool, undo *Undo) {
	// Load the search history if needed. Ignore any errors.
	if searchHistory == nil {
		searchHistorySlice, _ := LoadSearchHistory(searchHistoryFilename)
		searchHistory = &searchHistorySlice
	}
	var (
		previousSearch     string
		key                string
		initialLocation    = e.DataY().LineNumber()
		searchHistoryIndex int
		replaceMode        bool
		timeout            = 500 * time.Millisecond
	)

	searchPrompt := "Search:"
	if !searchForward {
		searchPrompt = "Search backwards:"
	}

AGAIN:
	doneCollectingLetters := false
	pressedReturn := false
	pressedTab := false
	if clearSearch {
		// Clear the previous search
		e.SetSearchTerm(c, status, "", false) // no timeout
	}
	s := e.SearchTerm()
	status.ClearAll(c)
	if s == "" {
		status.SetMessage(searchPrompt)
	} else {
		status.SetMessage(searchPrompt + " " + s)
	}
	status.ShowNoTimeout(c, e)
	for !doneCollectingLetters {
		if e.macro == nil || (e.playBackMacroCount == 0 && !e.macro.Recording) {
			// Read the next key in the regular way
			key = tty.String()
		} else {
			if e.macro.Recording {
				// Read and record the next key
				key = tty.String()
				if key != "c:20" { // ctrl-t
					// But never record the macro toggle button
					e.macro.Add(key)
				}
			} else if e.playBackMacroCount > 0 {
				key = e.macro.Next()
				if key == "" || key == "c:20" { // ctrl-t
					e.macro.Home()
					e.playBackMacroCount--
					// No more macro keys. Read the next key.
					key = tty.String()
				}
			}
		}
		switch key {
		case "c:8", "c:127": // ctrl-h or backspace
			if len(s) > 0 {
				s = s[:len(s)-1]
				if previousSearch == "" {
					e.SetSearchTermWithTimeout(c, status, s, false, timeout)
				}
				e.GoToLineNumber(initialLocation, c, status, false)
				status.SetMessage(searchPrompt + " " + s)
				status.ShowNoTimeout(c, e)
			}
		case "c:3", "c:6", "c:17", "c:27", "c:24": // ctrl-c, ctrl-f, ctrl-q, esc or ctrl-x
			s = ""
			if previousSearch == "" {
				e.SetSearchTermWithTimeout(c, status, s, false, timeout)
			}
			doneCollectingLetters = true
		case "c:9": // tab
			// collect letters again, this time for the replace term
			pressedTab = true
			doneCollectingLetters = true
		case "c:13": // return
			pressedReturn = true
			doneCollectingLetters = true
		case "c:22": // ctrl-v, paste the last line in the clipboard
			// Read the clipboard
			clipboardString, err := clip.ReadAll(false) // non-primary clipboard
			if err == nil && strings.TrimSpace(s) == "" {
				clipboardString, err = clip.ReadAll(true) // try the primary clipboard
			}
			if err == nil { // success
				if strings.Contains(clipboardString, "\n") {
					lines := strings.Split(clipboardString, "\n")
					for i := len(lines) - 1; i >= 0; i-- {
						trimmedLine := strings.TrimSpace(lines[i])
						if trimmedLine != "" {
							s = trimmedLine
							status.SetMessage(searchPrompt + " " + s)
							status.ShowNoTimeout(c, e)
							break
						}
					}
				} else if trimmedLine := strings.TrimSpace(clipboardString); trimmedLine != "" {
					s = trimmedLine
					status.SetMessage(searchPrompt + " " + s)
					status.ShowNoTimeout(c, e)
				}
			}
		case "↑": // previous in the search history
			if len(*searchHistory) == 0 {
				break
			}
			searchHistoryIndex--
			if searchHistoryIndex < 0 {
				// wraparound
				searchHistoryIndex = len(*searchHistory) - 1
			}
			s = (*searchHistory)[searchHistoryIndex]
			if previousSearch == "" {
				e.SetSearchTermWithTimeout(c, status, s, false, timeout)
			}
			status.SetMessage(searchPrompt + " " + s)
			status.ShowNoTimeout(c, e)
		case "↓": // next in the search history
			if len(*searchHistory) == 0 {
				break
			}
			searchHistoryIndex++
			if searchHistoryIndex >= len(*searchHistory) {
				// wraparound
				searchHistoryIndex = 0
			}
			s = (*searchHistory)[searchHistoryIndex]
			if previousSearch == "" {
				e.SetSearchTermWithTimeout(c, status, s, false, timeout)
			}
			status.SetMessage(searchPrompt + " " + s)
			status.ShowNoTimeout(c, e)
		default:
			if key != "" && !strings.HasPrefix(key, "c:") {
				s += key
				if previousSearch == "" {
					e.SetSearchTermWithTimeout(c, status, s, false, timeout)
				}
				status.SetMessage(searchPrompt + " " + s)
				status.ShowNoTimeout(c, e)
			}
		}
	}
	status.ClearAll(c)
	// Search settings
	forward := searchForward // forward search
	wrap := true             // with wraparound
	foundNoTypos := false
	spellCheckMode := false
	if s == "" && !replaceMode {
		// No search string entered, and not in replace mode, use the current word, if available
		s = e.CurrentWord()
	} else if s == "f" {
		// A special case, search backwards to the start of the function (or to "main")
		s = e.FuncPrefix()
		if s == "" {
			s = "main"
		}
		forward = false
	} else if s == "t" {
		// A special case, search forward for typos
		spellCheckMode = true
		foundNoTypos = false
		typo, err := e.SearchForTypo(c, status)
		if err != nil {
			return
		} else if err == errFoundNoTypos || typo == "" {
			foundNoTypos = true
			status.Clear(c)
			status.SetMessage("No typos found")
			status.Show(c, e)
			return
		} else {
			s = typo
			forward = true
		}
	}
	if pressedTab && previousSearch == "" { // search text -> tab
		// got the search text, now gather the replace text
		previousSearch = e.searchTerm
		searchPrompt = "Replace with:"
		replaceMode = true
		goto AGAIN
	} else if pressedTab && previousSearch != "" { // search text -> tab -> replace text- > tab
		undo.Snapshot(e)
		// replace once
		searchFor := previousSearch
		replaceWith := s
		replaced := strings.Replace(e.String(), searchFor, replaceWith, 1)
		e.LoadBytes([]byte(replaced))
		if replaceWith == "" {
			status.messageAfterRedraw = "Removed " + searchFor + ", once"
		} else {
			status.messageAfterRedraw = "Replaced " + searchFor + " with " + replaceWith + ", once"
		}
		// Save "searchFor" to the search history
		if trimmedSearchString := strings.TrimSpace(searchFor); trimmedSearchString != "" {
			if lastEntryIsNot(*searchHistory, trimmedSearchString) {
				*searchHistory = append(*searchHistory, trimmedSearchString)
			}
			// ignore errors saving the search history, since it's not critical
			if !e.slowLoad {
				SaveSearchHistory(searchHistoryFilename, *searchHistory)
			}
		}
		// Set up a redraw and return
		e.redraw = true
		return
	} else if pressedReturn && previousSearch != "" { // search text -> tab -> replace text -> return
		undo.Snapshot(e)
		// replace all
		searchForBytes := []byte(previousSearch)
		replaceWithBytes := []byte(s)
		// check if we're searching and replacing an unicode character, like "U+0047" or "u+0000"
		if r, err := runeFromUBytes(searchForBytes); err == nil { // success
			searchForBytes = []byte(string(r))
		}
		if r, err := runeFromUBytes(replaceWithBytes); err == nil { // success
			replaceWithBytes = []byte(string(r))
		}
		// perform the replacements, and count the number of instances
		allBytes := []byte(e.String())
		instanceCount := bytes.Count(allBytes, searchForBytes)
		allReplaced := bytes.ReplaceAll(allBytes, searchForBytes, replaceWithBytes)
		// replace the contents
		e.LoadBytes(allReplaced)
		// build a status message
		extraS := ""
		if instanceCount != 1 {
			extraS = "s"
		}
		if s == "" {
			status.messageAfterRedraw = fmt.Sprintf("Removed %d instance%s of %s", instanceCount, extraS, previousSearch)
		} else {
			status.messageAfterRedraw = fmt.Sprintf("Replaced %d instance%s of %s with %s", instanceCount, extraS, previousSearch, s)
		}
		// Save "searchFor" to the search history
		if trimmedSearchString := strings.TrimSpace(string(searchForBytes)); trimmedSearchString != "" {
			if lastEntryIsNot(*searchHistory, trimmedSearchString) {
				*searchHistory = append(*searchHistory, trimmedSearchString)
			}
			// ignore errors saving the search history, since it's not critical
			if !e.slowLoad {
				SaveSearchHistory(searchHistoryFilename, *searchHistory)
			}
		}
		// Set up a redraw and return
		e.redraw = true
		return
	}
	e.SetSearchTermWithTimeout(c, status, s, spellCheckMode, timeout)
	if pressedReturn {
		// Return to the first location before performing the actual search
		e.GoToLineNumber(initialLocation, c, status, false)
		// Save "s" to the search history
		if trimmedSearchString := strings.TrimSpace(s); s != "" {
			if lastEntryIsNot(*searchHistory, trimmedSearchString) {
				*searchHistory = append(*searchHistory, trimmedSearchString)
			}
			// ignore errors saving the search history, since it's not critical
			if !e.slowLoad {
				SaveSearchHistory(searchHistoryFilename, *searchHistory)
			}
		} else if len(*searchHistory) > 0 {
			s = (*searchHistory)[searchHistoryIndex]
			e.SetSearchTerm(c, status, s, false) // no timeout
		}
	}
	if previousSearch == "" {
		// Perform the actual search
		if err := e.GoToNextMatch(c, status, wrap, forward); err == errNoSearchMatch {
			// If no match was found, and return was not pressed, try again from the top
			// e.GoToTop(c, status)
			// err = e.GoToNextMatch(c, status)
			if err == errNoSearchMatch {
				if foundNoTypos || spellCheckMode {
					status.SetMessage("No typos found")
					e.ClearSearch()
				} else if wrap {
					status.SetMessage(s + " not found")
				} else {
					status.SetMessage(s + " not found from here")
				}
				status.ShowNoTimeout(c, e)
			}
		}
		e.Center(c)
	}
}

// LoadSearchHistory will load a list of strings from the given filename
func LoadSearchHistory(filename string) ([]string, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return []string{}, err
	}
	// This can load empty words, but they should never be stored in the first place
	return strings.Split(string(data), "\n"), nil
}

// SaveSearchHistory will save a list of strings to the given filename
func SaveSearchHistory(filename string, list []string) error {
	if len(list) == 0 {
		return nil
	}

	if noWriteToCache {
		return nil
	}

	// First create the folder, if needed, in a best effort attempt
	folderPath := filepath.Dir(filename)
	os.MkdirAll(folderPath, os.ModePerm)

	// Then save the data, with strict permissions
	data := []byte(strings.Join(list, "\n"))
	return os.WriteFile(filename, data, 0o600)
}

// NanoNextTypo tries to jump to the next typo
func (e *Editor) NanoNextTypo(c *vt100.Canvas, status *StatusBar) {
	if typoWord, err := e.SearchForTypo(c, status); err == nil || err == errFoundNoTypos {
		e.redraw = true
		e.redrawCursor = true
		if err == errFoundNoTypos || typoWord == "" {
			status.Clear(c)
			status.SetMessage("No typos found")
			status.Show(c, e)
			return
		}
		e.SetSearchTerm(c, status, typoWord, true) // true for spellCheckMode
		if err := e.GoToNextMatch(c, status, true, true); err == errNoSearchMatch {
			status.SetMessage("No typos found")
			e.ClearSearch()
			status.ShowNoTimeout(c, e)
		}
	}
}
