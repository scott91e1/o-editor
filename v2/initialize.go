package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xyproto/env"
	"github.com/xyproto/mode"
	"github.com/xyproto/vt100"
)

// NewEditor takes a filename and a line number to jump to (may be 0)
// Returns an Editor, a status message and an error type
func NewEditor(tty *vt100.TTY, c *vt100.Canvas, filename string, lineNumber LineNumber, colNumber ColNumber, theme Theme, origSyntaxHighlight bool) (*Editor, string, error) {

	var (
		startTime          = time.Now()
		createdNewFile     bool   // used for indicating that a new file was created
		scrollSpeed        = 10   // number of lines to scroll when using `ctrl-n` and `ctrl-p`
		statusMessage      string // used when loading or creating a file, for the initial status message
		found              bool
		recordedLineNumber LineNumber
		err                error
		baseFilename       = filepath.Base(filename)
		readOnly           bool
	)

	// mode is what would have been an enum in other languages, for signalling if this file should be in git mode, markdown mode etc
	m := mode.Detect(filename)

	syntaxHighlight := origSyntaxHighlight && m != mode.Text && (m != mode.Blank || filepath.Ext(baseFilename) != "")

	adjustSyntaxHighlightingKeywords(m) // no theme changes, just language detection and keyword configuration

	tabsSpaces := m.TabsSpaces()

	// Additional per-mode considerations, before launching the editor
	rainbowParenthesis := syntaxHighlight // rainbow parenthesis
	switch m {
	case mode.Markdown, mode.Text, mode.Blank:
		rainbowParenthesis = false
	case mode.ManPage:
		readOnly = true
	}

	// New editor struct. Scroll 10 lines at a time, no word wrap.
	e := NewCustomEditor(tabsSpaces,
		rainbowParenthesis,
		scrollSpeed,
		m,
		theme,
		syntaxHighlight)

	if readOnly {
		e.readOnly = true
	}

	// For non-highlighted files, adjust the word wrap
	if !e.syntaxHighlight {
		// Adjust the word wrap if the terminal is too narrow
		w := int(c.Width())
		if w < e.wrapWidth {
			e.wrapWidth = w
		}
	}

	// Minor adjustments for some file types
	switch e.mode {
	case mode.Git:
		e.clearOnQuit = true
	case mode.ManPage:
		e.clearOnQuit = true
	}

	// Set the editor filename
	e.filename = filename

	// We wish to redraw the canvas and reposition the cursor
	e.redraw = true
	e.redrawCursor = true

	// Use os.Stat to check if the file exists, and load the file if it does
	var warningMessage string
	if fileInfo, err := os.Stat(e.filename); err == nil { // no issue

		// TODO: Enter file-rename mode when opening a directory?
		// Check if this is a directory
		if fileInfo.IsDir() {
			return nil, "", errors.New(e.filename + " is a directory")
		}

		warningMessage, err = e.Load(c, tty, e.filename)
		if err != nil {
			return nil, "", err
		}

		if !e.Empty() {
			firstLine := e.Line(0)
			//logf("File is not empty: %s. First line: %s\n", filename, firstLine)
			if m, found := mode.DetectFromContents(e.mode, firstLine, e.String); found && m != mode.Blank {
				//logf("Setting mode: %s\n", m.String())
				e.mode = m
			}
			if e.mode == mode.Config {
				e.syntaxHighlight = true
			}
		}

		if !e.slowLoad {
			// Test write, to check if the file can be written or not
			testfile, err := os.OpenFile(e.filename, os.O_WRONLY, 0664)
			if err != nil {
				// can not open the file for writing
				e.readOnly = true
				// Set the color to red when in read-only mode, unless the file is not in "/usr/share/doc",
				// in which case the user is unlikely to want to save the file,
				// and is likely to want a better reading experience.
				if !strings.HasPrefix(e.filename, "/usr/share/doc") {
					e.Foreground = vt100.LightRed
					// disable syntax highlighting, to make it clear that the text is red
					e.syntaxHighlight = false
				}
			}
			testfile.Close()
		}
	} else {

		// Prepare an empty file
		if newMode, err := e.PrepareEmpty(c, tty, e.filename); err != nil {
			return nil, "", err
		} else if newMode != mode.Blank {
			e.mode = newMode
		}

		// Test save, to check if the file can be created and written, or not
		if err := e.Save(c, tty); err != nil {
			// Check if the new file can be saved before the user starts working on the file.
			return nil, "", err
		}

		// Creating a new empty file worked out fine, don't save it until the user saves it
		if os.Remove(e.filename) != nil {
			// This should never happen
			return nil, "", errors.New("could not remove an empty file that was just created: " + e.filename)
		}
		createdNewFile = true
	}

	// The editing mode is decided at this point

	// The shebang may have been for bash, make further adjustments
	adjustSyntaxHighlightingKeywords(e.mode)

	// Additional per-mode considerations, before launching the editor
	e.tabsSpaces = m.TabsSpaces()

	switch e.mode {
	case mode.Markdown, mode.Text, mode.Blank:
		e.rainbowParenthesis = false
	}

	// If we're editing a git commit message, add a newline and enable word-wrap at 80
	if e.mode == mode.Git {
		e.Git = vt100.LightGreen

		if filepath.Base(e.filename) == "MERGE_MSG" {
			e.InsertLineBelow()
		} else if e.EmptyLine() {
			e.InsertLineBelow()
		}
		e.wrapWidth = 80
	}

	// If the file starts with a hash bang, enable syntax highlighting
	if strings.HasPrefix(strings.TrimSpace(e.Line(0)), "#!") && !e.readOnly {
		// Enable syntax highlighting and redraw
		e.syntaxHighlight = true
	}

	// Use a light theme if XTERM_VERSION (and not running with "ko") or
	// TERMINAL_EMULATOR is set to "JetBrains-JediTerm",
	// because $COLORFGBG is "15;0" even though the background is white.
	if !e.readOnly {
		if (env.Has("XTERM_VERSION") && !env.Bool("KO") && env.Str("ALACRITTY_LOG") == "") || env.Str("TERMINAL_EMULATOR") == "JetBrains-JediTerm" {
			e.setLightTheme()
		} else if shell := env.Str("SHELL"); (shell == "/bin/csh" || shell == "/bin/ksh" || strings.HasPrefix(shell, "/usr/local/bin")) && filepath.Base(os.Args[0]) != "default" {
			// This is likely to be FreeBSD or OpenBSD (and the executable/link name is not "default")
			e.setRedBlackTheme()
		}
	}

	// Find the absolute path to this filename
	absFilename, err := e.AbsFilename()
	if err != nil {
		// This should never happen, just use the given filename
		absFilename = e.filename
	}

	// Load the location history. This will be saved again later. Errors are ignored.
	e.locationHistory, err = LoadLocationHistory(locationHistoryFilename)
	if err == nil { // no error
		recordedLineNumber, found = e.locationHistory[absFilename]
	}

	if !e.slowLoad {
		// Load the search history. This will be saved again later. Errors are ignored.
		searchHistory, _ = LoadSearchHistory(searchHistoryFilename)
	}

	// Jump to the correct line number
	switch {
	case lineNumber > 0:
		if colNumber > 0 {
			e.GoToLineNumberAndCol(lineNumber, colNumber, c, nil, false)
		} else {
			e.GoToLineNumber(lineNumber, c, nil, false)
		}
		e.redraw = true
		e.redrawCursor = true
	case lineNumber == 0 && e.mode != mode.Git:
		// Load the o location history, if a line number was not given on the command line (and if available)
		if !found && !e.slowLoad {
			// Try to load the NeoVim location history, then
			recordedLineNumber, err = FindInNvimLocationHistory(nvimLocationHistoryFilename, absFilename)
			found = err == nil
		}
		if !found && !e.slowLoad {
			// Try to load the ViM location history, then
			recordedLineNumber, err = FindInVimLocationHistory(vimLocationHistoryFilename, absFilename)
			found = err == nil
		}
		// Check if an existing line number was found
		if found {
			lineNumber = recordedLineNumber
			e.GoToLineNumber(lineNumber, c, nil, true)
			e.redraw = true
			e.redrawCursor = true
			break
		}
		fallthrough
	default:
		// Draw editor lines from line 0 to h onto the canvas at 0,0
		e.DrawLines(c, false, false)
		e.redraw = false
	}

	// Make sure the location history isn't empty (the search history can be empty, it's just a string slice)
	if e.locationHistory == nil {
		e.locationHistory = make(map[string]LineNumber, 1)
		e.locationHistory[absFilename] = lineNumber
	}

	// Redraw the TUI, if needed
	if e.redraw {
		e.Center(c)
		if err := e.DrawLines(c, true, false); err != nil {
			warningMessage += " (" + err.Error() + ")"
		}
		e.redraw = false
	}

	// Record the startup duration, in milliseconds
	//startupMilliseconds := time.Since(startTime).Milliseconds() // Go 1.11 and above only
	startupMilliseconds := int64(time.Since(startTime)) / 1e6

	// Craft an appropriate status message
	if createdNewFile {
		statusMessage = "New " + e.filename
	} else if e.Empty() {
		statusMessage = "Loaded empty file: " + e.filename + warningMessage
		if e.readOnly {
			statusMessage += " (read only)"
		}
	} else {
		// If startup is slow (> 100 ms), display the startup time in the status bar
		if startupMilliseconds >= 100 {
			statusMessage = fmt.Sprintf("Loaded %s%s (%dms)", e.filename, warningMessage, startupMilliseconds)
		} else {
			statusMessage = fmt.Sprintf("Loaded %s%s", e.filename, warningMessage)
		}
		if e.readOnly {
			statusMessage += " (read only)"
		}
	}

	// If SSH_TTY or TMUX is set, redraw everything and then display the status message
	e.sshMode = env.Str("SSH_TTY") != "" || env.Str("TMUX") != "" || strings.Contains(env.Str("TERMCAP"), "|screen.")

	return e, statusMessage, nil
}