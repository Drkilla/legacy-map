package parser

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// TraceEntry represents a single line from an XDebug trace_format=1 file.
type TraceEntry struct {
	Level        int
	FunctionNr   int
	IsEntry      bool    // EntryExit == "0"
	IsExit       bool    // EntryExit == "1"
	IsReturn     bool    // EntryExit == "R"
	Timestamp    float64
	Memory       int64
	FunctionName string
	UserDefined  bool
	Filename     string
	LineNumber   int
	Params       []string
	ReturnValue  string // only for IsReturn
}

const defaultScanBufferSize = 1024 * 1024 // 1 MB — trace lines with many params can be long

// ParseFile parses an entire XDebug trace file and returns all entries.
// For large files, prefer ParseStream.
func ParseFile(path string) ([]TraceEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open trace file: %w", err)
	}
	defer f.Close()

	var entries []TraceEntry
	err = ParseStream(f, func(e TraceEntry) error {
		entries = append(entries, e)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return entries, nil
}

// ParseStream reads an XDebug trace_format=1 stream line by line and calls
// the callback for each parsed entry. It never loads the entire file into memory.
func ParseStream(r io.Reader, callback func(TraceEntry) error) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, defaultScanBufferSize), defaultScanBufferSize)

	// Skip 3-line header: Version, File format, TRACE START
	for i := 0; i < 3; i++ {
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return fmt.Errorf("reading header: %w", err)
			}
			return fmt.Errorf("unexpected end of file in header (line %d)", i+1)
		}
	}

	lineNum := 3
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		// Skip empty lines, footer, and summary lines (start with tabs)
		if line == "" || strings.HasPrefix(line, "TRACE END") || strings.HasPrefix(line, "\t") {
			continue
		}

		entry, err := parseLine(line)
		if err != nil {
			return fmt.Errorf("line %d: %w", lineNum, err)
		}

		if err := callback(entry); err != nil {
			return err
		}
	}

	return scanner.Err()
}

// parseLine parses a single tab-separated trace line into a TraceEntry.
func parseLine(line string) (TraceEntry, error) {
	fields := strings.Split(line, "\t")

	// Minimum fields: Level, FunctionNr, EntryExit (3 fields)
	if len(fields) < 3 {
		return TraceEntry{}, fmt.Errorf("expected at least 3 fields, got %d", len(fields))
	}

	var e TraceEntry

	level, err := strconv.Atoi(fields[0])
	if err != nil {
		return TraceEntry{}, fmt.Errorf("parse level %q: %w", fields[0], err)
	}
	e.Level = level

	funcNr, err := strconv.Atoi(fields[1])
	if err != nil {
		return TraceEntry{}, fmt.Errorf("parse function_nr %q: %w", fields[1], err)
	}
	e.FunctionNr = funcNr

	switch fields[2] {
	case "0":
		e.IsEntry = true
	case "1":
		e.IsExit = true
	case "R":
		e.IsReturn = true
	default:
		return TraceEntry{}, fmt.Errorf("unknown entry/exit marker %q", fields[2])
	}

	// Return lines in real XDebug output have empty timestamp/memory fields:
	// Level\tFuncNr\tR\t\t\tReturnValue
	if e.IsReturn {
		// Find the return value — it's the last non-empty field after the marker
		for i := len(fields) - 1; i > 2; i-- {
			if fields[i] != "" {
				e.ReturnValue = fields[i]
				break
			}
		}
		return e, nil
	}

	// Exit and Entry lines have timestamp and memory
	if len(fields) < 5 {
		return TraceEntry{}, fmt.Errorf("expected at least 5 fields, got %d", len(fields))
	}

	ts, err := strconv.ParseFloat(fields[3], 64)
	if err != nil {
		return TraceEntry{}, fmt.Errorf("parse timestamp %q: %w", fields[3], err)
	}
	e.Timestamp = ts

	mem, err := strconv.ParseInt(fields[4], 10, 64)
	if err != nil {
		return TraceEntry{}, fmt.Errorf("parse memory %q: %w", fields[4], err)
	}
	e.Memory = mem

	// Exit lines only have 5 fields
	if e.IsExit {
		return e, nil
	}

	// Entry lines: fields 5+ contain function info and params
	if len(fields) < 11 {
		return TraceEntry{}, fmt.Errorf("entry line expected at least 11 fields, got %d", len(fields))
	}

	e.FunctionName = fields[5]
	e.UserDefined = fields[6] == "1"
	// fields[7] = IncludeFile (ignored for now)
	e.Filename = fields[8]

	lineNo, err := strconv.Atoi(fields[9])
	if err != nil {
		return TraceEntry{}, fmt.Errorf("parse line_number %q: %w", fields[9], err)
	}
	e.LineNumber = lineNo

	paramCount, err := strconv.Atoi(fields[10])
	if err != nil {
		return TraceEntry{}, fmt.Errorf("parse param_count %q: %w", fields[10], err)
	}

	if paramCount > 0 && len(fields) > 11 {
		e.Params = make([]string, 0, paramCount)
		for i := 11; i < len(fields) && i < 11+paramCount; i++ {
			e.Params = append(e.Params, fields[i])
		}
	}

	return e, nil
}
